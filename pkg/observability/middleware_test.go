package observability

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
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
	app := fiber.New()
	app.Use(PrometheusMiddleware())
	app.Get("/:bucket/*", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	const routeLabel = "/:bucket/*"
	before := counterValue(t, RequestCounter.WithLabelValues("GET", routeLabel, "200"))

	// Two requests to DIFFERENT raw URLs that match the SAME route pattern.
	for _, url := range []string{"/photos/a/b/one.jpg", "/avatars/x/two.png"} {
		resp, err := app.Test(httptest.NewRequest("GET", url, nil))
		if err != nil {
			t.Fatalf("request to %s failed: %v", url, err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("expected 200 for %s, got %d", url, resp.StatusCode)
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
	app := fiber.New()
	app.Get("/metrics", MetricsHandler)

	resp, err := app.Test(httptest.NewRequest("GET", "/metrics", nil))
	if err != nil {
		t.Fatalf("metrics request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 from /metrics, got %d", resp.StatusCode)
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
