package unit

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/mstgnz/cdn/handler"
	"github.com/mstgnz/cdn/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func setupMockMinio() *minio.Client {
	client, err := minio.New("localhost:9000", &minio.Options{
		Creds:  credentials.NewStaticV4("minioadmin", "minioadmin", ""),
		Secure: false,
	})
	if err != nil {
		return nil
	}
	return client
}

type MockAwsService struct {
	mock.Mock
	service.AwsService
}

type MockCacheService struct {
	mock.Mock
	service.CacheService
}

func TestHealthCheck(t *testing.T) {
	// Setup
	app := fiber.New()
	mockMinio := setupMockMinio()
	mockAws := &MockAwsService{}
	mockCache := &MockCacheService{}

	healthChecker := handler.NewHealthChecker(mockMinio, mockAws, mockCache)
	app.Get("/health", healthChecker.HealthCheck)

	// Test cases
	tests := []struct {
		name           string
		expectedStatus int
		expectedBody   map[string]any
	}{
		{
			name:           "Success Response",
			expectedStatus: fiber.StatusOK,
			expectedBody: map[string]any{
				"success": true,
				"message": "Healthy",
				"data": map[string]any{
					"minio": "Connected",
					"aws":   "Connected",
					"redis": "Connected",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/health", nil)
			resp, err := app.Test(req)

			assert.NoError(t, err)
			assert.Equal(t, tt.expectedStatus, resp.StatusCode)

			var body map[string]any
			err = json.NewDecoder(resp.Body).Decode(&body)

			assert.NoError(t, err)
			assert.Equal(t, tt.expectedBody, body)
		})
	}
}

func TestUploadImage(t *testing.T) {
	// Setup
	app := fiber.New()
	mockMinio := setupMockMinio()
	mockAws := &MockAwsService{}
	mockImageService := &service.ImageService{}

	imageHandler := handler.NewImage(mockMinio, mockAws, mockImageService)
	app.Post("/upload", imageHandler.UploadImage)

	// Test cases
	tests := []struct {
		name           string
		payload        []byte
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "Invalid Request",
			payload:        []byte(`{}`),
			expectedStatus: fiber.StatusBadRequest,
			expectedError:  "Invalid request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/upload", bytes.NewBuffer(tt.payload))
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req)

			assert.NoError(t, err)
			assert.Equal(t, tt.expectedStatus, resp.StatusCode)

			if tt.expectedError != "" {
				var body map[string]any
				err = json.NewDecoder(resp.Body).Decode(&body)
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedError, body["message"])
			}
		})
	}
}
