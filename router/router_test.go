package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mstgnz/cdn/handler"
	"github.com/mstgnz/cdn/pkg/httpx"
)

// fakeImage answers GetImage with the parameters it was routed with and every
// other route with its name.
type fakeImage struct{}

func (fakeImage) GetImage(w http.ResponseWriter, r *http.Request) error {
	httpx.Text(w, http.StatusOK, strings.Join([]string{
		httpx.Param(r, "bucket"), httpx.Param(r, "width"), httpx.Param(r, "height"), httpx.Param(r, "*"),
	}, "|"))
	return nil
}

func named(name string) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, _ *http.Request) error {
		httpx.Text(w, http.StatusOK, name)
		return nil
	}
}

func (fakeImage) UploadImage(w http.ResponseWriter, r *http.Request) error {
	return named("upload")(w, r)
}
func (fakeImage) DeleteImage(w http.ResponseWriter, r *http.Request) error {
	return named("delete")(w, r)
}
func (fakeImage) ResizeImage(w http.ResponseWriter, r *http.Request) error {
	return named("resize")(w, r)
}
func (fakeImage) UploadWithUrl(w http.ResponseWriter, r *http.Request) error {
	return named("upload-url")(w, r)
}
func (fakeImage) BatchUpload(w http.ResponseWriter, r *http.Request) error {
	return named("batch-upload")(w, r)
}
func (fakeImage) BatchDelete(w http.ResponseWriter, r *http.Request) error {
	return named("batch-delete")(w, r)
}

func testRouter(t *testing.T, mutate func(*Deps)) (http.Handler, *atomic.Int32) {
	t.Helper()
	t.Setenv("TOKEN", "general-token-for-route-tests-0123456789")
	var uploads atomic.Int32
	pass := func(next http.Handler) http.Handler { return next }
	d := Deps{
		Image:         fakeImage{},
		AWS:           handler.NewAwsHandler(nil),
		Minio:         handler.NewMinioHandler(nil),
		WS:            handler.NewWebSocketHandler(nil),
		Archive:       handler.NewArchiveHandler(nil),
		Health:        handler.NewHealthChecker(nil, nil, nil),
		GlobalLimiter: pass,
		UploadLimiter: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				uploads.Add(1)
				next.ServeHTTP(w, r)
			})
		},
		FaviconFile: "../public/favicon.png",
	}
	if mutate != nil {
		mutate(&d)
	}
	return httpx.Wrap(0, New(d)), &uploads
}

func do(h http.Handler, method, target string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRoutesImageParameters(t *testing.T) {
	h, _ := testRouter(t, nil)
	cases := map[string]string{
		"/Golden/W:40/p/x.png/":   "Golden|40||p/x.png",
		"/golden/h:20/x.png":      "golden||20|x.png",
		"/golden/w:40/h:20/a/b.c": "golden|40|20|a/b.c",
		"/golden":                 "golden|||",
		"/metrics/x.png":          "metrics|||x.png",
	}
	for target, want := range cases {
		if rec := do(h, "GET", target); rec.Code != 200 || rec.Body.String() != want {
			t.Errorf("GET %s: %d %q, want %q", target, rec.Code, rec.Body.String(), want)
		}
	}
	if rec := do(h, "HEAD", "/golden/x.png"); rec.Code != 200 {
		t.Errorf("HEAD on an image route: %d; fiber's app.Get served HEAD", rec.Code)
	}
}

func TestRoutesUnmatched(t *testing.T) {
	h, _ := testRouter(t, nil)

	rec := do(h, "POST", "/nope")
	if rec.Code != 405 || rec.Header().Get("Allow") != "GET, HEAD, DELETE" || rec.Body.String() != "Method Not Allowed" {
		t.Fatalf("POST /nope: %d %v %q", rec.Code, rec.Header(), rec.Body.String())
	}
	if rec := do(h, "PATCH", "/upload"); rec.Header().Get("Allow") != "GET, HEAD, POST, DELETE" {
		t.Fatalf("PATCH /upload: Allow = %q", rec.Header().Get("Allow"))
	}

	off, _ := testRouter(t, func(d *Deps) { d.DisableGet, d.DisableDelete, d.DisableUpload = true, true, true })
	rec = do(off, "GET", "/a&b/x.png")
	if rec.Code != 404 || rec.Body.String() != "Cannot GET /a&amp;b/x.png" {
		t.Fatalf("disabled GET: %d %q", rec.Code, rec.Body.String())
	}
}

// fiber matched Use and Group prefixes without a segment boundary.
func TestRoutesPrefixGates(t *testing.T) {
	h, _ := testRouter(t, nil)
	if rec := do(h, "GET", "/minioextra/x.png"); rec.Code != 400 || !strings.Contains(rec.Body.String(), "no token provided") {
		t.Fatalf("/minioextra without a token: %d %q", rec.Code, rec.Body.String())
	}
	if rec := do(h, "GET", "/minioextra/x.png", "Authorization", "Bearer general-token-for-route-tests-0123456789"); rec.Body.String() != "minioextra|||x.png" {
		t.Fatalf("/minioextra with the token: %d %q", rec.Code, rec.Body.String())
	}
	if rec := do(h, "GET", "/AWSx/y"); rec.Code != 400 {
		t.Fatalf("/AWSx: %d", rec.Code)
	}
	if rec := do(h, "GET", "/wsextra/x.png"); rec.Code != 426 || rec.Body.String() != "Upgrade Required" {
		t.Fatalf("/wsextra: %d %q", rec.Code, rec.Body.String())
	}
	if rec := do(h, "GET", "/ws", "Connection", "Upgrade", "Upgrade", "websocket"); rec.Code != 401 || rec.Body.String() != "Unauthorized" {
		t.Fatalf("/ws without a token: %d %q", rec.Code, rec.Body.String())
	}
}

// The upload group's limiter counted whatever reached it: the upload routes, the
// index page and unmatched requests, but not the image routes registered earlier.
func TestRoutesUploadLimiterScope(t *testing.T) {
	h, uploads := testRouter(t, nil)
	do(h, "GET", "/golden/x.png")
	if uploads.Load() != 0 {
		t.Fatal("an image read was counted by the upload limiter")
	}
	do(h, "GET", "/")
	do(h, "POST", "/nope")
	if uploads.Load() != 2 {
		t.Fatalf("upload limiter counted %d, want 2 (index and unmatched)", uploads.Load())
	}

	off, offUploads := testRouter(t, func(d *Deps) { d.DisableUpload = true })
	do(off, "GET", "/")
	if offUploads.Load() != 0 {
		t.Fatal("with DISABLE_UPLOAD there is no upload group to count anything")
	}
}
