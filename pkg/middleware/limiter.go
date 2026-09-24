package middleware

import (
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/tinylib/msgp/msgp"

	"github.com/mstgnz/cdn/pkg/config"
	"github.com/mstgnz/cdn/pkg/httpx"
)

// limiterItem is fiber's limiter entry. Its msgp encoding is what sits in Redis,
// kept byte-identical so counters carry over between the fiber and chi builds
// during a deploy or a rollback.
type limiterItem struct {
	currHits int
	prevHits int
	exp      uint64
}

func (z limiterItem) marshal() []byte {
	o := make([]byte, 0, 32)
	o = append(o, 0x83, 0xa8, 'c', 'u', 'r', 'r', 'H', 'i', 't', 's')
	o = msgp.AppendInt(o, z.currHits)
	o = append(o, 0xa8, 'p', 'r', 'e', 'v', 'H', 'i', 't', 's')
	o = msgp.AppendInt(o, z.prevHits)
	o = append(o, 0xa3, 'e', 'x', 'p')
	return msgp.AppendUint64(o, z.exp)
}

func (z *limiterItem) unmarshal(b []byte) error {
	n, b, err := msgp.ReadMapHeaderBytes(b)
	if err != nil {
		return err
	}
	for ; n > 0; n-- {
		var field []byte
		if field, b, err = msgp.ReadMapKeyZC(b); err != nil {
			return err
		}
		switch string(field) {
		case "currHits":
			z.currHits, b, err = msgp.ReadIntBytes(b)
		case "prevHits":
			z.prevHits, b, err = msgp.ReadIntBytes(b)
		case "exp":
			z.exp, b, err = msgp.ReadUint64Bytes(b)
		default:
			b, err = msgp.Skip(b)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// loadItem mirrors fiber's manager.get: a storage error or a miss is a fresh
// entry, and a decode error keeps whatever decoded before it.
func loadItem(storage *RedisStorage, key string) limiterItem {
	var it limiterItem
	raw, err := storage.Get(key)
	if err != nil || raw == nil {
		return it
	}
	_ = it.unmarshal(raw)
	return it
}

// fixedWindow is fiber's FixedWindow limiter. The per-process mutex does not
// make the Redis read-modify-write atomic across replicas; neither did fiber.
func fixedWindow(max int, window time.Duration, keyOf func(*http.Request) string,
	storage *RedisStorage, reached func(http.ResponseWriter)) func(http.Handler) http.Handler {
	var mu sync.Mutex
	expiration := uint64(math.Ceil(window.Seconds()))
	if expiration < 1 {
		expiration = 1
	}
	maxStr := strconv.Itoa(max)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyOf(r)

			mu.Lock()
			e := loadItem(storage, key)
			ts := uint64(time.Now().Unix())
			if e.exp == 0 {
				e.exp = ts + expiration
			} else if ts >= e.exp {
				e.currHits = 0
				e.exp = ts + expiration
			}
			e.currHits++
			resetInSec := e.exp - ts
			remaining := max - e.currHits
			_ = storage.Set(key, e.marshal(), time.Duration(expiration)*time.Second)
			mu.Unlock()

			if remaining < 0 {
				w.Header().Set("Retry-After", strconv.FormatUint(resetInSec, 10))
				reached(w)
				return
			}

			// fiber set these after c.Next(), so an outer limiter's values win.
			httpx.Late(w, func(h http.Header) {
				h.Set("X-RateLimit-Limit", maxStr)
				h.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
				h.Set("X-RateLimit-Reset", strconv.FormatUint(resetInSec, 10))
			})
			next.ServeHTTP(w, r)
		})
	}
}

// NewAdvancedRateLimiter limits by RateLimitKey with Redis storage. If Redis is
// unreachable at boot it retries briefly and then fails open instead of taking
// the service down; Cloudflare and nginx still throttle coarsely meanwhile.
func NewAdvancedRateLimiter(max int, duration time.Duration) func(http.Handler) http.Handler {
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
		return func(next http.Handler) http.Handler { return next }
	}

	return fixedWindow(max, duration, RateLimitKey, storage, func(w http.ResponseWriter) {
		httpx.JSON(w, http.StatusTooManyRequests, map[string]any{
			"success": false,
			"message": "Rate limit exceeded",
			"data":    map[string]any{"wait": duration.String()},
		})
	})
}

// DefaultAdvancedRateLimiter is the global limiter: RATE_LIMIT per minute.
// RATE_LIMIT_DURATION is not read here and never was.
func DefaultAdvancedRateLimiter() func(http.Handler) http.Handler {
	return NewAdvancedRateLimiter(config.GetEnvAsIntOrDefault("RATE_LIMIT", 100), time.Minute)
}
