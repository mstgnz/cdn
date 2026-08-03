package service

import (
	"testing"
	"time"
)

// TestNewCacheServiceReturnsUsableServiceWhenRedisIsDown pins the fix for the
// production incident on 2026-08-03. Redis replays its RDB before answering
// commands, so on a cold start of the whole stack the API pings while Redis is
// still "LOADING" and the ping fails. The caller used to discard the client on
// that error, which turned a few seconds of startup into a process that ran
// without a cache until somebody restarted it: two of three replicas came up
// that way and their /health then panicked on the nil interface.
//
// The service must come back usable regardless, because go-redis dials lazily
// per command and reconnects on its own once Redis is accepting connections.
func TestNewCacheServiceReturnsUsableServiceWhenRedisIsDown(t *testing.T) {
	// Port 1 is reserved and nothing listens on it, so this always fails to
	// connect without depending on whether the developer has Redis running.
	t.Setenv("REDIS_URL", "redis://127.0.0.1:1")

	cache, err := NewCacheService()
	if err == nil {
		t.Fatal("expected an error for an unreachable Redis")
	}
	if cache == nil {
		t.Fatal("service was discarded on a connection error; it must stay usable so it can reconnect")
	}

	// Usable means calls reach the client and report Redis being down, rather
	// than panicking on a nil interface.
	if err := cache.Set("k", []byte("v"), time.Second); err == nil {
		t.Error("Set against an unreachable Redis returned no error")
	}
}

// A REDIS_URL that cannot be parsed is different in kind: no client can ever be
// built from it, so there is nothing to hand back and nothing to reconnect to.
// That is the one case where a nil service is correct, and the health check has
// to cope with it.
func TestNewCacheServiceRejectsUnparseableURL(t *testing.T) {
	t.Setenv("REDIS_URL", "://not-a-url")

	cache, err := NewCacheService()
	if err == nil {
		t.Fatal("expected an error for an unparseable REDIS_URL")
	}
	if cache != nil {
		t.Fatal("expected a nil service for configuration that can never work")
	}
}

func TestCacheService(t *testing.T) {
	cache, err := NewCacheService()
	if err != nil {
		// Redis is required for this test; skip (don't fail) when it is not
		// reachable so the suite stays green without infrastructure.
		t.Skipf("Redis not available (%v); start it (docker compose up -d redis) to run this test", err)
	}

	t.Run("set and get", func(t *testing.T) {
		key := "test:key"
		value := []byte("test value")

		err := cache.Set(key, value, time.Minute)
		if err != nil {
			t.Errorf("failed to set cache: %v", err)
		}

		got, err := cache.Get(key)
		if err != nil {
			t.Errorf("failed to get cache: %v", err)
		}

		if string(got) != string(value) {
			t.Errorf("got %q, want %q", string(got), string(value))
		}
	})

	t.Run("expiration", func(t *testing.T) {
		key := "test:expiration"
		value := []byte("expiring value")

		err := cache.Set(key, value, time.Millisecond*100)
		if err != nil {
			t.Errorf("failed to set cache: %v", err)
		}

		time.Sleep(time.Millisecond * 200)

		_, err = cache.Get(key)
		if err == nil {
			t.Error("expected error for expired key, got nil")
		}
	})

	t.Run("delete", func(t *testing.T) {
		key := "test:delete"
		value := []byte("value to delete")

		err := cache.Set(key, value, time.Minute)
		if err != nil {
			t.Errorf("failed to set cache: %v", err)
		}

		err = cache.Delete(key)
		if err != nil {
			t.Errorf("failed to delete cache: %v", err)
		}

		_, err = cache.Get(key)
		if err == nil {
			t.Error("expected error for deleted key, got nil")
		}
	})

	t.Run("resized image cache", func(t *testing.T) {
		bucket := "test-bucket"
		path := "test/image.jpg"
		width := uint(100)
		height := uint(100)
		data := []byte("fake image data")

		err := cache.SetResizedImage(bucket, path, width, height, data)
		if err != nil {
			t.Errorf("failed to set resized image cache: %v", err)
		}

		got, err := cache.GetResizedImage(bucket, path, width, height)
		if err != nil {
			t.Errorf("failed to get resized image cache: %v", err)
		}

		if string(got) != string(data) {
			t.Errorf("got %q, want %q", string(got), string(data))
		}
	})
}
