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
		path := c.Path()

		// Process request
		chainErr := c.Next()

		// Record metrics
		duration := time.Since(start).Seconds()
		status := strconv.Itoa(c.Response().StatusCode())

		RequestCounter.WithLabelValues(method, path, status).Inc()
		RequestDuration.WithLabelValues(method, path).Observe(duration)

		return chainErr
	}
}
