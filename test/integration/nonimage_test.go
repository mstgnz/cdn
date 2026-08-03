package integration

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

// This service is a CDN for files, not only for images: documents, audio and
// video are first-class. Everything that is not an image has to survive a round
// trip byte for byte, because the resize and image-validation machinery is
// gated on extension and anything leaking into the general path would silently
// corrupt exactly the formats nobody thinks to check.
//
// Which extensions are accepted at all is an allowlist in pkg/validator, and
// these fixtures stay inside it deliberately: a test that uploads a .txt would
// be asserting a policy the project does not have.
//
// Needs a running stack and CDN_TEST_TOKEN, like the rest of this package.

// fetchBytes GETs a path and returns the status, body and content type.
func fetchBytes(t *testing.T, path string) (int, []byte, string) {
	t.Helper()

	resp, err := (&http.Client{Timeout: timeout}).Get(baseURL + path)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return resp.StatusCode, body, resp.Header.Get("Content-Type")
}

// Fixtures carry real magic bytes, because content validation checks them.
var (
	pdfMagic  = []byte("%PDF-1.4\n")
	zipMagic  = []byte{0x50, 0x4B, 0x03, 0x04} // xlsx/pptx are ZIP containers
	mp4Magic  = []byte{0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70}
	mp3Magic  = []byte{0x49, 0x44, 0x33}                               // ID3v2
	riffMagic = []byte{0x52, 0x49, 0x46, 0x46}                         // wav/avi
	oleMagic  = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1} // legacy doc/xls
)

// withMagic prefixes a body with a format signature and pads it with
// non-repeating bytes, so a truncated or duplicated chunk cannot hide behind a
// run of identical values.
func withMagic(magic []byte, size int) []byte {
	out := make([]byte, 0, len(magic)+size)
	out = append(out, magic...)
	for i := 0; i < size; i++ {
		out = append(out, byte(i*31%251))
	}
	return out
}

func nonImageFixtures() []struct {
	name    string
	content []byte
} {
	return []struct {
		name    string
		content []byte
	}{
		{"document.pdf", append(append([]byte{}, pdfMagic...), []byte("1 0 obj\n<< /Type /Catalog >>\nendobj\n%%EOF\n")...)},
		{"sheet.xlsx", withMagic(zipMagic, 512)},
		{"deck.pptx", withMagic(zipMagic, 256)},
		{"report.docx", withMagic(zipMagic, 384)},
		{"legacy.doc", withMagic(oleMagic, 320)},
		{"bundle.zip", withMagic(zipMagic, 640)},
		{"clip.mp4", withMagic(mp4Magic, 2048)},
		{"song.mp3", withMagic(mp3Magic, 1024)},
		{"sound.wav", withMagic(riffMagic, 512)},
		{"dump.sql", []byte("-- a dump\nINSERT INTO t (c) VALUES ('ğüşiöç');\n")},
		// Text formats go through the UTF-8 fallback rather than a signature, and
		// the commas and quotes are there so a naive parser in the path would
		// corrupt it visibly.
		{"table.csv", []byte("id,name,total\n1,\"Ünal, Ayşe\",12.50\n2,\"O'Brien\",3\n")},
	}
}

// The core promise for non-image content: what comes out is exactly what went
// in. A single byte of difference means the CDN cannot be trusted with anything
// but pictures.
func TestNonImageFilesRoundTripByteForByte(t *testing.T) {
	requireServer(t)
	token := requireToken(t)

	for _, fixture := range nonImageFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			status, object := upload(t, token, testBucket, fixture.name, fixture.content)
			if status != http.StatusCreated {
				t.Fatalf("upload %s: status %d", fixture.name, status)
			}
			defer deleteObject(t, token, testBucket, object)

			getStatus, body, _ := fetchBytes(t, "/"+testBucket+"/"+object)
			if getStatus != http.StatusOK {
				t.Fatalf("get: status %d", getStatus)
			}

			if !bytes.Equal(body, fixture.content) {
				t.Fatalf("content changed in transit: sent %d bytes (%x), got %d bytes (%x)",
					len(fixture.content), sha256.Sum256(fixture.content),
					len(body), sha256.Sum256(body))
			}
		})
	}
}

// Resize is gated on the object's extension, so a width parameter on a document
// has to be ignored rather than fed to an image decoder. Handing a PDF to
// ImageMagick would at best fail and at worst reach for a delegate, which is the
// class of thing the hardened policy exists to prevent.
func TestResizeParamsAreIgnoredForNonImages(t *testing.T) {
	requireServer(t)
	token := requireToken(t)

	pdf := append(append([]byte{}, pdfMagic...), []byte("1 0 obj\n<< /Type /Catalog >>\nendobj\n%%EOF\n")...)

	status, object := upload(t, token, testBucket, "resize-me.pdf", pdf)
	if status != http.StatusCreated {
		t.Fatalf("upload: status %d", status)
	}
	defer deleteObject(t, token, testBucket, object)

	// Both resize forms: the query one and the path one.
	for _, path := range []string{
		"/" + testBucket + "/" + object + "?width=100&height=100",
		"/" + testBucket + "/w:100/h:100/" + object,
	} {
		t.Run(path, func(t *testing.T) {
			getStatus, body, _ := fetchBytes(t, path)
			if getStatus != http.StatusOK {
				t.Fatalf("status %d", getStatus)
			}
			if !bytes.Equal(body, pdf) {
				t.Fatalf("a resize parameter altered a non-image file: got %d bytes, want %d", len(body), len(pdf))
			}
		})
	}
}

// A large object takes the streaming read path rather than the buffered one,
// which is where a truncation bug would show up.
func TestLargeNonImageFileStreamsIntact(t *testing.T) {
	requireServer(t)
	token := requireToken(t)

	content := withMagic(mp4Magic, 5<<20) // 5 MB

	status, object := upload(t, token, testBucket, "large.mp4", content)
	if status != http.StatusCreated {
		t.Fatalf("upload: status %d", status)
	}
	defer deleteObject(t, token, testBucket, object)

	getStatus, body, _ := fetchBytes(t, "/"+testBucket+"/"+object)
	if getStatus != http.StatusOK {
		t.Fatalf("get: status %d", getStatus)
	}
	if len(body) != len(content) {
		t.Fatalf("length changed: sent %d, got %d", len(content), len(body))
	}
	if sha256.Sum256(body) != sha256.Sum256(content) {
		t.Fatal("content changed in transit despite matching length")
	}
}

// Extensions outside the allowlist are refused. This is a security boundary, not
// an oversight: the upload path is the one place an attacker chooses the name of
// a file this service will later serve.
func TestUnsupportedExtensionsAreRejected(t *testing.T) {
	requireServer(t)
	token := requireToken(t)

	for _, name := range []string{"notes.txt", "data.json", "script.sh", "page.html", "app.js", "tool.exe"} {
		t.Run(name, func(t *testing.T) {
			status, _ := upload(t, token, testBucket, name, []byte("harmless enough content"))
			if status == http.StatusCreated {
				t.Fatalf("%s was accepted; the extension allowlist did not hold", name)
			}
		})
	}
}

// The extension is only the first gate. Content is checked independently, so a
// payload that matches no known format cannot arrive under a name the allowlist
// happens to permit.
func TestAllowedExtensionWithUnknownContentIsRejected(t *testing.T) {
	requireServer(t)
	token := requireToken(t)

	// An ELF header: not any accepted format, not text.
	elf := []byte{0x7F, 0x45, 0x4C, 0x46, 0x02, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00}

	for _, name := range []string{"payload.zip", "payload.docx", "payload.pdf", "payload.csv"} {
		t.Run(name, func(t *testing.T) {
			status, _ := upload(t, token, testBucket, name, elf)
			if status == http.StatusCreated {
				t.Fatalf("%s carrying an ELF header was accepted; the content check did not run", name)
			}
		})
	}
}

// Stored XSS regression, verified against a live server.
//
// A .csv or .sql passes content validation by being valid UTF-8 text, and Go's
// sniffer reports text/html for anything that opens with markup. Before the fix
// this service answered `<script>…</script>` uploaded as evil.csv with
// Content-Type: text/html on its own origin, and nosniff was no defence because
// the browser was not guessing, it was being told.
//
// The bytes are still stored and returned unchanged. What changed is the type
// they are announced under.
func TestMarkupInTextFilesIsNotServedAsHTML(t *testing.T) {
	requireServer(t)
	token := requireToken(t)

	payloads := []struct {
		filename string
		content  []byte
	}{
		{"payload.csv", []byte("<script>alert(document.domain)</script>")},
		{"payload.sql", []byte("<html><body><script>alert(1)</script></body></html>")},
		{"payload.csv", []byte("<!DOCTYPE html><html><head></head><body>x</body></html>")},
	}

	for _, p := range payloads {
		t.Run(p.filename+"/"+string(p.content[:12]), func(t *testing.T) {
			status, object := upload(t, token, testBucket, p.filename, p.content)
			if status != http.StatusCreated {
				// Rejecting it outright is also an acceptable outcome; what must not
				// happen is acceptance followed by an HTML content type.
				t.Skipf("upload rejected with %d, nothing to serve", status)
			}
			defer deleteObject(t, token, testBucket, object)

			getStatus, body, contentType := fetchBytes(t, "/"+testBucket+"/"+object)
			if getStatus != http.StatusOK {
				t.Fatalf("get: status %d", getStatus)
			}

			if strings.Contains(strings.ToLower(contentType), "html") {
				t.Fatalf("served as %q; a browser would execute this on the CDN origin", contentType)
			}

			// The file itself is untouched: this is a serving fix, not a filter.
			if !bytes.Equal(body, p.content) {
				t.Errorf("content was altered: got %q", body)
			}
		})
	}
}

// batchUpload posts several files to /batch/upload in one request.
func batchUpload(t *testing.T, token, bucket string, files map[string][]byte) (int, []map[string]any) {
	t.Helper()

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for name, content := range files {
		part, err := form.CreateFormFile("files", name)
		if err != nil {
			t.Fatalf("create form file %s: %v", name, err)
		}
		if _, err := part.Write(content); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := form.WriteField("bucket", bucket); err != nil {
		t.Fatalf("write bucket field: %v", err)
	}
	if err := form.Close(); err != nil {
		t.Fatalf("close form: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/batch/upload", &body)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		t.Fatalf("batch upload request: %v", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)

	var decoded struct {
		Data struct {
			Results []map[string]any `json:"results"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw, &decoded)
	if len(decoded.Data.Results) > 0 {
		return resp.StatusCode, decoded.Data.Results
	}

	// The response has also been shaped as a bare list; fall back so a shape
	// change produces a readable failure rather than a nil dereference.
	var alt struct {
		Data []map[string]any `json:"data"`
	}
	_ = json.Unmarshal(raw, &alt)
	return resp.StatusCode, alt.Data
}

// Batch upload runs its files concurrently, which is where results get crossed:
// a shared buffer, or a loop variable captured by reference, shows up as one
// file's bytes stored under another file's name. Mixing image and non-image
// content in one request exercises both branches at once, and the assertion is
// per-file rather than on the count, because a crossed pair still counts right.
func TestBatchUploadStoresEveryFileWithItsOwnContent(t *testing.T) {
	requireServer(t)
	token := requireToken(t)

	files := map[string][]byte{
		"batch-a.png":  pngBytes(t, 40, 40),
		"batch-b.png":  pngBytes(t, 80, 20),
		"batch-c.pdf":  append(append([]byte{}, pdfMagic...), []byte("first document\n%%EOF\n")...),
		"batch-d.sql":  []byte("-- the fourth file\nSELECT 4;\n"),
		"batch-e.mp4":  withMagic(mp4Magic, 4096),
		"batch-f.xlsx": withMagic(zipMagic, 300),
	}

	status, results := batchUpload(t, token, testBucket, files)
	if status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("batch upload: status %d", status)
	}
	if len(results) != len(files) {
		t.Fatalf("expected %d results, got %d: %v", len(files), len(results), results)
	}

	seen := map[string]bool{}
	for _, res := range results {
		filename, _ := res["filename"].(string)
		object, _ := res["object_name"].(string)

		if ok, _ := res["success"].(bool); !ok {
			t.Errorf("%s: upload failed: %v", filename, res["error"])
			continue
		}
		if object == "" {
			t.Errorf("%s: no object name returned", filename)
			continue
		}
		defer deleteObject(t, token, testBucket, object)

		want, known := files[filename]
		if !known {
			t.Errorf("result names a file that was never sent: %q", filename)
			continue
		}
		if seen[filename] {
			t.Errorf("%s: reported twice", filename)
		}
		seen[filename] = true

		getStatus, body, _ := fetchBytes(t, "/"+testBucket+"/"+object)
		if getStatus != http.StatusOK {
			t.Errorf("%s: get returned %d", filename, getStatus)
			continue
		}
		if len(body) == 0 {
			t.Errorf("%s: stored object is empty", filename)
			continue
		}

		// Non-images must be byte-identical. Images may legitimately be re-encoded
		// when optimization is on, so they are only checked for being a real,
		// non-empty image of their own.
		isImage := filename == "batch-a.png" || filename == "batch-b.png"
		if !isImage && !bytes.Equal(body, want) {
			t.Errorf("%s: content changed in transit (sent %d bytes, got %d)", filename, len(want), len(body))
		}
		if isImage && !bytes.HasPrefix(body, []byte{0x89, 'P', 'N', 'G'}) {
			t.Errorf("%s: stored object is not a PNG", filename)
		}
	}

	if len(seen) != len(files) {
		t.Errorf("only %d of %d files were reported", len(seen), len(files))
	}
}

// A batch larger than MAX_BATCH_FILES must be refused outright rather than
// partially applied.
func TestBatchUploadRejectsOversizedBatch(t *testing.T) {
	requireServer(t)
	token := requireToken(t)

	files := map[string][]byte{}
	for i := 0; i < 101; i++ {
		files[fmt.Sprintf("bulk-%03d.sql", i)] = []byte("SELECT 1;")
	}

	status, _ := batchUpload(t, token, testBucket, files)
	if status != http.StatusBadRequest {
		t.Fatalf("an oversized batch was accepted: status %d", status)
	}
}

// Deleting has to take effect immediately. This is the reason nginx.conf carries
// no response cache: an object that keeps being served after DELETE is a worse
// failure than a missing optimisation, and the open source nginx build has no
// way to purge.
func TestDeletedObjectStopsBeingServed(t *testing.T) {
	requireServer(t)
	token := requireToken(t)

	content := append(append([]byte{}, pdfMagic...), []byte("delete me\n%%EOF\n")...)
	status, object := upload(t, token, testBucket, "ephemeral.pdf", content)
	if status != http.StatusCreated {
		t.Fatalf("upload: status %d", status)
	}

	// Fetch once first, so any cache in the path has been given the chance to
	// store it. Without this the test would pass even with caching enabled.
	if getStatus, _, _ := fetchBytes(t, "/"+testBucket+"/"+object); getStatus != http.StatusOK {
		t.Fatalf("first get: status %d", getStatus)
	}

	if code := deleteObject(t, token, testBucket, object); code != http.StatusOK {
		t.Fatalf("delete: status %d", code)
	}

	_, body, _ := fetchBytes(t, "/"+testBucket+"/"+object)
	if bytes.Equal(body, content) {
		t.Fatal("the object is still served after deletion")
	}
}
