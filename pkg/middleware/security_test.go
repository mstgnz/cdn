package middleware

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// callHandler runs fn inside a fiber request and returns its string body,
// applying the given request headers.
func callHandler(t *testing.T, fn fiber.Handler, headers map[string]string) string {
	t.Helper()
	app := fiber.New()
	app.Get("/", fn)
	req := httptest.NewRequest("GET", "/", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

// TestClientIP_PrefersCloudflareHeader verifies the real client IP comes from
// CF-Connecting-IP and that a spoofable X-Forwarded-For is ignored.
func TestClientIP_PrefersCloudflareHeader(t *testing.T) {
	got := callHandler(t, func(c *fiber.Ctx) error {
		return c.SendString(ClientIP(c))
	}, map[string]string{
		"CF-Connecting-IP": "203.0.113.7",
		"X-Forwarded-For":  "1.2.3.4", // attacker-supplied, must be ignored
		"X-Real-IP":        "172.18.0.1",
	})
	if got != "203.0.113.7" {
		t.Fatalf("expected CF-Connecting-IP 203.0.113.7, got %q", got)
	}
}

// TestClientIP_FallsBackToPeer verifies that without the Cloudflare header we
// fall back to the TCP peer rather than returning empty.
func TestClientIP_FallsBackToPeer(t *testing.T) {
	got := callHandler(t, func(c *fiber.Ctx) error {
		return c.SendString(ClientIP(c))
	}, nil)
	if got == "" {
		t.Fatal("expected a fallback IP, got empty string")
	}
}

// TestRateLimitKey verifies the key combines the real client IP with the
// bearer token, and uses IP only when no token is present.
func TestRateLimitKey(t *testing.T) {
	withToken := callHandler(t, func(c *fiber.Ctx) error {
		return c.SendString(RateLimitKey(c))
	}, map[string]string{
		"CF-Connecting-IP": "203.0.113.7",
		"Authorization":    "Bearer secret-token",
	})
	if withToken != "203.0.113.7:secret-token" {
		t.Fatalf("expected ip:token key, got %q", withToken)
	}

	noToken := callHandler(t, func(c *fiber.Ctx) error {
		return c.SendString(RateLimitKey(c))
	}, map[string]string{
		"CF-Connecting-IP": "203.0.113.7",
	})
	if noToken != "203.0.113.7" {
		t.Fatalf("expected ip-only key, got %q", noToken)
	}
}
