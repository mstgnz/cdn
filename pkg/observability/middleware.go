package observability

import (
	"net/http"
	"strconv"
	"time"
)

// statusReporter is the part of the request's response writer this needs;
// wrappers that do not report it are unwrapped until one does.
type statusReporter interface{ Status() int }

func statusOf(w http.ResponseWriter) int {
	for {
		if s, ok := w.(statusReporter); ok {
			return s.Status()
		}
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return 0
		}
		w = u.Unwrap()
	}
}

// PrometheusMiddleware counts requests and their duration.
//
// The endpoint label is the matched ROUTE PATTERN (e.g. "/:bucket/*"), never the
// raw path: on a CDN the raw path is effectively unbounded, one series per
// object URL. endpoint returns the pattern after the request was routed; status
// comes from w, which must report the status it wrote.
func PrometheusMiddleware(endpoint func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)

			status := http.StatusOK
			if s := statusOf(w); s != 0 {
				status = s
			}
			label := endpoint(r)
			RequestCounter.WithLabelValues(r.Method, label, strconv.Itoa(status)).Inc()
			RequestDuration.WithLabelValues(r.Method, label).Observe(time.Since(start).Seconds())
		})
	}
}
