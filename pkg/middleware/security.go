package middleware

import (
	"crypto/subtle"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

// SecurityConfig represents security middleware configuration
type SecurityConfig struct {
	Token              string
	RateLimit          RateLimitConfig
	CORS               CORSConfig
	TrustedProxies     []string
	MaxRequestBodySize int
	RequestTimeout     time.Duration
}

type RateLimitConfig struct {
	MaxRequests    int
	WindowDuration time.Duration
	ExemptedPaths  []string
	UploadLimit    int
	UploadWindow   time.Duration
	DeleteLimit    int
	DeleteWindow   time.Duration
}

type CORSConfig struct {
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string
	ExposeHeaders    []string
	MaxAge           int
	AllowCredentials bool
}

// DefaultSecurityConfig returns default security configuration
func DefaultSecurityConfig() SecurityConfig {
	return SecurityConfig{
		RateLimit: RateLimitConfig{
			MaxRequests:    100,
			WindowDuration: time.Minute,
			ExemptedPaths: []string{
				"/health",
				"/metrics",
			},
			UploadLimit:  10,
			UploadWindow: time.Minute,
			DeleteLimit:  20,
			DeleteWindow: time.Minute,
		},
		CORS: CORSConfig{
			AllowOrigins:     []string{"*"},
			AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
			ExposeHeaders:    []string{"Content-Length"},
			MaxAge:           86400,
			AllowCredentials: true,
		},
		TrustedProxies:     []string{"127.0.0.1"},
		MaxRequestBodySize: 100 * 1024 * 1024, // 100MB
		RequestTimeout:     30 * time.Second,
	}
}

// SecurityMiddleware returns security middleware chain
func SecurityMiddleware(cfg SecurityConfig) []fiber.Handler {
	return []fiber.Handler{
		// CORS middleware
		cors.New(cors.Config{
			AllowOrigins:     strings.Join(cfg.CORS.AllowOrigins, ","),
			AllowMethods:     strings.Join(cfg.CORS.AllowMethods, ","),
			AllowHeaders:     strings.Join(cfg.CORS.AllowHeaders, ","),
			ExposeHeaders:    strings.Join(cfg.CORS.ExposeHeaders, ","),
			MaxAge:           cfg.CORS.MaxAge,
			AllowCredentials: cfg.CORS.AllowCredentials,
		}),

		// Rate limiter middleware
		limiter.New(limiter.Config{
			Max:        cfg.RateLimit.MaxRequests,
			Expiration: cfg.RateLimit.WindowDuration,
			KeyGenerator: func(c *fiber.Ctx) string {
				return c.IP()
			},
			LimitReached: func(c *fiber.Ctx) error {
				return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
					"error": "Rate limit exceeded",
				})
			},
			SkipFailedRequests:     false,
			SkipSuccessfulRequests: false,
			Next: func(c *fiber.Ctx) bool {
				path := c.Path()
				for _, exemptedPath := range cfg.RateLimit.ExemptedPaths {
					if strings.HasPrefix(path, exemptedPath) {
						return true
					}
				}
				return false
			},
		}),

		// Upload rate limiter
		limiter.New(limiter.Config{
			Max:        cfg.RateLimit.UploadLimit,
			Expiration: cfg.RateLimit.UploadWindow,
			KeyGenerator: func(c *fiber.Ctx) string {
				return c.IP() + ":upload"
			},
			LimitReached: func(c *fiber.Ctx) error {
				return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
					"error": "Upload rate limit exceeded",
				})
			},
			Next: func(c *fiber.Ctx) bool {
				return !strings.HasPrefix(c.Path(), "/upload")
			},
		}),

		// Token authentication middleware
		func(c *fiber.Ctx) error {
			token := c.Get("Authorization")
			if token == "" {
				token = c.Query("token")
			}

			if subtle.ConstantTimeCompare([]byte(token), []byte(cfg.Token)) != 1 {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
					"error": "Invalid token",
				})
			}

			return c.Next()
		},
	}
}

// UploadLimiter returns specific rate limiter for upload endpoints
func UploadLimiter(cfg RateLimitConfig) fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        cfg.UploadLimit,
		Expiration: cfg.UploadWindow,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP() + ":upload"
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "Upload rate limit exceeded",
			})
		},
	})
}

// DeleteLimiter returns specific rate limiter for delete endpoints
func DeleteLimiter(cfg RateLimitConfig) fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        cfg.DeleteLimit,
		Expiration: cfg.DeleteWindow,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP() + ":delete"
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "Delete rate limit exceeded",
			})
		},
	})
}

// RateLimitKey generates a unique key for rate limiting based on IP and token
func RateLimitKey(c *fiber.Ctx) string {
	// Get client IP
	ip := c.IP()

	// Get token from header
	token := c.Get("Authorization")
	token = strings.TrimPrefix(token, "Bearer ")

	// If no token, use IP only
	if token == "" {
		return ip
	}

	// Combine IP and token for the key
	return fmt.Sprintf("%s:%s", ip, token)
}

// NewAdvancedRateLimiter creates a new rate limiter middleware with Redis storage
func NewAdvancedRateLimiter(max int, duration time.Duration) fiber.Handler {
	storage, err := NewRedisStorage()
	if err != nil {
		panic(err)
	}

	config := limiter.Config{
		Max:          max,
		Expiration:   duration,
		KeyGenerator: RateLimitKey,
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"status":  false,
				"message": "Rate limit exceeded",
				"data": fiber.Map{
					"wait": duration.String(),
				},
			})
		},
		Storage: storage,
	}

	return limiter.New(config)
}

// DefaultAdvancedRateLimiter returns a default rate limiter middleware (100 requests per minute)
func DefaultAdvancedRateLimiter() fiber.Handler {
	return NewAdvancedRateLimiter(100, time.Minute)
}
