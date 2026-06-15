package handler

import (
	"bytes"
	"context"
	stdimage "image"
	"image/color"
	"image/png"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/mstgnz/cdn/service"
)

func testEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// dialTestMinio connects to a local MinIO. If MinIO is not reachable the test
// is SKIPPED (not failed), so this never breaks CI or developers who do not
// have MinIO running. Point it elsewhere with MINIO_TEST_ENDPOINT /
// MINIO_TEST_ACCESS_KEY / MINIO_TEST_SECRET_KEY. Defaults match a local
// `docker compose up -d minio` (localhost:9000, minioadmin/minioadmin).
func dialTestMinio(t *testing.T) *minio.Client {
	t.Helper()
	endpoint := testEnv("MINIO_TEST_ENDPOINT", "localhost:9000")
	access := testEnv("MINIO_TEST_ACCESS_KEY", "minioadmin")
	secret := testEnv("MINIO_TEST_SECRET_KEY", "minioadmin")

	cl, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(access, secret, ""),
		Secure: false,
	})
	if err != nil {
		t.Skipf("MinIO client init failed (%v); set MINIO_TEST_* to run", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := cl.ListBuckets(ctx); err != nil {
		t.Skipf("local MinIO not reachable at %s (%v); run `docker compose up -d minio` to exercise this test", endpoint, err)
	}
	return cl
}

func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func mustPut(t *testing.T, cl *minio.Client, ctx context.Context, bucket, name string, data []byte, contentType string) {
	t.Helper()
	_, err := cl.PutObject(ctx, bucket, name, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		t.Fatalf("put %s: %v", name, err)
	}
}

// TestGetImage_Integration exercises the real GetImage paths against a local
// MinIO: streaming for direct (non-resize) fetches of both an image and a
// non-image (PDF), and the buffered ImageMagick resize path. Verifies the
// streamed bytes are intact (no early close / no truncation) and that resize
// actually shrinks the image.
func TestGetImage_Integration(t *testing.T) {
	cl := dialTestMinio(t)
	ctx := context.Background()
	const bucket = "cdn-stream-itest"

	if exists, _ := cl.BucketExists(ctx, bucket); !exists {
		if err := cl.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			t.Fatalf("make bucket: %v", err)
		}
	}

	pngBytes := makePNG(t, 240, 120)
	// A non-image payload large enough that streaming vs buffering matters.
	pdfBytes := append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("payload-"), 5000)...)

	mustPut(t, cl, ctx, bucket, "pic.png", pngBytes, "image/png")
	mustPut(t, cl, ctx, bucket, "doc.pdf", pdfBytes, "application/pdf")

	t.Cleanup(func() {
		_ = cl.RemoveObject(ctx, bucket, "pic.png", minio.RemoveObjectOptions{})
		_ = cl.RemoveObject(ctx, bucket, "doc.pdf", minio.RemoveObjectOptions{})
		_ = cl.RemoveBucket(ctx, bucket)
	})

	imageSvc := &service.ImageService{MinioClient: cl}
	h := NewImage(cl, service.NewAwsService(), imageSvc)
	app := fiber.New()
	app.Get("/:bucket/*", h.GetImage)

	t.Run("stream original image unchanged", func(t *testing.T) {
		resp, err := app.Test(httptest.NewRequest("GET", "/"+bucket+"/pic.png", nil), -1)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if !bytes.Equal(body, pngBytes) {
			t.Fatalf("streamed image differs: got %d bytes, want %d", len(body), len(pngBytes))
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/png") {
			t.Fatalf("content-type = %q, want image/png", ct)
		}
	})

	t.Run("stream non-image (pdf) unchanged", func(t *testing.T) {
		resp, err := app.Test(httptest.NewRequest("GET", "/"+bucket+"/doc.pdf", nil), -1)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if !bytes.Equal(body, pdfBytes) {
			t.Fatalf("streamed pdf differs: got %d bytes, want %d", len(body), len(pdfBytes))
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/pdf") {
			t.Fatalf("content-type = %q, want application/pdf", ct)
		}
	})

	t.Run("resize image is buffered and shrunk", func(t *testing.T) {
		resp, err := app.Test(httptest.NewRequest("GET", "/"+bucket+"/pic.png?width=60", nil), -1)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		cfg, _, err := stdimage.DecodeConfig(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("decode resized image: %v", err)
		}
		if cfg.Width > 60 || cfg.Width == 0 {
			t.Fatalf("resized width = %d, want 0 < w <= 60", cfg.Width)
		}
		if cfg.Width >= 240 {
			t.Fatalf("image was not resized: width = %d", cfg.Width)
		}
	})
}
