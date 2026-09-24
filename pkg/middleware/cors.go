package middleware

import (
	"net/http"
	"strings"
)

// CORS is fiber's cors middleware for the one configuration this service used:
// AllowOrigins, AllowHeaders and AllowMethods "*", MaxAge 86400, no credentials.
// Browser panels preflight their uploads and dos copies CDN images onto a canvas,
// so every header here is load-bearing.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") == "" {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") == "" {
			appendVary(h, "Origin")
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodOptions {
			h.Set("Access-Control-Allow-Origin", "*")
			h.Set("Access-Control-Max-Age", "86400")
			next.ServeHTTP(w, r)
			return
		}
		appendVary(h, "Access-Control-Request-Method")
		appendVary(h, "Access-Control-Request-Headers")
		appendVary(h, "Origin")
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "*")
		h.Set("Access-Control-Allow-Headers", "*")
		h.Set("Access-Control-Max-Age", "86400")
		w.WriteHeader(http.StatusNoContent)
	})
}

// appendVary is fiber's Ctx.Append: one comma-joined header, no duplicates.
func appendVary(h http.Header, value string) {
	cur := h.Get("Vary")
	switch {
	case cur == "":
		h.Set("Vary", value)
	case cur != value && !strings.HasPrefix(cur, value+",") && !strings.HasSuffix(cur, " "+value) &&
		!strings.Contains(cur, " "+value+","):
		h.Set("Vary", cur+", "+value)
	}
}
