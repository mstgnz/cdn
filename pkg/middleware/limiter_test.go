package middleware

import (
	"bytes"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mstgnz/cdn/pkg/httpx"
	"github.com/mstgnz/cdn/service"
)

// mapCache is an in-memory CacheService, enough for the limiter's storage.
type mapCache struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (c *mapCache) Get(k string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[k]
	if !ok {
		return nil, service.ErrCacheMiss
	}
	return v, nil
}
func (c *mapCache) Set(k string, v []byte, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[k] = v
	return nil
}
func (c *mapCache) Delete(string) error { return nil }
func (c *mapCache) FlushAll() error     { return nil }
func (c *mapCache) Close() error        { return nil }
func (c *mapCache) GetResizedImage(string, string, uint, uint) ([]byte, error) {
	return nil, nil
}
func (c *mapCache) SetResizedImage(string, string, uint, uint, []byte) error { return nil }

// The Redis value is fiber's msgp encoding of its limiter item, byte for byte:
// a three-entry fixmap, fixint hit counts, and exp as a uint32 while unix time
// fits one. A fiber and a chi replica must read each other's counters.
func TestLimiterItemEncoding(t *testing.T) {
	exp := uint64(1790000000)
	got := limiterItem{currHits: 3, prevHits: 0, exp: exp}.marshal()

	want := []byte{0x83, 0xa8}
	want = append(want, "currHits"...)
	want = append(want, 0x03, 0xa8)
	want = append(want, "prevHits"...)
	want = append(want, 0x00, 0xa3)
	want = append(want, "exp"...)
	want = append(want, 0xce)
	want = binary.BigEndian.AppendUint32(want, uint32(exp))
	if !bytes.Equal(got, want) {
		t.Fatalf("encoding\n got %x\nwant %x", got, want)
	}

	var back limiterItem
	if err := back.unmarshal(got); err != nil || back.currHits != 3 || back.exp != exp {
		t.Fatalf("round trip: %+v %v", back, err)
	}
	if err := back.unmarshal([]byte{0x83, 0xa8}); err == nil {
		t.Fatal("a truncated value decoded without error")
	}
}

func limited(max int, storage *RedisStorage, next http.Handler) http.Handler {
	key := func(*http.Request) string { return "k" }
	return fixedWindow(max, time.Minute, key, storage, func(w http.ResponseWriter) {
		httpx.Text(w, http.StatusTooManyRequests, "limited")
	})(next)
}

func TestFixedWindowCountsAndRefuses(t *testing.T) {
	storage := &RedisStorage{cache: &mapCache{m: map[string][]byte{}}}
	h := httpx.Wrap(0, limited(2, storage, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpx.Text(w, http.StatusOK, "ok")
	})))

	for i, want := range []string{"1", "0"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
		if rec.Code != 200 || rec.Header().Get("X-Ratelimit-Remaining") != want || rec.Header().Get("X-Ratelimit-Limit") != "2" {
			t.Fatalf("request %d: %d %v", i+1, rec.Code, rec.Header())
		}
		if reset := rec.Header().Get("X-Ratelimit-Reset"); reset != "60" && reset != "59" {
			t.Fatalf("request %d: reset = %q", i+1, reset)
		}
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("third request: %d %v", rec.Code, rec.Header())
	}
	if rec.Header().Get("X-Ratelimit-Limit") != "" {
		t.Fatal("a refused request carries its own limiter's headers; fiber sent only Retry-After")
	}
}

// The upload limiter sits inside the global one and fiber keyed both on the same
// identity, so one upload counted twice and the outer limiter's headers won.
func TestNestedLimitersShareTheCounter(t *testing.T) {
	storage := &RedisStorage{cache: &mapCache{m: map[string][]byte{}}}
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { httpx.Text(w, 200, "ok") })
	h := httpx.Wrap(0, limited(10, storage, limited(3, storage, ok)))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/upload", nil))
	if rec.Header().Get("X-Ratelimit-Limit") != "10" || rec.Header().Get("X-Ratelimit-Remaining") != "9" {
		t.Fatalf("first: %v", rec.Header())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/upload", nil)) // hits 3 and 4
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second upload: %d, want 429 from the inner limiter", rec.Code)
	}
	if rec.Header().Get("X-Ratelimit-Limit") != "10" || rec.Header().Get("X-Ratelimit-Remaining") != "7" {
		t.Fatalf("the outer limiter's headers must decorate the inner 429: %v", rec.Header())
	}
}
