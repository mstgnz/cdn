package service

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/mstgnz/cdn/pkg/config"
	"github.com/mstgnz/cdn/pkg/observability"
	"github.com/rs/zerolog"
)

type CacheService interface {
	Get(key string) ([]byte, error)
	Set(key string, value []byte, expiration time.Duration) error
	Delete(key string) error
	GetResizedImage(bucket, path string, width, height uint) ([]byte, error)
	SetResizedImage(bucket, path string, width, height uint, data []byte) error
	FlushAll() error
	Close() error
}

type redisCache struct {
	client *redis.Client
	logger zerolog.Logger
	hits   int64
	misses int64
}

// cacheConnectAttempts and cacheConnectTimeout bound the boot ping. Redis
// replays its RDB before it will answer commands, so on a cold start of the
// whole stack the API is dialing while Redis is still loading and the first ping
// comes back "LOADING Redis is loading the dataset in memory". That is a
// starting service, not a broken one, and retrying briefly gets a clean start
// instead of a warning operators learn to ignore.
//
// The timeout is deliberately short. Retrying is a cosmetic win, not a
// functional one: the service is returned and works either way, so blocking boot
// on an optional dependency has to stay cheap. Worst case here is roughly 7.5
// seconds (three 2s pings plus 1.5s of backoff) and only when Redis is a black
// hole; the case this exists for answers immediately with a LOADING error, which
// costs about 1.5 seconds in total.
const (
	cacheConnectAttempts = 3
	cacheConnectTimeout  = 2 * time.Second
)

// NewCacheService builds the cache client. It returns a usable CacheService even
// when the connection cannot be established, alongside an error describing why.
//
// The two return values are independent on purpose. A nil service is reserved
// for configuration that can never work (an unparseable REDIS_URL); a live
// server that is merely unreachable right now yields a usable service and an
// error. Callers should log the error and keep the service.
//
// This matters because the client does not dial here. go-redis connects lazily
// per command from its own pool and reconnects on its own, so a client whose
// first ping failed still works the moment Redis accepts connections. Discarding
// it converted a few seconds of RDB loading into a process that ran without a
// cache until someone restarted it, which is exactly what happened in production
// on 2026-08-03: two of three replicas came up cacheless and stayed that way.
func NewCacheService() (CacheService, error) {
	redisURL := config.GetEnvOrDefault("REDIS_URL", "redis://cdn-redis:6379")

	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL: %v", err)
	}

	client := redis.NewClient(opt)
	service := &redisCache{
		client: client,
		logger: observability.Logger(),
	}

	for attempt := 1; attempt <= cacheConnectAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), cacheConnectTimeout)
		err = client.Ping(ctx).Err()
		cancel()

		if err == nil {
			return service, nil
		}
		if attempt < cacheConnectAttempts {
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
		}
	}

	return service, fmt.Errorf("failed to connect to Redis after %d attempts: %v", cacheConnectAttempts, err)
}

// ErrCacheMiss reports that a key simply is not in the cache. It is the single
// most common outcome on this service (the rate limiter looks up a key per
// client IP, and the first request from any IP misses by definition), so callers
// need to be able to tell it apart from Redis actually being broken. Match it
// with errors.Is.
var ErrCacheMiss = errors.New("cache: key not found")

func (c *redisCache) Get(key string) ([]byte, error) {
	start := time.Now()
	ctx := context.Background()
	var err error
	var miss bool

	defer func() {
		duration := time.Since(start).Seconds()

		// A miss and a failure are different events and must not share a label:
		// with both counted as "miss" a Redis outage reads as a cold cache on the
		// dashboards instead of an incident. Only hits and misses feed the ratio;
		// a failed lookup is neither.
		status := "hit"
		switch {
		case miss:
			status = "miss"
			atomic.AddInt64(&c.misses, 1)
		case err != nil:
			status = "error"
		default:
			atomic.AddInt64(&c.hits, 1)
		}

		observability.CacheOperations.WithLabelValues("get", status).Inc()
		observability.CacheOperationDuration.WithLabelValues("get", status).Observe(duration)

		// Update hit ratio
		hits := atomic.LoadInt64(&c.hits)
		misses := atomic.LoadInt64(&c.misses)
		ratio := float64(0)
		if total := hits + misses; total > 0 {
			ratio = float64(hits) / float64(total)
		}
		observability.CacheHitRatio.WithLabelValues("get").Set(ratio)

		// Only a real failure is worth an error line. Logging misses here buried
		// the log under one ERROR per rate-limited request, which is both noise
		// and a steady stream of client IPs written to disk for no reason.
		if err != nil && !miss {
			c.logger.Error().Err(err).Str("key", key).Msg("Cache get failed")
		}
	}()

	val, err := c.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		miss = true
		return nil, fmt.Errorf("%w: %s", ErrCacheMiss, key)
	}

	if err == nil {
		observability.CacheSize.WithLabelValues("data").Add(float64(len(val)))
	}

	return val, err
}

func (c *redisCache) Set(key string, value []byte, expiration time.Duration) error {
	start := time.Now()
	ctx := context.Background()
	var err error

	defer func() {
		duration := time.Since(start).Seconds()
		status := "success"
		if err != nil {
			status = "error"
		}
		observability.CacheOperations.WithLabelValues("set", status).Inc()
		observability.CacheOperationDuration.WithLabelValues("set", status).Observe(duration)

		if err != nil {
			c.logger.Error().Err(err).Str("key", key).Msg("Cache set failed")
		}
	}()

	err = c.client.Set(ctx, key, value, expiration).Err()

	if err == nil {
		observability.CacheSize.WithLabelValues("data").Add(float64(len(value)))
	}

	return err
}

func (c *redisCache) Delete(key string) error {
	start := time.Now()
	ctx := context.Background()
	var err error

	defer func() {
		duration := time.Since(start).Seconds()
		status := "success"
		if err != nil {
			status = "error"
		}
		observability.CacheOperations.WithLabelValues("delete", status).Inc()
		observability.CacheOperationDuration.WithLabelValues("delete", status).Observe(duration)

		if err != nil {
			c.logger.Error().Err(err).Str("key", key).Msg("Cache delete failed")
		}
	}()

	err = c.client.Del(ctx, key).Err()
	return err
}

func (c *redisCache) GetResizedImage(bucket, path string, width, height uint) ([]byte, error) {
	key := fmt.Sprintf("resize:%s:%s:%d:%d", bucket, path, width, height)
	return c.Get(key)
}

func (c *redisCache) SetResizedImage(bucket, path string, width, height uint, data []byte) error {
	key := fmt.Sprintf("resize:%s:%s:%d:%d", bucket, path, width, height)
	// Cache for 24 hours
	return c.Set(key, data, 24*time.Hour)
}

func (c *redisCache) FlushAll() error {
	start := time.Now()
	ctx := context.Background()
	var err error

	defer func() {
		duration := time.Since(start).Seconds()
		status := "success"
		if err != nil {
			status = "error"
		}
		observability.CacheOperations.WithLabelValues("flush_all", status).Inc()
		observability.CacheOperationDuration.WithLabelValues("flush_all", status).Observe(duration)

		if err != nil {
			c.logger.Error().Err(err).Msg("Cache flush_all failed")
		}
	}()

	err = c.client.FlushAll(ctx).Err()
	return err
}

func (c *redisCache) Close() error {
	start := time.Now()
	var err error

	defer func() {
		duration := time.Since(start).Seconds()
		status := "success"
		if err != nil {
			status = "error"
		}
		observability.CacheOperations.WithLabelValues("close", status).Inc()
		observability.CacheOperationDuration.WithLabelValues("close", status).Observe(duration)

		if err != nil {
			c.logger.Error().Err(err).Msg("Cache close failed")
		}
	}()

	err = c.client.Close()
	return err
}
