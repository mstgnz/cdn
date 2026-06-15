package unit

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/gofiber/fiber/v2"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/mstgnz/cdn/handler"
	"github.com/mstgnz/cdn/service"
	"github.com/stretchr/testify/assert"
)

// deadMinio returns a MinIO client pointed at a closed port so the health
// check's ListBuckets fails fast and deterministically (no infra required).
func deadMinio(t *testing.T) *minio.Client {
	t.Helper()
	client, err := minio.New("127.0.0.1:1", &minio.Options{
		Creds:  credentials.NewStaticV4("x", "y", ""),
		Secure: false,
	})
	if err != nil {
		t.Fatalf("minio client: %v", err)
	}
	return client
}

// stubCache implements only the CacheService methods the health check uses,
// reporting a healthy cache.
type stubCache struct{ service.CacheService }

func (stubCache) Set(string, []byte, time.Duration) error { return nil }
func (stubCache) Get(string) ([]byte, error)              { return []byte("ok"), nil }

// stubAws implements only the AwsService method the health check uses.
type stubAws struct{ service.AwsService }

func (stubAws) ListBuckets() ([]s3types.Bucket, error) { return nil, nil }

// TestHealthCheck_Degraded verifies the real contract: when MinIO (a core
// dependency) is unreachable, the endpoint reports degraded with 503 even
// though cache and AWS are healthy. Deterministic and infra-free.
func TestHealthCheck_Degraded(t *testing.T) {
	app := fiber.New()
	hc := handler.NewHealthChecker(deadMinio(t), stubAws{}, stubCache{})
	app.Get("/health", hc.HealthCheck)

	resp, err := app.Test(httptest.NewRequest("GET", "/health", nil), -1)
	assert.NoError(t, err)
	assert.Equal(t, fiber.StatusServiceUnavailable, resp.StatusCode)

	var body map[string]any
	assert.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

	data, ok := body["data"].(map[string]any)
	assert.True(t, ok, "expected data object")
	assert.Equal(t, "degraded", data["status"])

	services, ok := data["services"].(map[string]any)
	assert.True(t, ok, "expected services object")
	assert.Contains(t, services["minio"], "unhealthy")
	assert.Equal(t, "healthy", services["cache"])
	assert.Equal(t, "healthy", services["aws"])
}

// TestUploadImage_InvalidForm verifies UploadImage rejects a non-multipart
// body before touching storage (returns 400 "File Not Found!").
func TestUploadImage_InvalidForm(t *testing.T) {
	app := fiber.New()
	h := handler.NewImage(deadMinio(t), stubAws{}, &service.ImageService{})
	app.Post("/upload", h.UploadImage)

	req := httptest.NewRequest("POST", "/upload", bytes.NewBuffer([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	assert.NoError(t, err)
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)

	var body map[string]any
	assert.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "File Not Found!", body["message"])
}
