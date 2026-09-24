package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/mstgnz/cdn/pkg/httpx"
	"github.com/mstgnz/cdn/pkg/observability"
)

// Recoverer answers a panic the way fiber's recover middleware and default error
// handler did: 500, text/plain, the panic value as the body. It has to be mounted
// OUTSIDE PanicLogger (registered first), per rules/security.md section 16.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler {
				panic(rec) // net/http's own signal to drop the connection
			}
			if x := httpx.From(w); x != nil && x.Status() != 0 {
				return // too late for a response; PanicLogger already logged it
			}
			// The limiter headers were set after c.Next() in fiber, which a panic skipped.
			httpx.DropLate(w)
			msg := fmt.Sprintf("%v", rec)
			if err, ok := rec.(error); ok {
				msg = err.Error()
			}
			httpx.Text(w, http.StatusInternalServerError, msg)
		}()
		next.ServeHTTP(w, r)
	})
}

// PanicLogger records a panic with its stack and re-panics for Recoverer.
func PanicLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec != http.ErrAbortHandler {
					logger := observability.Logger()
					logger.Error().
						Str("event", "http.panic").
						Str("method", r.Method).
						Str("path", httpx.RawPath(r)).
						Str("panic", fmt.Sprintf("%v", rec)).
						Str("stack", string(debug.Stack())).
						Msg("handler panicked")
				}
				panic(rec)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
