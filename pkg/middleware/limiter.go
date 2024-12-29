package middleware

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

// RateLimiterConfig default rate limiter configuration
var RateLimiterConfig = limiter.Config{
	Max:        100,             // Maximum number of requests per duration
	Expiration: 1 * time.Minute, // Duration for rate limit window
	KeyGenerator: func(c *fiber.Ctx) string {
		return c.IP() // Use client IP as key
	},
	LimitReached: func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
			"status":  false,
			"message": "Rate limit exceeded. Please try again later.",
		})
	},
	SkipFailedRequests:     false, // Count failed requests
	SkipSuccessfulRequests: false, // Count successful requests
}

// NewRateLimiter creates a new rate limiter middleware with custom config
func NewRateLimiter(max int, duration time.Duration) fiber.Handler {
	config := RateLimiterConfig
	config.Max = max
	config.Expiration = duration
	return limiter.New(config)
}

// DefaultRateLimiter creates a rate limiter with default configuration
func DefaultRateLimiter() fiber.Handler {
	return limiter.New(RateLimiterConfig)
}
