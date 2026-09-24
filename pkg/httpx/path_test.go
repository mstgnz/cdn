package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestDetectionPath(t *testing.T) {
	cases := map[string]string{
		"/":                 "/",
		"/Health":           "/health",
		"/health/":          "/health",
		"/golden/P%20X.PNG": "/golden/p%20x.png",
		"/a//":              "/a",
		"/\xc5\x9fehir":     "/\xc5\x9fehir", // non-ASCII bytes untouched, length kept
	}
	for in, want := range cases {
		if got := DetectionPath(in); got != want {
			t.Errorf("DetectionPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// Param has to hand handlers what fiber's c.Params did: the raw path's case and
// encoding, sliced where fiber sliced it. Each row is a request the golden
// files pin end to end.
func TestParamMatchesFiber(t *testing.T) {
	r := chi.NewRouter()
	r.Use(Prepare)
	echo := func(names ...string) http.HandlerFunc {
		return func(w http.ResponseWriter, req *http.Request) {
			out := ""
			for _, n := range names {
				out += n + "=" + Param(req, n) + ";"
			}
			_, _ = w.Write([]byte(out))
		}
	}
	r.Get("/{bucket}/w:{width}/h:{height}/*", echo("bucket", "width", "height", "*"))
	r.Get("/{bucket}/w:{width}/*", echo("bucket", "width", "*"))
	r.Get("/{bucket}/*", echo("bucket", "*"))
	r.Get("/{bucket}", echo("bucket", "*"))
	r.Get("/aws/{bucket}/exists", echo("bucket"))

	cases := []struct{ target, want string }{
		{"/golden/p/x.png", "bucket=golden;*=p/x.png;"},
		{"/GOLDEN/p/X.png", "bucket=GOLDEN;*=p/X.png;"},
		{"/golden/p/x.png/", "bucket=golden;*=p/x.png;"},
		{"/golden//p/x.png", "bucket=golden;*=/p/x.png;"},
		{"/golden/pct%20dir/x.png", "bucket=golden;*=pct%20dir/x.png;"},
		{"/golden/W:40/p/x.png", "bucket=golden;width=40;*=p/x.png;"},
		{"/golden/w:40/h:20/x.png", "bucket=golden;width=40;height=20;*=x.png;"},
		{"/golden", "bucket=golden;*=;"},
		{"/golden/", "bucket=golden;*=;"},
		{"/AWS/MyBucket/exists", "bucket=MyBucket;"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest("GET", c.target, nil))
		if got := rec.Body.String(); got != c.want {
			t.Errorf("%s: got %q, want %q", c.target, got, c.want)
		}
	}
}

func TestParamWithoutRouteIsEmpty(t *testing.T) {
	if got := Param(httptest.NewRequest("GET", "/x", nil), "bucket"); got != "" {
		t.Fatalf("Param outside a chi route = %q, want empty", got)
	}
}
