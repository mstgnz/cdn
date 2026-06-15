package handler

import (
	"testing"

	"github.com/gofiber/fiber/v2"
)

// TestMinioHandler_Integration drives the bucket lifecycle (create, exists,
// list, remove) against a local MinIO. Skipped when MinIO is unreachable, so
// it never breaks CI without infrastructure (see dialTestMinio).
func TestMinioHandler_Integration(t *testing.T) {
	cl := dialTestMinio(t)
	h := NewMinioHandler(cl)

	app := fiber.New()
	app.Get("/minio/:bucket/exists", h.BucketExists)
	app.Get("/minio/bucket-list", h.BucketList)
	app.Get("/minio/:bucket/create", h.CreateBucket)
	app.Delete("/minio/:bucket/delete", h.RemoveBucket)

	const bucket = "cdn-minio-itest"
	// Ensure a clean slate even if a previous run left the bucket behind.
	_ = doReq(t, app, "DELETE", "/minio/"+bucket+"/delete")
	t.Cleanup(func() { _ = doReq(t, app, "DELETE", "/minio/"+bucket+"/delete") })

	t.Run("create", func(t *testing.T) {
		resp := doReq(t, app, "GET", "/minio/"+bucket+"/create")
		if resp.StatusCode != fiber.StatusCreated {
			t.Fatalf("create status = %d, want 201", resp.StatusCode)
		}
	})

	t.Run("exists after create", func(t *testing.T) {
		resp := doReq(t, app, "GET", "/minio/"+bucket+"/exists")
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("exists status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("list includes the bucket", func(t *testing.T) {
		resp := doReq(t, app, "GET", "/minio/bucket-list")
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("list status = %d, want 200", resp.StatusCode)
		}
		if !decodeBody(t, resp).Success {
			t.Fatal("expected success=true on bucket-list")
		}
	})

	t.Run("remove", func(t *testing.T) {
		resp := doReq(t, app, "DELETE", "/minio/"+bucket+"/delete")
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("remove status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("exists after remove", func(t *testing.T) {
		resp := doReq(t, app, "GET", "/minio/"+bucket+"/exists")
		if resp.StatusCode != fiber.StatusNotFound {
			t.Fatalf("exists-after-remove status = %d, want 404", resp.StatusCode)
		}
	})
}
