package service

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"sync"
	"testing"

	"gopkg.in/gographics/imagick.v3/imagick"
)

// TestMain initializes the ImageMagick environment once for the whole test
// run, mirroring how cmd/main.go drives the process. Individual ImageService
// methods must not call Initialize/Terminate themselves.
func TestMain(m *testing.M) {
	imagick.Initialize()
	code := m.Run()
	imagick.Terminate()
	os.Exit(code)
}

// makeTestPNG builds an in-memory PNG of the given size using only the
// standard library, so the tests need no binary fixtures.
func makeTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("failed to encode test png: %v", err)
	}
	return buf.Bytes()
}

// decodeDimensions decodes width/height from an encoded image using the
// standard library, independent of ImageMagick, to verify resize output.
func decodeDimensions(t *testing.T, data []byte) (int, int) {
	t.Helper()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("failed to decode resized image: %v", err)
	}
	return cfg.Width, cfg.Height
}

func TestImagickGetWidthHeight(t *testing.T) {
	s := &ImageService{}
	src := makeTestPNG(t, 120, 80)

	err, w, h := s.ImagickGetWidthHeight(src)
	if err != nil {
		t.Fatalf("ImagickGetWidthHeight returned error: %v", err)
	}
	if w != 120 || h != 80 {
		t.Fatalf("expected 120x80, got %dx%d", w, h)
	}
}

func TestImagickGetWidthHeight_InvalidInput(t *testing.T) {
	s := &ImageService{}
	err, w, h := s.ImagickGetWidthHeight([]byte("not an image"))
	if err == nil {
		t.Fatalf("expected error for invalid image, got width=%d height=%d", w, h)
	}
}

func TestImagickResize(t *testing.T) {
	s := &ImageService{}
	src := makeTestPNG(t, 200, 100)

	out := s.ImagickResize(src, 100, 50)
	if len(out) == 0 {
		t.Fatal("ImagickResize returned empty output")
	}

	w, h := decodeDimensions(t, out)
	// RatioWidthHeight preserves aspect ratio; the longest edge must shrink.
	if w > 100 || h > 100 {
		t.Fatalf("resized image not within bounds: got %dx%d", w, h)
	}
	if w == 200 || h == 100 {
		t.Fatalf("image was not resized: got %dx%d", w, h)
	}
}

// TestImagickConcurrentResize is the regression guard for the per-request
// Initialize/Terminate bug. With the global environment initialized once in
// TestMain, many goroutines must be able to create, use and destroy their own
// wands concurrently without crashing or producing empty output. Running it
// against the old code (Initialize/Terminate inside every call) panics/crashes.
func TestImagickConcurrentResize(t *testing.T) {
	s := &ImageService{}
	src := makeTestPNG(t, 300, 200)

	const goroutines = 64
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out := s.ImagickResize(src, 80, 80)
			if len(out) == 0 {
				errs <- &resizeError{"empty output under concurrency"}
				return
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent resize failed: %v", err)
	}
}

type resizeError struct{ msg string }

func (e *resizeError) Error() string { return e.msg }
