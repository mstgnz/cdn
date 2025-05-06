package service

import (
	"testing"
	"time"
)

func TestCacheService(t *testing.T) {
	cache, err := NewCacheService()
	if err != nil {
		t.Fatalf("failed to create cache service: %v", err)
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
