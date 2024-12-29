package observability

import (
	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// RequestCounter counts all HTTP requests
	RequestCounter = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cdn_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "endpoint", "status"},
	)

	// RequestDuration measures request duration
	RequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cdn_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "endpoint"},
	)

	// ImageProcessingDuration measures image processing duration
	ImageProcessingDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cdn_image_processing_duration_seconds",
			Help:    "Image processing duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation"},
	)

	// StorageOperationDuration measures storage operation duration
	StorageOperationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cdn_storage_operation_duration_seconds",
			Help:    "Storage operation duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation", "provider"},
	)
)

// MetricsHandler HTTP handler for Prometheus metrics
func MetricsHandler(c *fiber.Ctx) error {
	metrics, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Error collecting metrics")
	}

	data := ""
	for _, mf := range metrics {
		data += mf.String() + "\n"
	}

	c.Set("Content-Type", "text/plain")
	return c.SendString(data)
}
