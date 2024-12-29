package service

import (
	"context"
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
}

type redisCache struct {
	client *redis.Client
	logger zerolog.Logger
	hits   int64
	misses int64
}

func NewCacheService(redisURL string) (CacheService, error) {
	if redisURL == "" {
		redisURL = config.GetEnvOrDefault("REDIS_URL", "redis://localhost:6379")
	}

	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL: %v", err)
	}

	client := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %v", err)
	}

	return &redisCache{
		client: client,
		logger: observability.Logger(),
	}, nil
}

func (c *redisCache) Get(key string) ([]byte, error) {
	start := time.Now()
	ctx := context.Background()
	var err error

	defer func() {
		duration := time.Since(start).Seconds()
		status := "hit"
		if err != nil {
			status = "miss"
			atomic.AddInt64(&c.misses, 1)
		} else {
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

		if err != nil {
			c.logger.Error().Err(err).Str("key", key).Msg("Cache get failed")
		}
	}()

	val, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("key not found: %s", key)
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
