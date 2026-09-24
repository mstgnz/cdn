package observability

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/mstgnz/cdn/pkg/httpx"
)

// counterValue reads the current value of a counter child without pulling in
// the testutil package (which would add a new test-only dependency).
func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		t.Fatalf("failed to read counter: %v", err)
	}
	return m.GetCounter().GetValue()
}

// TestPrometheusMiddleware_UsesRoutePattern verifies that the request counter
// is labelled with the matched route pattern (bounded cardinality), not the
// raw request path (unbounded). This is the regression guard for the metric
// cardinality leak that grew memory and broke /metrics.
func TestPrometheusMiddleware_UsesRoutePattern(t *testing.T) {
	const routeLabel = "/:bucket/*"
	r := chi.NewRouter()
	r.Use(PrometheusMiddleware(func(req *http.Request) string {
		if chi.RouteContext(req.Context()).RoutePattern() == "/{bucket}/*" {
			return routeLabel
		}
		return "/"
	}))
	r.Get("/{bucket}/*", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	app := httpx.Wrap(0, r)

	before := counterValue(t, RequestCounter.WithLabelValues("GET", routeLabel, "200"))

	// Two requests to DIFFERENT raw URLs that match the SAME route pattern.
	for _, url := range []string{"/photos/a/b/one.jpg", "/avatars/x/two.png"} {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, httptest.NewRequest("GET", url, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for %s, got %d", url, rec.Code)
		}
	}

	after := counterValue(t, RequestCounter.WithLabelValues("GET", routeLabel, "200"))
	if got := after - before; got != 2 {
		t.Fatalf("expected route-pattern counter to increase by 2, got %v", got)
	}

	// The raw URLs must NOT have produced their own series.
	if v := counterValue(t, RequestCounter.WithLabelValues("GET", "/photos/a/b/one.jpg", "200")); v != 0 {
		t.Fatalf("raw path leaked into a metric series: got %v", v)
	}
}

// TestMetricsHandler_ServesExpositionFormat verifies /metrics returns 200 and
// the standard Prometheus text format (not the previous proto debug string,
// and not a 500 on a partial gather error).
func TestMetricsHandler_ServesExpositionFormat(t *testing.T) {
	rec := httptest.NewRecorder()
	MetricsHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	resp := rec.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /metrics, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Content-Length") == "" {
		t.Fatal("exposition sent without a Content-Length; fiber's adaptor always set one")
	}

	body, _ := io.ReadAll(resp.Body)
	out := string(body)
	// A registered counter must appear with its HELP line in text format.
	if !strings.Contains(out, "cdn_http_requests_total") {
		t.Fatalf("expected exposition output to contain cdn_http_requests_total, got:\n%s", out)
	}
	if !strings.Contains(out, "# HELP") {
		t.Fatalf("expected Prometheus text format with # HELP lines, got:\n%s", out)
	}
}
