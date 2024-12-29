package handler

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/minio/minio-go/v7"
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

// HealthCheck handles health check requests
func (h *HealthChecker) HealthCheck(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	minioHealth := h.checkMinioHealth(ctx)
	awsHealth := h.checkAwsHealth(ctx)
	cacheHealth := h.checkCacheHealth(ctx)

	overallStatus := "healthy"
	if minioHealth != "healthy" || awsHealth != "healthy" || cacheHealth != "healthy" {
		overallStatus = "degraded"
		c.Status(fiber.StatusServiceUnavailable)
	}

	status := map[string]interface{}{
		"status": overallStatus,
		"services": map[string]interface{}{
			"minio": minioHealth,
			"aws":   awsHealth,
			"cache": cacheHealth,
		},
		"timestamp": time.Now().UTC(),
	}

	return service.Response(c, fiber.StatusOK, true, "Health check", status)
}

func (h *HealthChecker) checkMinioHealth(ctx context.Context) string {
	if _, err := h.minioClient.ListBuckets(ctx); err != nil {
		return "unhealthy: " + err.Error()
	}
	return "healthy"
}

func (h *HealthChecker) checkAwsHealth(ctx context.Context) string {
	if _, err := h.awsService.ListBuckets(); err != nil {
		return "unhealthy: " + err.Error()
	}
	return "healthy"
}

func (h *HealthChecker) checkCacheHealth(ctx context.Context) string {
	testKey := "health:test"
	testValue := []byte("test")

	// Try to set a test value
	if err := h.cache.Set(testKey, testValue, time.Second); err != nil {
		return "unhealthy: set failed - " + err.Error()
	}

	// Try to get the test value
	if _, err := h.cache.Get(testKey); err != nil {
		return "unhealthy: get failed - " + err.Error()
	}

	return "healthy"
}
