package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Two fiber limiters set the same headers after c.Next(); the outer one ran last
// and its values were the ones sent.
func TestLateHeadersOuterWins(t *testing.T) {
	h := Wrap(0, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		Late(w, func(h http.Header) { h.Set("X-Limit", "outer") })
		Late(w, func(h http.Header) { h.Set("X-Limit", "inner"); h.Set("X-Inner", "1") })
		Text(w, http.StatusOK, "ok")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if got := rec.Header().Get("X-Limit"); got != "outer" {
		t.Fatalf("X-Limit = %q, want outer", got)
	}
	if rec.Header().Get("X-Inner") != "1" {
		t.Fatal("inner-only header lost")
	}
}

func TestDropLate(t *testing.T) {
	h := Wrap(0, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		Late(w, func(h http.Header) { h.Set("X-Limit", "1") })
		DropLate(w)
		Text(w, http.StatusOK, "ok")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Header().Get("X-Limit") != "" {
		t.Fatal("dropped late header was written")
	}
}

// A returned error becomes fiber's default error response, unless the handler
// already answered.
func TestHandlerError(t *testing.T) {
	rec := httptest.NewRecorder()
	Wrap(0, Handler(func(http.ResponseWriter, *http.Request) error {
		return errors.New("boom")
	})).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 500 || rec.Body.String() != "boom" || rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("got %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}

	rec = httptest.NewRecorder()
	Wrap(0, Handler(func(w http.ResponseWriter, _ *http.Request) error {
		Text(w, http.StatusTeapot, "answered")
		return errors.New("late")
	})).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusTeapot || rec.Body.String() != "answered" {
		t.Fatalf("an answered request was overwritten: %d %q", rec.Code, rec.Body.String())
	}
}

// JSON bodies go out with an explicit length; net/http alone would chunk them
// above 2 KB where fasthttp never did.
func TestJSONHasLength(t *testing.T) {
	rec := httptest.NewRecorder()
	JSON(rec, 201, map[string]any{"b": "<x>", "a": 1})
	// encoding/json escapes <, > and & as <, > and &.
	if want := "{\"a\":1,\"b\":\"\\u003cx\\u003e\"}"; rec.Body.String() != want {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if rec.Header().Get("Content-Length") != "27" || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("headers = %v", rec.Header())
	}
}
