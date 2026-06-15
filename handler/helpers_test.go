package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// apiResp mirrors the envelope produced by service.Response.
type apiResp struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// doReq runs a request with no body through the fiber app (no timeout).
func doReq(t *testing.T, app *fiber.App, method, target string) *http.Response {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest(method, target, nil), -1)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, target, err)
	}
	return resp
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
