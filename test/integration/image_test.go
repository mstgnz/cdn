package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"testing"
)

// These exercise the behaviour that was previously only ever checked by hand:
// the upload/serve/resize/delete lifecycle, bucket isolation, and the rejection
// of files that are not what their extension claims.
//
// They need a running stack and the server's TOKEN, and skip when either is
// missing, in the same spirit as requireServer.
//
//	docker compose up -d
//	CDN_TEST_TOKEN=$(grep '^TOKEN=' .env | cut -d= -f2-) go test ./test/integration/...
//
// TestBucketScopedTokenIsolation additionally needs CDN_TEST_BUCKET_TOKEN, a
// token from config/tokens.json scoped to CDN_TEST_BUCKET.

const testBucket = "integration-test"

func requireToken(t *testing.T) string {
	t.Helper()
	token := strings.TrimSpace(os.Getenv("CDN_TEST_TOKEN"))
	if token == "" {
		t.Skip("CDN_TEST_TOKEN is not set; export the server's TOKEN to run authenticated integration tests")
	}
	return token
}

// pngBytes builds a solid-colour PNG of the given size.
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// upload posts a file to /upload and returns the status and the stored object
// name (empty when the upload was rejected).
func upload(t *testing.T, token, bucket, filename string, content []byte) (int, string) {
	t.Helper()

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if bucket != "" {
		if err := form.WriteField("bucket", bucket); err != nil {
			t.Fatalf("write bucket field: %v", err)
		}
	}
	if err := form.Close(); err != nil {
		t.Fatalf("close form: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/upload", &body)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	defer resp.Body.Close()

	var decoded struct {
		Data struct {
			ObjectName string `json:"objectName"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	return resp.StatusCode, decoded.Data.ObjectName
}

// fetchPNGSize GETs a path and returns the decoded dimensions of the response.
func fetchPNGSize(t *testing.T, path string) (int, int) {
	t.Helper()

	resp, err := (&http.Client{Timeout: timeout}).Get(baseURL + path)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get %s: status %d", path, resp.StatusCode)
	}
	cfg, err := png.DecodeConfig(resp.Body)
	if err != nil {
		t.Fatalf("get %s: response is not a PNG: %v", path, err)
	}
	return cfg.Width, cfg.Height
}

func deleteObject(t *testing.T, token, bucket, object string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, baseURL+"/"+bucket+"/"+object, nil)
	if err != nil {
		t.Fatalf("build delete request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		t.Fatalf("delete request: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// TestResizeViaPathParams is the regression test for the bug this file was
// written after: the three path-based resize routes were registered, routed and
// documented, but the handler only ever read the query form, so every one of
// them quietly served the original image.
func TestResizeViaPathParams(t *testing.T) {
	requireServer(t)
	token := requireToken(t)

	const (
		originalW = 200
		originalH = 120
	)
	status, object := upload(t, token, testBucket, "resize-path.png", pngBytes(t, originalW, originalH))
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("upload failed with status %d", status)
	}
	t.Cleanup(func() { deleteObject(t, token, testBucket, object) })

	// Sanity check: unresized, the original comes back untouched.
	if w, h := fetchPNGSize(t, "/"+testBucket+"/"+object); w != originalW || h != originalH {
		t.Fatalf("plain GET returned %dx%d, want %dx%d", w, h, originalW, originalH)
	}

	cases := []struct {
		name  string
		path  string
		wantW int
		wantH int
	}{
		{"width and height", fmt.Sprintf("/%s/w:100/h:100/%s", testBucket, object), 100, 100},
		{"width only keeps the ratio", fmt.Sprintf("/%s/w:100/%s", testBucket, object), 100, 60},
		{"height only keeps the ratio", fmt.Sprintf("/%s/h:60/%s", testBucket, object), 100, 60},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, h := fetchPNGSize(t, c.path)
			if w != c.wantW || h != c.wantH {
				t.Fatalf("%s returned %dx%d, want %dx%d (an unresized %dx%d means the path form is being ignored)",
					c.path, w, h, c.wantW, c.wantH, originalW, originalH)
			}
		})
	}
}

func TestResizeViaQueryParams(t *testing.T) {
	requireServer(t)
	token := requireToken(t)

	status, object := upload(t, token, testBucket, "resize-query.png", pngBytes(t, 200, 120))
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("upload failed with status %d", status)
	}
	t.Cleanup(func() { deleteObject(t, token, testBucket, object) })

	if w, h := fetchPNGSize(t, fmt.Sprintf("/%s/%s?width=100&height=100", testBucket, object)); w != 100 || h != 100 {
		t.Fatalf("query resize returned %dx%d, want 100x100", w, h)
	}
	if w, h := fetchPNGSize(t, fmt.Sprintf("/%s/%s?width=50", testBucket, object)); w != 50 || h != 30 {
		t.Fatalf("query width-only resize returned %dx%d, want 50x30", w, h)
	}
}

// TestResizeDimensionsAreClamped covers the DoS guard on the public GET route:
// an oversized request must be capped rather than allocating whatever was asked
// for, and a negative one must not wrap around into a huge unsigned value.
func TestResizeDimensionsAreClamped(t *testing.T) {
	requireServer(t)
	token := requireToken(t)

	status, object := upload(t, token, testBucket, "clamp.png", pngBytes(t, 200, 120))
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("upload failed with status %d", status)
	}
	t.Cleanup(func() { deleteObject(t, token, testBucket, object) })

	// MAX_RESIZE_DIMENSION defaults to 4096.
	if w, h := fetchPNGSize(t, fmt.Sprintf("/%s/%s?width=99999&height=99999", testBucket, object)); w > 4096 || h > 4096 {
		t.Fatalf("oversized resize returned %dx%d, want both capped at 4096", w, h)
	}
	// Negative values floor to zero, which means no resize at all.
	if w, h := fetchPNGSize(t, fmt.Sprintf("/%s/%s?width=-50&height=-50", testBucket, object)); w != 200 || h != 120 {
		t.Fatalf("negative resize returned %dx%d, want the original 200x120", w, h)
	}
}

func TestUploadServeDeleteLifecycle(t *testing.T) {
	requireServer(t)
	token := requireToken(t)

	status, object := upload(t, token, testBucket, "lifecycle.png", pngBytes(t, 64, 64))
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("upload failed with status %d", status)
	}
	if object == "" {
		t.Fatal("upload returned no object name")
	}

	if w, h := fetchPNGSize(t, "/"+testBucket+"/"+object); w != 64 || h != 64 {
		t.Fatalf("served image is %dx%d, want 64x64", w, h)
	}

	if code := deleteObject(t, token, testBucket, object); code != http.StatusOK {
		t.Fatalf("delete returned %d, want 200", code)
	}

	// A deleted object falls back to the placeholder rather than erroring, so the
	// check is that the bytes are no longer the uploaded image.
	if w, h := fetchPNGSize(t, "/"+testBucket+"/"+object); w == 64 && h == 64 {
		t.Fatal("the object is still served after deletion")
	}
}

// TestMaliciousUploadsAreRejected covers the file-validation surface: an
// extension that is not allowed at all, and content that does not match the
// image extension it claims.
func TestMaliciousUploadsAreRejected(t *testing.T) {
	requireServer(t)
	token := requireToken(t)

	webshell := []byte("<?php system($_GET['c']); ?>")

	cases := []struct {
		name     string
		filename string
		content  []byte
	}{
		{"php extension", "shell.php", webshell},
		{"html extension", "page.html", []byte("<html><script>alert(1)</script></html>")},
		{"php content behind a jpg extension", "shell.jpg", webshell},
		{"php content behind a double extension", "shell.php.jpg", webshell},
		{"php content behind an svg extension", "shell.svg", webshell},
		{"truncated png", "broken.png", []byte("\x89PNG\r\n\x1a\nnot really a png")},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, object := upload(t, token, testBucket, c.filename, c.content)
			if status < 400 {
				if object != "" {
					deleteObject(t, token, testBucket, object)
				}
				t.Fatalf("%s was accepted with status %d, want a 4xx rejection", c.filename, status)
			}
		})
	}
}

// TestUploadedImageIsServedInert covers the polyglot case: a file that really is
// a valid image but carries an appended script payload is stored as uploaded,
// so the defence is in how it is served, not in rejecting it.
func TestUploadedImageIsServedInert(t *testing.T) {
	requireServer(t)
	token := requireToken(t)

	polyglot := append(pngBytes(t, 32, 32), []byte("<?php system($_GET['c']); ?>")...)
	status, object := upload(t, token, testBucket, "polyglot.png", polyglot)
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("valid png with an appended payload was rejected with %d", status)
	}
	t.Cleanup(func() { deleteObject(t, token, testBucket, object) })

	resp, err := (&http.Client{Timeout: timeout}).Get(baseURL + "/" + testBucket + "/" + object)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "image/") {
		t.Errorf("Content-Type = %q, want an image/* type so the payload cannot be reinterpreted", got)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

// TestSVGIsServedRenderableAndSandboxed pins both halves of how SVG is served.
// It used to go out as text/plain, because http.DetectContentType cannot
// recognise SVG, and with the global nosniff header browsers then refused to
// render it at all. Declaring the real type is what makes it work; the CSP is
// what keeps an inline <script> from running.
func TestSVGIsServedRenderableAndSandboxed(t *testing.T) {
	requireServer(t)
	token := requireToken(t)

	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10">` +
		`<script>alert(1)</script><rect width="10" height="10" fill="red"/></svg>`)

	status, object := upload(t, token, testBucket, "picture.svg", svg)
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("svg upload rejected with status %d", status)
	}
	t.Cleanup(func() { deleteObject(t, token, testBucket, object) })

	resp, err := (&http.Client{Timeout: timeout}).Get(baseURL + "/" + testBucket + "/" + object)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "image/svg+xml") {
		t.Errorf("Content-Type = %q, want image/svg+xml; text/plain would stop browsers rendering it", got)
	}

	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "sandbox") {
		t.Errorf("Content-Security-Policy = %q, want a sandbox directive to block inline scripts", csp)
	}
	if !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("Content-Security-Policy = %q, want default-src 'none'", csp)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

func TestUnauthenticatedWritesAreRejected(t *testing.T) {
	requireServer(t)

	status, _ := upload(t, "", testBucket, "nope.png", pngBytes(t, 16, 16))
	if status < 400 {
		t.Errorf("upload without a token returned %d, want a 4xx", status)
	}

	status, _ = upload(t, "definitely-not-the-server-token", testBucket, "nope.png", pngBytes(t, 16, 16))
	if status < 400 {
		t.Errorf("upload with a wrong token returned %d, want a 4xx", status)
	}
}

// TestBucketScopedTokenIsolation is the end-to-end counterpart of the unit tests
// in pkg/config and handler: a scoped token may write to its own bucket and
// nothing else, and may not reach the operator routes at all.
func TestBucketScopedTokenIsolation(t *testing.T) {
	requireServer(t)
	generalToken := requireToken(t)

	scopedToken := strings.TrimSpace(os.Getenv("CDN_TEST_BUCKET_TOKEN"))
	scopedBucket := strings.TrimSpace(os.Getenv("CDN_TEST_BUCKET"))
	if scopedToken == "" || scopedBucket == "" {
		t.Skip("CDN_TEST_BUCKET_TOKEN and CDN_TEST_BUCKET are not set; add an entry to config/tokens.json to run this")
	}
	credential := scopedBucket + ":" + scopedToken

	// The other bucket has to exist, otherwise a rejection could just as well be
	// "no such bucket" rather than the isolation check doing its job.
	if status, object := upload(t, generalToken, testBucket, "other.png", pngBytes(t, 16, 16)); status < 400 {
		t.Cleanup(func() { deleteObject(t, generalToken, testBucket, object) })
	}

	t.Run("writes to its own bucket", func(t *testing.T) {
		status, object := upload(t, credential, scopedBucket, "own.png", pngBytes(t, 16, 16))
		if status != http.StatusCreated && status != http.StatusOK {
			t.Fatalf("status %d, want a successful upload", status)
		}
		deleteObject(t, credential, scopedBucket, object)
	})

	t.Run("bucket field may be omitted", func(t *testing.T) {
		status, object := upload(t, credential, "", "implicit.png", pngBytes(t, 16, 16))
		if status != http.StatusCreated && status != http.StatusOK {
			t.Fatalf("status %d, want the token's own bucket to be used", status)
		}
		deleteObject(t, credential, scopedBucket, object)
	})

	t.Run("cannot write to another bucket", func(t *testing.T) {
		status, object := upload(t, credential, testBucket, "intruder.png", pngBytes(t, 16, 16))
		if status != http.StatusForbidden {
			if object != "" {
				deleteObject(t, generalToken, testBucket, object)
			}
			t.Fatalf("status %d, want 403", status)
		}
	})

	t.Run("cannot delete from another bucket", func(t *testing.T) {
		if code := deleteObject(t, credential, testBucket, "anything.png"); code != http.StatusForbidden {
			t.Fatalf("status %d, want 403", code)
		}
	})

	t.Run("the bucket prefix alone grants nothing", func(t *testing.T) {
		status, _ := upload(t, scopedBucket+":wrong-secret-of-a-plausible-length", scopedBucket, "x.png", pngBytes(t, 16, 16))
		if status < 400 {
			t.Errorf("a wrong secret was accepted with status %d", status)
		}
		status, _ = upload(t, testBucket+":"+scopedToken, testBucket, "x.png", pngBytes(t, 16, 16))
		if status < 400 {
			t.Errorf("a valid secret paired with another bucket was accepted with status %d", status)
		}
	})

	t.Run("operator routes reject it", func(t *testing.T) {
		for _, route := range []string{"/minio/bucket-list", "/aws/bucket-list", "/monitor", "/metrics"} {
			req, err := http.NewRequest(http.MethodGet, baseURL+route, nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			req.Header.Set("Authorization", "Bearer "+credential)
			resp, err := (&http.Client{Timeout: timeout}).Do(req)
			if err != nil {
				t.Fatalf("request %s: %v", route, err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()

			if resp.StatusCode < 400 {
				t.Errorf("%s accepted a bucket-scoped token with status %d", route, resp.StatusCode)
			}
		}
	})
}
