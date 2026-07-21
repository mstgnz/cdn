package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/mstgnz/cdn/service"
)

// newImageApp builds the image handler with a nil MinIO client. The validation
// paths under test reject the request before any MinIO call, so the nil client
// is never dereferenced.
func newImageApp() *fiber.App {
	h := NewImage(nil, service.NewAwsService(), &service.ImageService{})
	app := fiber.New()
	app.Post("/upload", h.UploadImage)
	app.Post("/resize", h.ResizeImage)
	app.Post("/upload-url", h.UploadWithUrl)
	app.Delete("/batch/delete", h.BatchDelete)
	return app
}

// multipartForm builds a multipart body with optional fields and an optional
// file part. Returns the body and the Content-Type header value.
func multipartForm(t *testing.T, fields map[string]string, fileField, fileName string, fileData []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("write field: %v", err)
		}
	}
	if fileField != "" {
		fw, err := w.CreateFormFile(fileField, fileName)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := fw.Write(fileData); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &buf, w.FormDataContentType()
}

func TestUploadImage_NoFile_BadRequest(t *testing.T) {
	app := newImageApp()
	body, ct := multipartForm(t, map[string]string{"bucket": "b"}, "", "", nil)
	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", ct)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400 when no file", resp.StatusCode)
	}
}

func TestResizeImage_NoFile_BadRequest(t *testing.T) {
	app := newImageApp()
	body, ct := multipartForm(t, map[string]string{"width": "100"}, "", "", nil)
	req := httptest.NewRequest("POST", "/resize", body)
	req.Header.Set("Content-Type", ct)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400 when no file", resp.StatusCode)
	}
}

// TestResizeImage_NonImagePassthrough verifies a non-image upload with no
// resize params is returned as-is (no ImageMagick, no MinIO).
func TestResizeImage_NonImagePassthrough(t *testing.T) {
	app := newImageApp()
	payload := []byte("just some plain text")
	body, ct := multipartForm(t, nil, "file", "notes.txt", payload)
	req := httptest.NewRequest("POST", "/resize", body)
	req.Header.Set("Content-Type", ct)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(got, payload) {
		t.Fatalf("passthrough body = %q, want %q", got, payload)
	}
}

func TestUploadWithUrl_InvalidBody_BadRequest(t *testing.T) {
	app := newImageApp()
	req := httptest.NewRequest("POST", "/upload-url", bytes.NewReader([]byte("{not-json")))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400 on invalid body", resp.StatusCode)
	}
}

// TestUploadWithUrl_PrivateURL_BadRequest guards the SSRF fix: a private/
// loopback target must be rejected with 400 before any network or MinIO call.
func TestUploadWithUrl_PrivateURL_BadRequest(t *testing.T) {
	app := newImageApp()
	body := []byte(`{"bucket":"b","url":"http://127.0.0.1:9/x.png"}`)
	req := httptest.NewRequest("POST", "/upload-url", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400 on private URL", resp.StatusCode)
	}
}

// TestBatchDelete_TooManyFiles_BadRequest guards the batch-size cap: a request
// over MAX_BATCH_FILES is rejected before any storage call (nil MinIO here).
func TestBatchDelete_TooManyFiles_BadRequest(t *testing.T) {
	t.Setenv("MAX_BATCH_FILES", "100")
	app := newImageApp()

	files := make([]string, 101)
	for i := range files {
		files[i] = "f.jpg"
	}
	body, err := json.Marshal(map[string]any{"bucket": "b", "files": files})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("DELETE", "/batch/delete", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400 on oversized batch", resp.StatusCode)
	}
}

func TestBatchDelete_InvalidBody_BadRequest(t *testing.T) {
	app := newImageApp()
	req := httptest.NewRequest("DELETE", "/batch/delete", bytes.NewReader([]byte("{not-json")))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400 on invalid body", resp.StatusCode)
	}
}
