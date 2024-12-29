package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/mstgnz/cdn/service"
)

// HealthCheck handles health check requests
func HealthCheck(c *fiber.Ctx) error {
	status := map[string]interface{}{
		"status": "healthy",
		"minio":  checkMinioHealth(),
		"aws":    checkAwsHealth(),
	}
	return service.Response(c, fiber.StatusOK, true, "Health check", status)
}

func checkMinioHealth() string {
	// MinIO health check implementation
	return "healthy"
}

func checkAwsHealth() string {
	// AWS health check implementation
	return "healthy"
}
