package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/mstgnz/cdn/pkg/httpx"
)

// testRouter mounts routes the way cmd does: raw-path preparation for fiber's
// parameter semantics and the response writer the handlers expect.
func testRouter(routes func(r chi.Router)) http.Handler {
	r := chi.NewRouter()
	r.Use(httpx.Prepare)
	routes(r)
	return httpx.Wrap(0, r)
}

// serve runs a request through h and returns the response.
func serve(h http.Handler, req *http.Request) *http.Response {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

// apiResp mirrors the envelope produced by service.Response.
type apiResp struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// doReq runs a request with no body through the handler.
func doReq(t *testing.T, h http.Handler, method, target string) *http.Response {
	t.Helper()
	return serve(h, httptest.NewRequest(method, target, nil))
}

// decodeBody parses the JSON envelope from a response.
func decodeBody(t *testing.T, resp *http.Response) apiResp {
	t.Helper()
	var out apiResp
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("failed to decode body %q: %v", string(body), err)
	}
	return out
}
