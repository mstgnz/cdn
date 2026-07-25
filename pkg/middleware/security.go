package middleware

import (
	"crypto/subtle"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/mstgnz/cdn/pkg/config"
	"github.com/mstgnz/cdn/service"
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
				return ClientIP(c)
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
				return ClientIP(c) + ":upload"
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
			return ClientIP(c) + ":upload"
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
			return ClientIP(c) + ":delete"
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "Delete rate limit exceeded",
			})
		},
	})
}

// ClientIP returns the real originating client IP for rate limiting.
//
// Cloudflare sits in front of this service and sets CF-Connecting-IP to the
// originating client; a browser cannot forge it through Cloudflare. We do NOT
// trust X-Real-IP (the docker nginx hop overwrites it with the upstream hop's
// address) nor the leftmost X-Forwarded-For entry (attacker-controllable —
// Cloudflare appends the real client after any client-supplied value). The TCP
// peer (c.IP()) is only a degenerate fallback for non-Cloudflare/internal
// access; without this helper every request collapses onto the proxy IP and
// shares one rate-limit bucket.
//
// Trust note: CF-Connecting-IP is only authoritative while the origin is
// reachable ONLY via Cloudflare. Lock the origin firewall to Cloudflare IP
// ranges so an attacker cannot hit the origin directly and spoof this header.
func ClientIP(c *fiber.Ctx) string {
	if ip := strings.TrimSpace(c.Get("CF-Connecting-IP")); ip != "" {
		return ip
	}
	return c.IP()
}

// RateLimitKey derives the rate-limit bucket for a request from the real client
// IP and the *verified* identity of its credential.
//
// Two properties matter here, and the previous "<ip>:<raw bearer>" key had
// neither:
//
//  1. No secret in the keyspace. The key is built from the identity a credential
//     resolves to (the general token, or a bucket name), never from the
//     credential itself, so token material never reaches Redis.
//  2. An unverified credential cannot mint a fresh counter. Because the old key
//     trusted the raw header, a client sending a different random token on every
//     request got a brand-new counter each time and was effectively never rate
//     limited. Anything that does not authenticate now shares the plain per-IP
//     bucket, which is the limit it was always meant to be under.
//
// Bucket names are validated at load time to lowercase letters, digits and '-',
// so a bucket name can never inject a separator into the key.
//
// The rate limiter is mounted before the auth middleware, so resolving here is
// deliberately read-only: rejecting a bad credential stays the auth middleware's
// job, this only decides which counter the request belongs to.
func RateLimitKey(c *fiber.Ctx) string {
	ip := ClientIP(c)

	p, err := service.ResolvePrincipal(c)
	if err != nil {
		return ip
	}
	if p.Scoped {
		return ip + "|bucket:" + p.Bucket
	}
	return ip + "|general"
}

// NewAdvancedRateLimiter creates a new rate limiter middleware with Redis
// storage. If Redis is unreachable at boot it retries briefly and then fails
// open (serves without rate limiting) instead of crashing the process, so a
// transient Redis outage cannot take down the whole service. Cloudflare/nginx
// still provide a coarse protection layer in that window.
func NewAdvancedRateLimiter(max int, duration time.Duration) fiber.Handler {
	var storage *RedisStorage
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		if storage, err = NewRedisStorage(); err == nil {
			break
		}
		slog.Warn("rate limiter: redis unavailable, retrying", "attempt", attempt, "error", err)
		time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
	}
	if err != nil || storage == nil {
		slog.Error("rate limiter: redis unavailable after retries, disabling rate limiting (fail-open)", "error", err)
		return func(c *fiber.Ctx) error { return c.Next() }
	}

	config := limiter.Config{
		Max:          max,
		Expiration:   duration,
		KeyGenerator: RateLimitKey,
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"success": false,
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
	return NewAdvancedRateLimiter(config.GetEnvAsIntOrDefault("RATE_LIMIT", 100), time.Minute)
}
