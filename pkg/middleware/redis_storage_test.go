package middleware

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/mstgnz/cdn/service"
)

// stubCache lets the adapter be exercised without Redis. Only Get matters here;
// the rest satisfy the interface.
type stubCache struct {
	val []byte
	err error
}

func (s stubCache) Get(string) ([]byte, error)              { return s.val, s.err }
func (s stubCache) Set(string, []byte, time.Duration) error { return nil }
func (s stubCache) Delete(string) error                     { return nil }
func (s stubCache) FlushAll() error                         { return nil }
func (s stubCache) Close() error                            { return nil }
func (s stubCache) GetResizedImage(string, string, uint, uint) ([]byte, error) {
	return nil, nil
}
func (s stubCache) SetResizedImage(string, string, uint, uint, []byte) error { return nil }

// fiber's Storage contract says a missing key is (nil, nil), not an error. This
// adapter backs the rate limiter, where the first request from any client IP is
// a miss by definition, so getting this wrong meant both a contract violation
// and an ERROR log line per rate-limited request.
func TestRedisStorageReportsMissAsEmptyNotError(t *testing.T) {
	s := &RedisStorage{cache: stubCache{err: fmt.Errorf("%w: some-key", service.ErrCacheMiss)}}

	val, err := s.Get("some-key")

	if err != nil {
		t.Fatalf("a cache miss must not surface as an error, got %v", err)
	}
	if val != nil {
		t.Fatalf("a cache miss must return no value, got %q", val)
	}
}

// A genuine Redis failure is a different thing and has to keep propagating,
// otherwise an outage would look like a permanently cold cache.
func TestRedisStoragePropagatesRealFailures(t *testing.T) {
	boom := errors.New("connection refused")
	s := &RedisStorage{cache: stubCache{err: boom}}

	if _, err := s.Get("some-key"); !errors.Is(err, boom) {
		t.Fatalf("want the underlying error, got %v", err)
	}
}

func TestRedisStorageReturnsHitValue(t *testing.T) {
	s := &RedisStorage{cache: stubCache{val: []byte("cached")}}

	val, err := s.Get("some-key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(val) != "cached" {
		t.Fatalf("want cached, got %q", val)
	}
}
