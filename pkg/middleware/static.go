package middleware

import (
	"net/http"
	"os"
	"strconv"

	"github.com/mstgnz/cdn/pkg/httpx"
)

// NoSniff stops user-uploaded content from being reinterpreted as HTML or script
// in the CDN origin's context.
func NoSniff(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

// Favicon is fiber's favicon middleware: /favicon.ico only, matched on the raw
// path and therefore case-sensitive. The file is read at boot; a missing one
// stops boot, as it did under fiber.
func Favicon(file string) func(http.Handler) http.Handler {
	icon, err := os.ReadFile(file)
	if err != nil {
		panic(err)
	}
	size := strconv.Itoa(len(icon))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if httpx.RawPath(r) != "/favicon.ico" {
				next.ServeHTTP(w, r)
				return
			}
			h := w.Header()
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				h.Set("Allow", "GET, HEAD, OPTIONS")
				h.Set("Content-Length", "0")
				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusOK)
				} else {
					w.WriteHeader(http.StatusMethodNotAllowed)
				}
				return
			}
			h.Set("Cache-Control", "public, max-age=31536000")
			h.Set("Content-Length", size)
			h.Set("Content-Type", "image/x-icon")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(icon)
		})
	}
}

// knownMethods are fiber's RequestMethods; anything else was refused before any
// middleware ran.
var knownMethods = map[string]bool{
	http.MethodGet: true, http.MethodHead: true, http.MethodPost: true, http.MethodPut: true,
	http.MethodDelete: true, http.MethodConnect: true, http.MethodOptions: true,
	http.MethodTrace: true, http.MethodPatch: true,
}

// Transport reproduces what fasthttp and fiber decided before the middleware
// chain: an unknown method is 400, a declared body over the limit is 413 with
// the connection closed. A body without a declared length is capped by
// MaxBytesReader instead.
func Transport(maxBody int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !knownMethods[r.Method] {
			httpx.Text(w, http.StatusBadRequest, httpx.TextInvalidMethod)
			return
		}
		if r.ContentLength > maxBody {
			w.Header().Set("Connection", "close")
			httpx.Text(w, http.StatusRequestEntityTooLarge, httpx.TextTooLarge)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		next.ServeHTTP(w, r)
	})
}
