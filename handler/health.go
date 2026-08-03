package handler

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/minio/minio-go/v7"
	"github.com/mstgnz/cdn/pkg/observability"
	"github.com/mstgnz/cdn/service"
)

type HealthChecker struct {
	minioClient *minio.Client
	awsService  service.AwsService
	cache       service.CacheService
}

func NewHealthChecker(minioClient *minio.Client, awsService service.AwsService, cache service.CacheService) *HealthChecker {
	return &HealthChecker{
		minioClient: minioClient,
		awsService:  awsService,
		cache:       cache,
	}
}

// isCoreHealthy reports whether the always-on core dependencies (MinIO object
// store and Redis cache) are healthy. Optional integrations such as AWS S3 are
// intentionally excluded so they cannot degrade the overall health signal.
func isCoreHealthy(minioHealth, cacheHealth string) bool {
	return minioHealth == "healthy" && cacheHealth == "healthy"
}

// HealthCheck handles health check requests
func (h *HealthChecker) HealthCheck(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	minioHealth := h.checkMinioHealth(ctx)
	awsHealth := h.checkAwsHealth(ctx)
	cacheHealth := h.checkCacheHealth(ctx)

	// AWS S3 is an optional, opt-in integration (only used when a request asks
	// for aws_upload). Its status is reported for visibility but it must NOT
	// flip the overall health, otherwise an unconfigured/placeholder AWS makes
	// the whole CDN report "degraded" (503) while MinIO + cache are fine, and
	// uptime monitors flap on it. Overall health = the always-on core only.
	overallStatus := "healthy"
	statusCode := fiber.StatusOK

	if !isCoreHealthy(minioHealth, cacheHealth) {
		overallStatus = "degraded"
		statusCode = fiber.StatusServiceUnavailable
	}

	data := map[string]any{
		"status": overallStatus,
		"services": map[string]any{
			"minio": minioHealth,
			"aws":   awsHealth,
			"cache": cacheHealth,
		},
		"timestamp": time.Now().UTC(),
	}

	return service.Response(c, statusCode, true, "Health check", data)
}

func (h *HealthChecker) checkMinioHealth(ctx context.Context) string {
	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds()
		observability.ServiceHealthCheckDuration.WithLabelValues("minio").Observe(duration)
		observability.LastHealthCheckTimestamp.WithLabelValues("minio").Set(float64(time.Now().Unix()))
	}()

	if _, err := h.minioClient.ListBuckets(ctx); err != nil {
		observability.ServiceHealth.WithLabelValues("minio").Set(0)
		return "unhealthy: " + err.Error()
	}
	observability.ServiceHealth.WithLabelValues("minio").Set(1)
	return "healthy"
}

func (h *HealthChecker) checkAwsHealth(ctx context.Context) string {
	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds()
		observability.ServiceHealthCheckDuration.WithLabelValues("aws").Observe(duration)
		observability.LastHealthCheckTimestamp.WithLabelValues("aws").Set(float64(time.Now().Unix()))
	}()

	if _, err := h.awsService.ListBuckets(); err != nil {
		observability.ServiceHealth.WithLabelValues("aws").Set(0)
		return "unhealthy: " + err.Error()
	}
	observability.ServiceHealth.WithLabelValues("aws").Set(1)
	return "healthy"
}

func (h *HealthChecker) checkCacheHealth(ctx context.Context) string {
	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds()
		observability.ServiceHealthCheckDuration.WithLabelValues("cache").Observe(duration)
		observability.LastHealthCheckTimestamp.WithLabelValues("cache").Set(float64(time.Now().Unix()))
	}()

	// A nil cache means NewCacheService could not build a client at all, which
	// only happens for a REDIS_URL that cannot be parsed. Report it instead of
	// dereferencing: CacheService is an interface, so a nil one panics on the
	// first method call, and this runs behind the recover middleware where that
	// surfaces as a bare 500 with no indication of the cause.
	if h.cache == nil {
		observability.ServiceHealth.WithLabelValues("cache").Set(0)
		return "unhealthy: cache not configured"
	}

	testKey := "health:test"
	testValue := []byte("test")

	// Try to set a test value
	if err := h.cache.Set(testKey, testValue, time.Second); err != nil {
		observability.ServiceHealth.WithLabelValues("cache").Set(0)
		return "unhealthy: set failed - " + err.Error()
	}

	// Try to get the test value
	if _, err := h.cache.Get(testKey); err != nil {
		observability.ServiceHealth.WithLabelValues("cache").Set(0)
		return "unhealthy: get failed - " + err.Error()
	}

	observability.ServiceHealth.WithLabelValues("cache").Set(1)
	return "healthy"
}
