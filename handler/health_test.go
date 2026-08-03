package handler

import (
	"context"
	"strings"
	"testing"
)

// TestCheckCacheHealthWithNilCache covers the shape production reached on
// 2026-08-03: the cache service was discarded at boot and the health checker was
// handed a nil. CacheService is an interface, so the nil is not caught by any
// method call, it panics on the first one. That surfaced as a bare 500 from the
// recover middleware with nothing pointing at the cause.
//
// NewCacheService no longer discards a working client, so a nil now only means
// an unparseable REDIS_URL, but the guard stays: reporting the state is always
// better than a panic, and this is the only place that can tell the operator.
func TestCheckCacheHealthWithNilCache(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("checkCacheHealth panicked on a nil cache: %v", r)
		}
	}()

	hc := &HealthChecker{cache: nil}
	got := hc.checkCacheHealth(context.Background())

	if !strings.HasPrefix(got, "unhealthy") {
		t.Fatalf("checkCacheHealth() = %q, want it to report unhealthy", got)
	}
	if isCoreHealthy("healthy", got) {
		t.Fatal("a missing cache was reported as core-healthy")
	}
}

// TestIsCoreHealthy verifies that overall health depends only on the always-on
// core (MinIO + cache) and is independent of optional integrations like AWS.
func TestIsCoreHealthy(t *testing.T) {
	tests := []struct {
		name        string
		minioHealth string
		cacheHealth string
		want        bool
	}{
		{"both healthy", "healthy", "healthy", true},
		{"minio down", "unhealthy: connection refused", "healthy", false},
		{"cache down", "healthy", "unhealthy: set failed", false},
		{"both down", "unhealthy", "unhealthy", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isCoreHealthy(tt.minioHealth, tt.cacheHealth); got != tt.want {
				t.Fatalf("isCoreHealthy(%q, %q) = %v, want %v",
					tt.minioHealth, tt.cacheHealth, got, tt.want)
			}
		})
	}
}
