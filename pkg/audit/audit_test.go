package audit

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
)

// capture runs fn inside a fiber request with the global logger redirected to a
// buffer, and returns the single decoded log entry it produced.
func capture(t *testing.T, headers map[string]string, fn func(c *fiber.Ctx)) map[string]any {
	t.Helper()

	var buf bytes.Buffer
	previous := zlog.Logger
	zlog.Logger = zerolog.New(&buf)
	t.Cleanup(func() { zlog.Logger = previous })

	app := fiber.New()
	app.Post("/upload", func(c *fiber.Ctx) error {
		fn(c)
		return c.SendString("done")
	})

	req := httptest.NewRequest("POST", "/upload", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if _, err := app.Test(req); err != nil {
		t.Fatalf("request failed: %v", err)
	}

	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("no log entry was written")
	}
	if strings.Count(line, "\n") > 0 {
		t.Fatalf("expected exactly one log entry, got:\n%s", line)
	}

	var entry map[string]any
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("log entry is not valid JSON (%v): %s", err, line)
	}
	return entry
}

func assertField(t *testing.T, entry map[string]any, key, want string) {
	t.Helper()
	got, ok := entry[key].(string)
	if !ok {
		t.Fatalf("field %q missing from entry %v", key, entry)
	}
	if got != want {
		t.Errorf("field %q = %q, want %q", key, got, want)
	}
}

func TestAuthFailure(t *testing.T) {
	entry := capture(t, map[string]string{"CF-Connecting-IP": "203.0.113.7"}, func(c *fiber.Ctx) {
		AuthFailure(c, "invalid token")
	})

	assertField(t, entry, "event", EventAuthFailure)
	assertField(t, entry, "reason", "invalid token")
	assertField(t, entry, "method", "POST")
	assertField(t, entry, "path", "/upload")
	assertField(t, entry, "ip", "203.0.113.7")
	assertField(t, entry, "level", "warn")
}

func TestScopedTokenOnOperatorRoute(t *testing.T) {
	entry := capture(t, map[string]string{"CF-Connecting-IP": "203.0.113.7"}, func(c *fiber.Ctx) {
		ScopedTokenOnOperatorRoute(c, "tedarik")
	})

	assertField(t, entry, "event", EventScopedTokenOnAdmin)
	assertField(t, entry, "token_bucket", "tedarik")
	assertField(t, entry, "ip", "203.0.113.7")
}

func TestBucketAccessDenied(t *testing.T) {
	entry := capture(t, map[string]string{"CF-Connecting-IP": "203.0.113.7"}, func(c *fiber.Ctx) {
		BucketAccessDenied(c, "tedarik", "sovtajyeri")
	})

	assertField(t, entry, "event", EventBucketAccessDenied)
	assertField(t, entry, "token_bucket", "tedarik")
	assertField(t, entry, "requested_bucket", "sovtajyeri")
}

// TestUsesTrustedClientIP guards the field that makes these entries actionable:
// a browser-supplied X-Forwarded-For must never win over the Cloudflare header,
// otherwise every entry can be attributed to an address of the caller's choosing.
func TestUsesTrustedClientIP(t *testing.T) {
	entry := capture(t, map[string]string{
		"CF-Connecting-IP": "203.0.113.7",
		"X-Forwarded-For":  "1.2.3.4",
	}, func(c *fiber.Ctx) {
		AuthFailure(c, "invalid token")
	})

	assertField(t, entry, "ip", "203.0.113.7")
}

// TestNeverLogsCredentials is the load-bearing guard of this package. The
// Authorization header is present on every one of these requests and must not
// reach the log through any field, including the ones the caller controls.
func TestNeverLogsCredentials(t *testing.T) {
	const (
		generalToken = "general-token-that-must-not-be-logged"
		bucketSecret = "bucket-secret-that-must-not-be-logged"
	)

	cases := []struct {
		name   string
		header string
		emit   func(c *fiber.Ctx)
	}{
		{"general token", "Bearer " + generalToken, func(c *fiber.Ctx) {
			AuthFailure(c, "invalid token")
		}},
		{"bucket token", "Bearer tedarik:" + bucketSecret, func(c *fiber.Ctx) {
			ScopedTokenOnOperatorRoute(c, "tedarik")
		}},
		{"bucket denied", "Bearer tedarik:" + bucketSecret, func(c *fiber.Ctx) {
			BucketAccessDenied(c, "tedarik", "sovtajyeri")
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			previous := zlog.Logger
			zlog.Logger = zerolog.New(&buf)
			t.Cleanup(func() { zlog.Logger = previous })

			app := fiber.New()
			app.Post("/upload", func(c *fiber.Ctx) error {
				tc.emit(c)
				return c.SendString("done")
			})
			req := httptest.NewRequest("POST", "/upload", nil)
			req.Header.Set("Authorization", tc.header)
			if _, err := app.Test(req); err != nil {
				t.Fatalf("request failed: %v", err)
			}

			out := buf.String()
			for _, secret := range []string{generalToken, bucketSecret, tc.header} {
				if strings.Contains(out, secret) {
					t.Fatalf("log entry leaks credential material: %s", out)
				}
			}
		})
	}
}
