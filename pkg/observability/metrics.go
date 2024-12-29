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

	// Health Check Metrics
	ServiceHealth = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "service_health_status",
			Help: "Current health status of services (1 for healthy, 0 for unhealthy)",
		},
		[]string{"service"},
	)

	ServiceHealthCheckDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "service_health_check_duration_seconds",
			Help:    "Duration of health checks in seconds",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
		},
		[]string{"service"},
	)

	LastHealthCheckTimestamp = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "service_last_health_check_timestamp",
			Help: "Timestamp of the last health check",
		},
		[]string{"service"},
	)

	// Worker Pool Metrics
	WorkerPoolQueueSize = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "worker_pool_queue_size",
			Help: "Current number of jobs in the worker pool queue",
		},
	)

	WorkerPoolActiveWorkers = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "worker_pool_active_workers",
			Help: "Current number of active workers in the pool",
		},
	)

	WorkerJobProcessingDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "worker_job_processing_duration_seconds",
			Help:    "Duration of job processing in seconds",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
		},
		[]string{"status"},
	)

	WorkerJobRetries = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "worker_job_retries_total",
			Help: "Total number of job retries",
		},
		[]string{"job_type"},
	)

	// Batch Processor Metrics
	BatchProcessorQueueSize = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "batch_processor_queue_size",
			Help: "Current number of items in the batch processor queue",
		},
	)

	BatchProcessingDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "batch_processing_duration_seconds",
			Help:    "Duration of batch processing in seconds",
			Buckets: []float64{.01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"status"},
	)

	BatchItemsProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "batch_items_processed_total",
			Help: "Total number of items processed by the batch processor",
		},
		[]string{"status"},
	)

	BatchRetries = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "batch_retries_total",
			Help: "Total number of batch retries",
		},
	)

	// Cache Metrics
	CacheOperations = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cache_operations_total",
			Help: "The total number of cache operations",
		},
		[]string{"operation", "status"},
	)

	CacheOperationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cache_operation_duration_seconds",
			Help:    "Duration of cache operations in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation", "status"},
	)

	CacheSize = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cache_size_bytes",
			Help: "Current size of cached data in bytes",
		},
		[]string{"type"},
	)

	CacheHitRatio = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cache_hit_ratio",
			Help: "Cache hit ratio",
		},
		[]string{"operation"},
	)

	// Circuit Breaker metrics
	CircuitBreakerState = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "circuit_breaker_state",
			Help: "Current state of circuit breaker (0: Closed, 1: Open, 2: Half-Open)",
		},
		[]string{"name"},
	)

	CircuitBreakerFailures = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "circuit_breaker_failures",
			Help: "Number of consecutive failures in circuit breaker",
		},
		[]string{"name"},
	)

	CircuitBreakerSuccesses = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "circuit_breaker_successes",
			Help: "Number of consecutive successes in circuit breaker",
		},
		[]string{"name"},
	)

	CircuitBreakerRequests = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "circuit_breaker_requests_total",
			Help: "Total number of requests handled by circuit breaker",
		},
		[]string{"name", "result"},
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
