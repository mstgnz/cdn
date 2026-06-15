package service

import "testing"

// TestMinioClient is a construction smoke test: MinioClient builds a client
// from env (or defaults) without connecting, so it must return a non-nil
// client and not panic. Real bucket/object operations are covered by the
// env-gated integration tests in the handler package.
func TestMinioClient(t *testing.T) {
	if got := MinioClient(); got == nil {
		t.Fatal("MinioClient() returned nil")
	}
}
