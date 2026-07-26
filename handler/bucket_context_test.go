package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/mstgnz/cdn/pkg/audit"
	"github.com/mstgnz/cdn/service"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
)

// runResolveBucket exercises resolveBucket behind a real fiber request, with or
// without a principal stored by the auth middleware.
func runResolveBucket(t *testing.T, principal *service.Principal, requested string) (string, error) {
	t.Helper()

	app := fiber.New()
	var (
		got    string
		gotErr error
	)
	app.Get("/", func(c *fiber.Ctx) error {
		if principal != nil {
			service.StorePrincipal(c, *principal)
		}
		got, gotErr = resolveBucket(c, requested)
		return c.SendString("done")
	})

	if _, err := app.Test(httptest.NewRequest("GET", "/", nil)); err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return got, gotErr
}

// TestResolveBucketGeneralTokenUnchanged is the backward-compatibility guard:
// with the general token every write endpoint must keep using exactly the bucket
// the request named.
func TestResolveBucketGeneralTokenUnchanged(t *testing.T) {
	general := &service.Principal{}

	for _, requested := range []string{"tedarik", "sovtajyeri", "anything-at-all", ""} {
		got, err := runResolveBucket(t, general, requested)
		if err != nil {
			t.Errorf("general token rejected bucket %q: %v", requested, err)
		}
		if got != requested {
			t.Errorf("general token: got %q, want %q", got, requested)
		}
	}
}

// TestResolveBucketNoPrincipalUnchanged covers a route wired without the
// bucket-aware middleware: it must behave like the general token, not like a
// scoped principal for an empty bucket.
func TestResolveBucketNoPrincipalUnchanged(t *testing.T) {
	got, err := runResolveBucket(t, nil, "tedarik")
	if err != nil {
		t.Fatalf("missing principal rejected: %v", err)
	}
	if got != "tedarik" {
		t.Fatalf("got %q, want %q", got, "tedarik")
	}
}

func TestResolveBucketScopedToken(t *testing.T) {
	scoped := &service.Principal{Scoped: true, Bucket: "tedarik"}

	cases := []struct {
		name      string
		requested string
		want      string
		wantErr   bool
	}{
		{"omitted falls back to the token bucket", "", "tedarik", false},
		{"matching bucket is allowed", "tedarik", "tedarik", false},
		{"padded matching bucket is allowed", "  tedarik  ", "tedarik", false},
		{"another bucket is refused", "sovtajyeri", "", true},
		{"case difference is refused", "Tedarik", "", true},
		{"prefix of the bucket is refused", "tedari", "", true},
		{"bucket with the token bucket as prefix is refused", "tedarik-2", "", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := runResolveBucket(t, scoped, c.requested)
			if c.wantErr {
				if err == nil {
					t.Fatalf("requested %q accepted, got bucket %q", c.requested, got)
				}
				if got != "" {
					t.Fatalf("refused request still returned a bucket: %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("requested %q rejected: %v", c.requested, err)
			}
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestResolveBucketEmitsAuditEventOnDenial checks the wiring rather than the
// audit package itself: a denial has to reach the log, and an allowed request
// must stay silent so the event keeps meaning something.
func TestResolveBucketEmitsAuditEventOnDenial(t *testing.T) {
	scoped := &service.Principal{Scoped: true, Bucket: "tedarik"}

	withCapturedLog := func(requested string) string {
		var buf bytes.Buffer
		previous := zlog.Logger
		zlog.Logger = zerolog.New(&buf)
		defer func() { zlog.Logger = previous }()

		runResolveBucket(t, scoped, requested)
		return buf.String()
	}

	denied := withCapturedLog("sovtajyeri")
	if !strings.Contains(denied, audit.EventBucketAccessDenied) {
		t.Errorf("denial did not emit %q: %s", audit.EventBucketAccessDenied, denied)
	}
	if !strings.Contains(denied, "tedarik") || !strings.Contains(denied, "sovtajyeri") {
		t.Errorf("denial entry is missing one of the bucket names: %s", denied)
	}

	if allowed := withCapturedLog("tedarik"); strings.Contains(allowed, audit.EventBucketAccessDenied) {
		t.Errorf("an allowed request emitted a denial event: %s", allowed)
	}
}

// TestBucketForbiddenResponse pins the status and makes sure the body names no
// bucket, so a scoped token cannot be used to probe which buckets exist.
func TestBucketForbiddenResponse(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error {
		return bucketForbidden(c)
	})

	res, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if res.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status = %d, want %d", res.StatusCode, fiber.StatusForbidden)
	}

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var body struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal body %q: %v", raw, err)
	}
	if body.Success {
		t.Error("success = true on a forbidden response")
	}
	if body.Message != errBucketForbidden.Error() {
		t.Errorf("message = %q, want %q", body.Message, errBucketForbidden.Error())
	}
}
