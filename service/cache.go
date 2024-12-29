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
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}

	client := redis.NewClient(opt)
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, err
	}

	return &redisCache{
		client: client,
		logger: observability.Logger(),
	}, nil
}

func (c *redisCache) Get(key string) ([]byte, error) {
	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds()
		observability.StorageOperationDuration.WithLabelValues("cache_get", "redis").Observe(duration)
	}()

	return c.client.Get(context.Background(), key).Bytes()
}

func (c *redisCache) Set(key string, value []byte, expiration time.Duration) error {
	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds()
		observability.StorageOperationDuration.WithLabelValues("cache_set", "redis").Observe(duration)
	}()

	return c.client.Set(context.Background(), key, value, expiration).Err()
}

func (c *redisCache) Delete(key string) error {
	return c.client.Del(context.Background(), key).Err()
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
