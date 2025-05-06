package middleware

import (
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

// Get retrieves a value from Redis
func (r *RedisStorage) Get(key string) ([]byte, error) {
	return r.cache.Get(sanitizeKey(key))
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
