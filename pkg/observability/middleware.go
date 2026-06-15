package observability

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
)

// PrometheusMiddleware middleware for monitoring Fiber requests
func PrometheusMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		method := c.Method()

		// Process request
		chainErr := c.Next()

		// Record metrics. Use the matched ROUTE PATTERN (e.g. "/:bucket/*")
		// as the endpoint label, never the raw request path. On a CDN the raw
		// path is effectively unbounded (every distinct object URL), which
		// would create one Prometheus series per URL: an unbounded memory leak
		// and a /metrics scrape that grows without limit. The route pattern
		// keeps cardinality bounded to the number of registered routes.
		endpoint := c.Route().Path
		duration := time.Since(start).Seconds()
		status := strconv.Itoa(c.Response().StatusCode())

		RequestCounter.WithLabelValues(method, endpoint, status).Inc()
		RequestDuration.WithLabelValues(method, endpoint).Observe(duration)

		return chainErr
	}
}
