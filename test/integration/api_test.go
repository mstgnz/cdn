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

// requireServer skips the test unless a CDN server is reachable at baseURL.
// These are end-to-end tests against a running stack; they are not meant to
// fail when no server is up (e.g. on a plain CI/dev machine).
func requireServer(t *testing.T) {
	t.Helper()
	c := &http.Client{Timeout: time.Second}
	resp, err := c.Get(baseURL + "/health")
	if err != nil {
		t.Skipf("no server at %s (%v); start the stack to run integration tests", baseURL, err)
	}
	_ = resp.Body.Close()
}

func TestHealthEndpoint(t *testing.T) {
	requireServer(t)
	client := &http.Client{Timeout: timeout}

	resp, err := client.Get(baseURL + "/health")
	assert.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	err = json.NewDecoder(resp.Body).Decode(&body)
	assert.NoError(t, err)

	// service.Response envelope: { success, message, data{ status, services } }.
	assert.Equal(t, true, body["success"])
	assert.Equal(t, "Health check", body["message"])
	if data, ok := body["data"].(map[string]any); ok {
		assert.Equal(t, "healthy", data["status"])
	} else {
		t.Fatalf("unexpected health body shape: %v", body)
	}
}

func TestUploadEndpoint(t *testing.T) {
	requireServer(t)
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

	// Should fail without proper form data / valid token
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestMetricsEndpoint(t *testing.T) {
	requireServer(t)
	client := &http.Client{Timeout: timeout}

	resp, err := client.Get(baseURL + "/metrics")
	assert.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/plain")
}
