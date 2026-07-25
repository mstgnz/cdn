package middleware

import (
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/mstgnz/cdn/pkg/config"
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

// rateLimitKeyFor returns the rate-limit key for a request from 203.0.113.7 with
// the given Authorization header ("" means no header at all).
func rateLimitKeyFor(t *testing.T, authHeader string) string {
	t.Helper()
	headers := map[string]string{"CF-Connecting-IP": "203.0.113.7"}
	if authHeader != "" {
		headers["Authorization"] = authHeader
	}
	return callHandler(t, func(c *fiber.Ctx) error {
		return c.SendString(RateLimitKey(c))
	}, headers)
}

// installBucketToken loads a single bucket-scoped token for the test.
func installBucketToken(t *testing.T, bucket, secret string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")
	body := `{"buckets":[{"bucket":"` + bucket + `","token":"` + secret + `"}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	if _, err := config.LoadBucketTokens(path); err != nil {
		t.Fatalf("load token file: %v", err)
	}
	t.Cleanup(func() {
		_, _ = config.LoadBucketTokens(filepath.Join(dir, "absent.json"))
	})
}

// TestRateLimitKey verifies the key identifies a verified credential without
// containing it, and falls back to the plain IP otherwise.
func TestRateLimitKey(t *testing.T) {
	const (
		generalToken = "the-general-token"
		bucketSecret = "0123456789abcdef0123456789abcdef"
	)
	t.Setenv("TOKEN", generalToken)
	installBucketToken(t, "tedarik", bucketSecret)

	cases := []struct {
		name       string
		authHeader string
		want       string
	}{
		{"no header", "", "203.0.113.7"},
		{"general token", "Bearer " + generalToken, "203.0.113.7|general"},
		{"bucket token", "Bearer tedarik:" + bucketSecret, "203.0.113.7|bucket:tedarik"},
		{"unknown token", "Bearer nope-not-a-real-token", "203.0.113.7"},
		{"malformed header", "NotBearer", "203.0.113.7"},
		{"right bucket, wrong secret", "Bearer tedarik:wrong", "203.0.113.7"},
		{"unconfigured bucket", "Bearer sovtajyeri:" + bucketSecret, "203.0.113.7"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rateLimitKeyFor(t, c.authHeader); got != c.want {
				t.Fatalf("key = %q, want %q", got, c.want)
			}
		})
	}
}

// TestRateLimitKeyNeverContainsTheSecret is the regression guard for the raw
// bearer value that used to be written straight into the Redis keyspace.
func TestRateLimitKeyNeverContainsTheSecret(t *testing.T) {
	const (
		generalToken = "general-token-that-must-not-leak"
		bucketSecret = "bucket-secret-that-must-not-leak-abcd"
	)
	t.Setenv("TOKEN", generalToken)
	installBucketToken(t, "tedarik", bucketSecret)

	for _, header := range []string{
		"Bearer " + generalToken,
		"Bearer tedarik:" + bucketSecret,
		"Bearer some-random-unverified-token",
	} {
		key := rateLimitKeyFor(t, header)
		for _, secret := range []string{generalToken, bucketSecret, "some-random-unverified-token"} {
			if strings.Contains(key, secret) {
				t.Fatalf("key %q contains credential material", key)
			}
		}
	}
}

// TestRateLimitKeyUnverifiedTokensShareOneBucket is the bypass guard: keying on
// the raw header let a client mint a fresh counter per request by varying the
// token, which disabled per-IP rate limiting entirely.
func TestRateLimitKeyUnverifiedTokensShareOneBucket(t *testing.T) {
	t.Setenv("TOKEN", "the-general-token")
	installBucketToken(t, "tedarik", "0123456789abcdef0123456789abcdef")

	first := rateLimitKeyFor(t, "Bearer random-token-number-one")
	second := rateLimitKeyFor(t, "Bearer random-token-number-two")
	third := rateLimitKeyFor(t, "Bearer tedarik:guessing-the-secret")

	if first != second || second != third {
		t.Fatalf("unverified credentials got separate counters: %q, %q, %q", first, second, third)
	}
	if first != "203.0.113.7" {
		t.Fatalf("unverified credential key = %q, want the plain IP", first)
	}
}

// TestRateLimitKeyDistinctIdentitiesDoNotShare keeps the original intent intact:
// a verified credential still gets its own quota.
func TestRateLimitKeyDistinctIdentitiesDoNotShare(t *testing.T) {
	const bucketSecret = "0123456789abcdef0123456789abcdef"
	t.Setenv("TOKEN", "the-general-token")
	installBucketToken(t, "tedarik", bucketSecret)

	general := rateLimitKeyFor(t, "Bearer the-general-token")
	scoped := rateLimitKeyFor(t, "Bearer tedarik:"+bucketSecret)
	anonymous := rateLimitKeyFor(t, "")

	if general == scoped || general == anonymous || scoped == anonymous {
		t.Fatalf("identities share a counter: general=%q scoped=%q anonymous=%q", general, scoped, anonymous)
	}
}
