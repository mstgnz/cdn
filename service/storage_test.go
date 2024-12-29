package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

var ErrObjectNotFound = errors.New("object not found")

type StorageClient interface {
	PutObject(ctx context.Context, bucket, path string, data io.Reader) error
	GetObject(ctx context.Context, bucket, path string) (io.ReadCloser, error)
	DeleteObject(ctx context.Context, bucket, path string) error
}

type StorageService struct {
	client StorageClient
}

func (s *StorageService) PutObject(ctx context.Context, bucket, path string, data io.Reader) error {
	return s.client.PutObject(ctx, bucket, path, data)
}

func (s *StorageService) GetObject(ctx context.Context, bucket, path string) (io.ReadCloser, error) {
	return s.client.GetObject(ctx, bucket, path)
}

func (s *StorageService) DeleteObject(ctx context.Context, bucket, path string) error {
	return s.client.DeleteObject(ctx, bucket, path)
}

type mockStorageClient struct {
	objects map[string][]byte
}

func newMockStorageClient() *mockStorageClient {
	return &mockStorageClient{
		objects: make(map[string][]byte),
	}
}

func (m *mockStorageClient) PutObject(ctx context.Context, bucket, path string, data io.Reader) error {
	buf := new(bytes.Buffer)
	_, err := io.Copy(buf, data)
	if err != nil {
		return err
	}
	key := bucket + "/" + path
	m.objects[key] = buf.Bytes()
	return nil
}

func (m *mockStorageClient) GetObject(ctx context.Context, bucket, path string) (io.ReadCloser, error) {
	key := bucket + "/" + path
	data, ok := m.objects[key]
	if !ok {
		return nil, ErrObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *mockStorageClient) DeleteObject(ctx context.Context, bucket, path string) error {
	key := bucket + "/" + path
	if _, ok := m.objects[key]; !ok {
		return ErrObjectNotFound
	}
	delete(m.objects, key)
	return nil
}

func TestStorageService(t *testing.T) {
	mockClient := newMockStorageClient()
	storage := &StorageService{
		client: mockClient,
	}

	t.Run("put and get object", func(t *testing.T) {
		bucket := "test-bucket"
		path := "test/object.txt"
		data := []byte("test data")

		err := storage.PutObject(context.Background(), bucket, path, bytes.NewReader(data))
		if err != nil {
			t.Errorf("failed to put object: %v", err)
		}

		reader, err := storage.GetObject(context.Background(), bucket, path)
		if err != nil {
			t.Errorf("failed to get object: %v", err)
		}
		defer reader.Close()

		got, err := io.ReadAll(reader)
		if err != nil {
			t.Errorf("failed to read object: %v", err)
		}

		if !bytes.Equal(got, data) {
			t.Errorf("got %q, want %q", string(got), string(data))
		}
	})

	t.Run("delete object", func(t *testing.T) {
		bucket := "test-bucket"
		path := "test/delete.txt"
		data := []byte("data to delete")

		err := storage.PutObject(context.Background(), bucket, path, bytes.NewReader(data))
		if err != nil {
			t.Errorf("failed to put object: %v", err)
		}

		err = storage.DeleteObject(context.Background(), bucket, path)
		if err != nil {
			t.Errorf("failed to delete object: %v", err)
		}

		_, err = storage.GetObject(context.Background(), bucket, path)
		if err != ErrObjectNotFound {
			t.Errorf("got error %v, want %v", err, ErrObjectNotFound)
		}
	})

	t.Run("get non-existent object", func(t *testing.T) {
		bucket := "test-bucket"
		path := "test/nonexistent.txt"

		_, err := storage.GetObject(context.Background(), bucket, path)
		if err != ErrObjectNotFound {
			t.Errorf("got error %v, want %v", err, ErrObjectNotFound)
		}
	})

	t.Run("delete non-existent object", func(t *testing.T) {
		bucket := "test-bucket"
		path := "test/nonexistent.txt"

		err := storage.DeleteObject(context.Background(), bucket, path)
		if err != ErrObjectNotFound {
			t.Errorf("got error %v, want %v", err, ErrObjectNotFound)
		}
	})
}
