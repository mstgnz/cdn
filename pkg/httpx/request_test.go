package httpx

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func multipartRequest(t *testing.T, target string, fields map[string]string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", target, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// fasthttp looked in the query string first, then the urlencoded body, then the
// multipart body; net/http's own FormValue prefers the body, which would change
// which bucket an upload lands in when both are given.
func TestFormValuePrecedence(t *testing.T) {
	req := multipartRequest(t, "/upload?bucket=fromquery", map[string]string{"bucket": "fromform", "path": "p"})
	if got := FormValue(req, "bucket"); got != "fromquery" {
		t.Errorf("bucket = %q, want the query value", got)
	}
	if got := FormValue(req, "path"); got != "p" {
		t.Errorf("path = %q, want the multipart value", got)
	}

	empty := multipartRequest(t, "/upload?bucket=", map[string]string{"bucket": "fromform"})
	if got := FormValue(empty, "bucket"); got != "fromform" {
		t.Errorf("an empty query value must fall through, got %q", got)
	}

	form := httptest.NewRequest("POST", "/x", strings.NewReader("bucket=frombody"))
	form.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if got := FormValue(form, "bucket"); got != "frombody" {
		t.Errorf("urlencoded bucket = %q", got)
	}
}

type bindTarget struct {
	Bucket    string   `json:"bucket"`
	URL       string   `json:"url"`
	Files     []string `json:"files"`
	AWSDelete bool     `json:"aws_delete"`
	Evict     *bool    `json:"evict"`
}

func bind(t *testing.T, ctype, body string) (bindTarget, error) {
	t.Helper()
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	var out bindTarget
	err := BindBody(req, &out)
	return out, err
}

func TestBindBodyJSON(t *testing.T) {
	out, err := bind(t, "application/json; charset=utf-8", `{"bucket":"b","files":["x","y"],"evict":false}`)
	if err != nil || out.Bucket != "b" || len(out.Files) != 2 || out.Evict == nil || *out.Evict {
		t.Fatalf("got %+v, %v", out, err)
	}
	if _, err := bind(t, "application/vnd.api+json", `{"bucket":"b"}`); err != nil {
		t.Fatalf("vendor json refused: %v", err)
	}
	if _, err := bind(t, "application/json", `{not-json`); err == nil {
		t.Fatal("invalid json accepted")
	}
}

// fiber's form binding matches struct field names case-insensitively (the json
// tags play no part), keeps the last value of a scalar, reads "files[]" as
// "files" and "on" as true.
func TestBindBodyForm(t *testing.T) {
	out, err := bind(t, "application/x-www-form-urlencoded", "BUCKET=a&BUCKET=b&url=http://x&files[]=1&files[]=2&awsdelete=on&evict=")
	if err != nil {
		t.Fatal(err)
	}
	if out.Bucket != "b" || out.URL != "http://x" || len(out.Files) != 2 || !out.AWSDelete {
		t.Fatalf("got %+v", out)
	}
	if out.Evict == nil || *out.Evict {
		t.Fatalf("an empty evict must allocate a false, got %v", out.Evict)
	}
	if _, err := bind(t, "application/x-www-form-urlencoded", "awsdelete=maybe"); err == nil {
		t.Fatal("an unparsable bool was accepted")
	}
	if _, err := bind(t, "application/x-www-form-urlencoded", "files[=1"); err == nil {
		t.Fatal("unmatched brackets were accepted")
	}
	if out, err := bind(t, "application/x-www-form-urlencoded", "aws_delete=true"); err != nil || out.AWSDelete {
		t.Fatalf("json tag names are not form aliases in fiber: %+v %v", out, err)
	}
}

// Batch delete is a DELETE; fasthttp read an urlencoded body whatever the
// method, net/http's ParseForm would not.
func TestBindBodyFormOnDelete(t *testing.T) {
	req := httptest.NewRequest("DELETE", "/batch/delete", strings.NewReader("bucket=b&files=x&files=y"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var out bindTarget
	if err := BindBody(req, &out); err != nil || out.Bucket != "b" || len(out.Files) != 2 {
		t.Fatalf("got %+v, %v", out, err)
	}
	if got := FormValue(req, "bucket"); got != "b" {
		t.Fatalf("FormValue after binding = %q", got)
	}
}

func TestBindBodyXMLAndUnknown(t *testing.T) {
	if out, err := bind(t, "application/xml", `<r><Bucket>b</Bucket></r>`); err != nil || out.Bucket != "b" {
		t.Fatalf("xml: %+v %v", out, err)
	}
	if _, err := bind(t, "", `{"bucket":"b"}`); err != ErrUnprocessableEntity {
		t.Fatalf("no content type: err = %v", err)
	}
	if _, err := bind(t, "text/plain", "x"); err != ErrUnprocessableEntity {
		t.Fatalf("text/plain: err = %v", err)
	}
}

// A file part over the memory limit spills to a temp file. net/http removes it
// only for the request it created itself, and handlers parse on copies, so
// Prepare has to; otherwise every large upload leaves a file behind.
func TestMultipartTempFilesRemovedAfterRequest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	previous := multipartMemory
	multipartMemory = 1
	t.Cleanup(func() { multipartMemory = previous })

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "big.bin")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(bytes.Repeat([]byte("x"), 64<<10))
	_ = mw.Close()

	var spilled int
	r := chi.NewRouter()
	r.Use(Prepare)
	r.Post("/upload", func(w http.ResponseWriter, req *http.Request) {
		req = req.WithContext(req.Context()) // a copy, as the auth middleware makes
		if _, err := FormFile(req, "file"); err != nil {
			t.Errorf("FormFile: %v", err)
		}
		entries, _ := os.ReadDir(dir)
		spilled = len(entries)
	})
	req := httptest.NewRequest("POST", "/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	r.ServeHTTP(httptest.NewRecorder(), req)

	if spilled == 0 {
		t.Fatal("the part did not spill to disk; the test proves nothing")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("%d temp file(s) left after the request", len(entries))
	}
}

type failingReader struct{ sent bool }

func (f *failingReader) Read(p []byte) (int, error) {
	if f.sent {
		return 0, io.ErrUnexpectedEOF
	}
	f.sent = true
	return copy(p, "bucket=b&files=x"), nil
}

// A body cut short must not be bound as if it were whole: fasthttp never ran a
// handler on a partial body.
func TestBindBodyRejectsTruncatedForm(t *testing.T) {
	req := httptest.NewRequest("DELETE", "/batch/delete", &failingReader{})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var out bindTarget
	if err := BindBody(req, &out); err == nil {
		t.Fatalf("a truncated body was bound: %+v", out)
	}
	if got := FormValue(req, "bucket"); got != "" {
		t.Fatalf("FormValue after a failed read = %q", got)
	}
}

func TestFormFileMissing(t *testing.T) {
	req := multipartRequest(t, "/", map[string]string{"bucket": "b"})
	if _, err := FormFile(req, "file"); err != http.ErrMissingFile {
		t.Fatalf("err = %v, want ErrMissingFile", err)
	}
	if _, err := FormFile(httptest.NewRequest("POST", "/", strings.NewReader("{}")), "file"); err == nil {
		t.Fatal("a non-multipart body yielded a file")
	}
}
