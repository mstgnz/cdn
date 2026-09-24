package handler

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/mstgnz/cdn/pkg/httpx"
	"github.com/mstgnz/cdn/service"
)

// A public read never answers 404: a traversal-shaped key gets the placeholder
// with 200, before MinIO is asked anything (the client here is nil).
func TestGetImageUnsafeKeyServesPlaceholder(t *testing.T) {
	// The placeholder path is relative to the service's working directory.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(".."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	want, err := os.ReadFile("public/notfound.png")
	if err != nil {
		t.Fatal(err)
	}

	h := NewImage(nil, service.NewAwsService(), service.NewArchive(service.NewAwsService()), &service.ImageService{})
	app := testRouter(func(r chi.Router) {
		r.Method(http.MethodGet, "/{bucket}/*", httpx.Handler(h.GetImage))
	})
	resp := serve(app, httptest.NewRequest("GET", "/golden/a/../x.png", nil))
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" || !bytes.Equal(got, want) {
		t.Fatalf("got %d %q and %d bytes, want the placeholder", resp.StatusCode, resp.Header.Get("Content-Type"), len(got))
	}
}
