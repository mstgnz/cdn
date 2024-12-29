package service

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
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
}

func NewCacheService(redisURL string) (CacheService, error) {
	if redisURL == "" {
		redisURL = GetEnv("REDIS_URL")
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
		observability.StorageOperationDuration.WithLabelValues("cache_get", "redis").Observe(duration)
		if err != nil {
			c.logger.Error().Err(err).Str("key", key).Msg("Cache get failed")
		}
	}()

	val, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	return val, err
}

func (c *redisCache) Set(key string, value []byte, expiration time.Duration) error {
	start := time.Now()
	ctx := context.Background()
	var err error

	defer func() {
		duration := time.Since(start).Seconds()
		observability.StorageOperationDuration.WithLabelValues("cache_set", "redis").Observe(duration)
		if err != nil {
			c.logger.Error().Err(err).Str("key", key).Msg("Cache set failed")
		}
	}()

	err = c.client.Set(ctx, key, value, expiration).Err()
	return err
}

func (c *redisCache) Delete(key string) error {
	ctx := context.Background()
	var err error

	defer func() {
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
