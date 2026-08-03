package handler

import (
	"bytes"
	stdimage "image"
	"image/color"
	"image/png"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// The stdlib image package is aliased because this package already declares a
// type called image (the handler itself).

// pngFixture builds a real, decodable PNG. The content gate runs before
// ImageMagick sees the bytes, and ImageMagick then has to decode them, so a
// hand-written signature is not enough here.
func pngFixture(t *testing.T, w, h int) []byte {
	t.Helper()
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 0x40, A: 0xFF})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// TestResizeImageReturnsTheResizedBytes is the regression test for an endpoint
// that did the work and threw it away. /resize decoded the upload, resized it,
// checked the result was not nil, discarded it, and answered with a JSON
// "Image processed successfully" carrying no image. A caller posting a file and
// a target size had no way to obtain the output.
func TestResizeImageReturnsTheResizedBytes(t *testing.T) {
	app := newImageApp()
	original := pngFixture(t, 400, 300)

	body, ct := multipartForm(t,
		map[string]string{"width": "100", "height": "75"},
		"file", "photo.png", original)
	req := httptest.NewRequest("POST", "/resize", body)
	req.Header.Set("Content-Type", ct)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("response body was empty; the resized image was not returned")
	}
	if bytes.Equal(got, original) {
		t.Fatal("response was the original bytes; nothing was resized")
	}

	cfg, _, err := stdimage.DecodeConfig(bytes.NewReader(got))
	if err != nil {
		t.Fatalf("response body did not decode as an image: %v", err)
	}
	if cfg.Width != 100 || cfg.Height != 75 {
		t.Fatalf("resized to %dx%d, want 100x75", cfg.Width, cfg.Height)
	}

	if declared := resp.Header.Get("Content-Type"); declared != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", declared)
	}
}
