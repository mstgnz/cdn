package handler

import "testing"

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
