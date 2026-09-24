package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"

	"github.com/mstgnz/cdn/pkg/httpx"
)

var okHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	httpx.Text(w, http.StatusOK, "ok")
})

func TestCORS(t *testing.T) {
	cases := []struct {
		name    string
		method  string
		headers map[string]string
		status  int
		want    map[string]string
	}{
		{"no origin", "GET", nil, 200, map[string]string{"Access-Control-Allow-Origin": "", "Vary": ""}},
		{"simple", "GET", map[string]string{"Origin": "https://a.test"}, 200,
			map[string]string{"Access-Control-Allow-Origin": "*", "Access-Control-Max-Age": "86400", "Vary": ""}},
		{"options without request method", "OPTIONS", map[string]string{"Origin": "https://a.test"}, 200,
			map[string]string{"Vary": "Origin", "Access-Control-Allow-Origin": ""}},
		{"preflight", "OPTIONS", map[string]string{"Origin": "https://a.test", "Access-Control-Request-Method": "POST"}, 204,
			map[string]string{
				"Vary":                         "Access-Control-Request-Method, Access-Control-Request-Headers, Origin",
				"Access-Control-Allow-Origin":  "*",
				"Access-Control-Allow-Methods": "*",
				"Access-Control-Allow-Headers": "*",
				"Access-Control-Max-Age":       "86400",
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(c.method, "/", nil)
			for k, v := range c.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			CORS(okHandler).ServeHTTP(rec, req)
			if rec.Code != c.status {
				t.Fatalf("status = %d, want %d", rec.Code, c.status)
			}
			for k, v := range c.want {
				if got := rec.Header().Get(k); got != v {
					t.Errorf("%s = %q, want %q", k, got, v)
				}
			}
		})
	}
}

// rules/security.md s16: the recoverer is registered first (outer) and the
// panic logger second (inner), so the panic is logged before it is swallowed.
// Mounted the other way round nothing is ever logged, and nothing looks wrong.
func TestRecovererOutsidePanicLogger(t *testing.T) {
	var buf bytes.Buffer
	previous := zlog.Logger
	zlog.Logger = zerolog.New(&buf)
	t.Cleanup(func() { zlog.Logger = previous })

	boom := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })
	h := httpx.Wrap(0, Recoverer(PanicLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpx.Late(w, func(h http.Header) { h.Set("X-Ratelimit-Limit", "1") })
		boom.ServeHTTP(w, r)
	}))))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != 500 || rec.Body.String() != "boom" || rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("got %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}
	if rec.Header().Get("X-Ratelimit-Limit") != "" {
		t.Fatal("limiter headers survived a panic; fiber set them after c.Next(), which never returned")
	}
	if !strings.Contains(buf.String(), `"event":"http.panic"`) || !strings.Contains(buf.String(), "boom") {
		t.Fatalf("panic not logged: %s", buf.String())
	}
}

func TestFavicon(t *testing.T) {
	file := filepath.Join(t.TempDir(), "favicon.png")
	if err := os.WriteFile(file, []byte("ICON"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := httpx.Wrap(0, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Favicon(file)(okHandler).ServeHTTP(w, r)
	}))
	run := func(method, target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
		return rec
	}

	if rec := run("GET", "/favicon.ico"); rec.Body.String() != "ICON" || rec.Header().Get("Content-Type") != "image/x-icon" ||
		rec.Header().Get("Cache-Control") != "public, max-age=31536000" {
		t.Fatalf("GET: %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}
	if rec := run("POST", "/favicon.ico"); rec.Code != 405 || rec.Header().Get("Allow") != "GET, HEAD, OPTIONS" {
		t.Fatalf("POST: %d %v", rec.Code, rec.Header())
	}
	if rec := run("OPTIONS", "/favicon.ico"); rec.Code != 200 {
		t.Fatalf("OPTIONS: %d", rec.Code)
	}
	if rec := run("GET", "/FAVICON.ICO"); rec.Body.String() != "ok" {
		t.Fatal("favicon matched case-insensitively; fiber compared the raw path")
	}
}

func TestTransport(t *testing.T) {
	h := Transport(10, okHandler)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("FOO", "/", nil))
	if rec.Code != 400 || rec.Body.String() != "Invalid http method" {
		t.Fatalf("unknown method: %d %q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/", strings.NewReader("more than ten bytes")))
	if rec.Code != 413 || rec.Header().Get("Connection") != "close" || rec.Body.String() != "Request Entity Too Large" {
		t.Fatalf("too large: %d %v %q", rec.Code, rec.Header(), rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/", strings.NewReader("small")))
	if rec.Code != 200 {
		t.Fatalf("small body: %d", rec.Code)
	}
}
