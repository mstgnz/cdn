package httpx

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// Bodies of the plain-text answers fiber produced itself.
const (
	TextMethodNotAllowed = "Method Not Allowed"
	TextUpgradeRequired  = "Upgrade Required"
	TextUnauthorized     = "Unauthorized"
	TextInvalidMethod    = "Invalid http method"
	TextTooLarge         = "Request Entity Too Large"
)

// Handler is a handler that may return an error, as fiber handlers did. A
// returned error becomes fiber's default error response: 500, text/plain, the
// error text.
type Handler func(http.ResponseWriter, *http.Request) error

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h(w, r); err != nil {
		if x := From(w); x != nil && x.Status() != 0 {
			return
		}
		Text(w, http.StatusInternalServerError, err.Error())
	}
}

// JSON writes v as fiber's c.JSON did: encoding/json.Marshal (sorted map keys,
// HTML escaped, no trailing newline) and Content-Type application/json.
func JSON(w http.ResponseWriter, status int, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		// fiber returned the error to its default handler.
		Text(w, http.StatusInternalServerError, err.Error())
		return
	}
	Bytes(w, status, "application/json", raw)
}

// Text writes a plain-text body the way fiber's default error handler did.
func Text(w http.ResponseWriter, status int, body string) {
	Bytes(w, status, "text/plain; charset=utf-8", []byte(body))
}

// Bytes writes a complete body with an explicit length. net/http would switch to
// chunked encoding above 2 KB, which fasthttp never did for a buffered body.
func Bytes(w http.ResponseWriter, status int, contentType string, body []byte) {
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
