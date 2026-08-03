package middleware

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/mstgnz/cdn/service"
)

var (
	// Only allow alphanumeric characters, hyphens, underscores, and dots
	keyPattern = regexp.MustCompile(`[^a-zA-Z0-9\-_\.]`)
)

// sanitizeKey cleans and secures Redis keys
func sanitizeKey(key string) string {
	// Replace spaces with hyphens
	key = strings.ReplaceAll(key, " ", "-")
	// Remove disallowed characters
	key = keyPattern.ReplaceAllString(key, "")
	// Limit maximum length
	if len(key) > 512 {
		key = key[:512]
	}
	return key
}

// RedisStorage implements fiber.Storage interface for Redis
type RedisStorage struct {
	cache service.CacheService
}

// NewRedisStorage creates a new Redis storage adapter
func NewRedisStorage() (*RedisStorage, error) {
	cache, err := service.NewCacheService()
	if err != nil {
		return nil, err
	}
	return &RedisStorage{cache: cache}, nil
}

// Get retrieves a value from Redis.
//
// fiber's Storage contract is that a key which does not exist returns
// (nil, nil), not an error. That distinction matters here because this adapter
// backs the rate limiter, where the first request from any client IP is a miss
// by definition: passing the miss up as an error both violated the contract and
// meant every rate-limited request produced a log line.
func (r *RedisStorage) Get(key string) ([]byte, error) {
	val, err := r.cache.Get(sanitizeKey(key))
	if errors.Is(err, service.ErrCacheMiss) {
		return nil, nil
	}
	return val, err
}

// Set stores a value in Redis
func (r *RedisStorage) Set(key string, val []byte, exp time.Duration) error {
	return r.cache.Set(sanitizeKey(key), val, exp)
}

// Delete removes a value from Redis
func (r *RedisStorage) Delete(key string) error {
	return r.cache.Delete(sanitizeKey(key))
}

// Reset clears all values from Redis
func (r *RedisStorage) Reset() error {
	return r.cache.FlushAll()
}

// Close closes the Redis connection
func (r *RedisStorage) Close() error {
	return nil
}
