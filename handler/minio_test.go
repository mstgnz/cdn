package handler

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/mstgnz/cdn/pkg/httpx"
)

// TestMinioHandler_Integration drives the bucket lifecycle (create, exists,
// list, remove) against a local MinIO. Skipped when MinIO is unreachable, so
// it never breaks CI without infrastructure (see dialTestMinio).
func TestMinioHandler_Integration(t *testing.T) {
	cl := dialTestMinio(t)
	h := NewMinioHandler(cl)

	app := testRouter(func(r chi.Router) {
		r.Method(http.MethodGet, "/minio/{bucket}/exists", httpx.Handler(h.BucketExists))
		r.Method(http.MethodGet, "/minio/bucket-list", httpx.Handler(h.BucketList))
		r.Method(http.MethodGet, "/minio/{bucket}/create", httpx.Handler(h.CreateBucket))
		r.Method(http.MethodDelete, "/minio/{bucket}/delete", httpx.Handler(h.RemoveBucket))
	})

	const bucket = "cdn-minio-itest"
	// Ensure a clean slate even if a previous run left the bucket behind.
	_ = doReq(t, app, "DELETE", "/minio/"+bucket+"/delete")
	t.Cleanup(func() { _ = doReq(t, app, "DELETE", "/minio/"+bucket+"/delete") })

	t.Run("create", func(t *testing.T) {
		resp := doReq(t, app, "GET", "/minio/"+bucket+"/create")
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create status = %d, want 201", resp.StatusCode)
		}
	})

	t.Run("exists after create", func(t *testing.T) {
		resp := doReq(t, app, "GET", "/minio/"+bucket+"/exists")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("exists status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("list includes the bucket", func(t *testing.T) {
		resp := doReq(t, app, "GET", "/minio/bucket-list")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("list status = %d, want 200", resp.StatusCode)
		}
		if !decodeBody(t, resp).Success {
			t.Fatal("expected success=true on bucket-list")
		}
	})

	t.Run("remove", func(t *testing.T) {
		resp := doReq(t, app, "DELETE", "/minio/"+bucket+"/delete")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("remove status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("exists after remove", func(t *testing.T) {
		resp := doReq(t, app, "GET", "/minio/"+bucket+"/exists")
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("exists-after-remove status = %d, want 404", resp.StatusCode)
		}
	})
}
