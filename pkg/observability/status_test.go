package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type reporting struct {
	http.ResponseWriter
	status int
}

func (r reporting) Status() int { return r.status }

type wrapping struct{ inner http.ResponseWriter }

func (w wrapping) Header() http.Header         { return w.inner.Header() }
func (w wrapping) Write(b []byte) (int, error) { return w.inner.Write(b) }
func (w wrapping) WriteHeader(int)             {}
func (w wrapping) Unwrap() http.ResponseWriter { return w.inner }

// The status label must come from the writer that saw the status, even when
// another wrapper sits in front of it.
func TestStatusOfUnwraps(t *testing.T) {
	if got := statusOf(wrapping{inner: reporting{ResponseWriter: httptest.NewRecorder(), status: 418}}); got != 418 {
		t.Fatalf("statusOf through a wrapper = %d, want 418", got)
	}
	if got := statusOf(httptest.NewRecorder()); got != 0 {
		t.Fatalf("statusOf without a reporter = %d, want 0", got)
	}
}

// main stops the process when the tracer cannot be built, so it must build with
// no collector listening; spans are only exported later, in the background.
func TestInitTracerNeedsNoCollector(t *testing.T) {
	cleanup, err := InitTracer("cdn-test", "http://127.0.0.1:1/api/traces")
	if err != nil {
		t.Fatalf("InitTracer: %v", err)
	}
	cleanup()
	if Tracer("cdn-test") == nil {
		t.Fatal("no tracer after InitTracer")
	}
}
