package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const (
	baseURL = "http://localhost:9090"
	timeout = 10 * time.Second
)

func TestHealthEndpoint(t *testing.T) {
	client := &http.Client{Timeout: timeout}

	resp, err := client.Get(baseURL + "/health")
	assert.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	err = json.NewDecoder(resp.Body).Decode(&body)
	assert.NoError(t, err)

	assert.Equal(t, true, body["status"])
	assert.Equal(t, "Healthy", body["message"])
}

func TestUploadEndpoint(t *testing.T) {
	client := &http.Client{Timeout: timeout}

	// Test file upload
	fileContents := []byte("test image content")
	req, err := http.NewRequest("POST", baseURL+"/upload", bytes.NewBuffer(fileContents))
	assert.NoError(t, err)

	req.Header.Set("Content-Type", "multipart/form-data")
	req.Header.Set("Authorization", "test-token")

	resp, err := client.Do(req)
	assert.NoError(t, err)
	defer resp.Body.Close()

	// Should fail without proper form data
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestMetricsEndpoint(t *testing.T) {
	client := &http.Client{Timeout: timeout}

	resp, err := client.Get(baseURL + "/metrics")
	assert.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/plain")
}
