package httpx

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The same outcomes as fasthttp's ParseByteRange for a 100-byte file.
func TestParseByteRange(t *testing.T) {
	cases := []struct {
		in         string
		start, end int
		ok         bool
	}{
		{"bytes=0-9", 0, 9, true},
		{"bytes=90-", 90, 99, true},
		{"bytes=-10", 90, 99, true},
		{"bytes=-500", 0, 99, true},
		{"bytes=50-500", 50, 99, true},
		{"bytes=100-", 0, 0, false},
		{"bytes=9-3", 0, 0, false},
		{"bytes=a-b", 0, 0, false},
		{"items=0-9", 0, 0, false},
		{"bytes=5", 0, 0, false},
	}
	for _, c := range cases {
		start, end, ok := parseByteRange(c.in, 100)
		if ok != c.ok || (ok && (start != c.start || end != c.end)) {
			t.Errorf("%q: got (%d,%d,%v), want (%d,%d,%v)", c.in, start, end, ok, c.start, c.end, c.ok)
		}
	}
}

func sendTestFile(t *testing.T, headers map[string]string, method string) *http.Response {
	t.Helper()
	path := filepath.Join(t.TempDir(), "p.png")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := Wrap(0, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		Late(w, func(h http.Header) { h.Set("X-Late", "1") })
		SendFile(w, r, path)
	}))
	req := httptest.NewRequest(method, "/", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

func TestSendFileFull(t *testing.T) {
	res := sendTestFile(t, nil, "GET")
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/png" ||
		res.Header.Get("Content-Length") != "10" || res.Header.Get("Accept-Ranges") != "bytes" ||
		res.Header.Get("Last-Modified") == "" || res.Header.Get("X-Late") != "1" {
		t.Fatalf("unexpected response %d %v", res.StatusCode, res.Header)
	}
}

// fasthttp reset the whole response for 304 and 416, so nothing set earlier by
// middleware survives; headers fiber's limiter set after c.Next() still do.
func TestSendFileNotModifiedResetsEarlierHeaders(t *testing.T) {
	res := sendTestFile(t, map[string]string{"If-Modified-Since": time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)}, "GET")
	if res.StatusCode != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", res.StatusCode)
	}
	if res.Header.Get("X-Content-Type-Options") != "" || res.Header.Get("X-Late") != "1" || len(res.Header) != 1 {
		t.Fatalf("304 headers: %v, want only the late one", res.Header)
	}
}

func TestSendFileIgnoresNonRFC1123Dates(t *testing.T) {
	res := sendTestFile(t, map[string]string{"If-Modified-Since": "Monday, 01-Jan-40 00:00:00 GMT"}, "GET")
	if res.StatusCode != 200 {
		t.Fatalf("status = %d, want 200: fasthttp only parsed RFC 1123", res.StatusCode)
	}
}

func TestSendFileBadRangeIsBare416(t *testing.T) {
	res := sendTestFile(t, map[string]string{"Range": "bytes=50-"}, "GET")
	if res.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("status = %d, want 416", res.StatusCode)
	}
	if res.Header.Get("X-Content-Type-Options") != "" || res.Header.Get("Accept-Ranges") != "" || res.Header.Get("X-Late") != "1" {
		t.Fatalf("416 headers: %v, want earlier ones reset and the late one kept", res.Header)
	}
}

func TestSendFileRangeHead(t *testing.T) {
	res := sendTestFile(t, map[string]string{"Range": "bytes=2-4"}, "HEAD")
	if res.StatusCode != http.StatusPartialContent || res.Header.Get("Content-Length") != "3" ||
		res.Header.Get("Content-Range") != "bytes 2-4/10" {
		t.Fatalf("unexpected %d %v", res.StatusCode, res.Header)
	}
}

func TestSendFileMissingIs404(t *testing.T) {
	rec := httptest.NewRecorder()
	SendFile(rec, httptest.NewRequest("GET", "/", nil), filepath.Join(t.TempDir(), "absent.png"))
	if rec.Code != http.StatusNotFound || !strings.HasPrefix(rec.Body.String(), "sendfile: file ") {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}
