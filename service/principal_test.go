package service

import (
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/mstgnz/cdn/pkg/config"
)

const (
	testBucketSecret  = "0123456789abcdef0123456789abcdef"
	otherBucketSecret = "fedcba9876543210fedcba9876543210"
)

// loadTestTokens installs a two-bucket token file for the duration of a test and
// clears the in-memory index afterwards, so tests cannot leak tokens into each
// other through the package-level store.
func loadTestTokens(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")
	body := `{"buckets":[
		{"bucket":"tedarik","token":"` + testBucketSecret + `"},
		{"bucket":"tramer","token":"` + otherBucketSecret + `"}
	]}`
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

// resolve runs ResolvePrincipal behind a real fiber request.
func resolve(t *testing.T, authHeader string) (Principal, error) {
	t.Helper()
	app := fiber.New()
	var (
		principal  Principal
		resolveErr error
	)
	app.Get("/", func(c *fiber.Ctx) error {
		principal, resolveErr = ResolvePrincipal(c)
		return c.SendString("done")
	})
	req := httptest.NewRequest("GET", "/", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	if _, err := app.Test(req); err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return principal, resolveErr
}

func TestResolvePrincipalGeneralToken(t *testing.T) {
	t.Setenv("TOKEN", "the-general-token")
	loadTestTokens(t)

	p, err := resolve(t, "Bearer the-general-token")
	if err != nil {
		t.Fatalf("general token rejected: %v", err)
	}
	if p.Scoped || p.Bucket != "" {
		t.Fatalf("general token produced a scoped principal: %+v", p)
	}
}

// TestResolvePrincipalGeneralTokenWithColon is the backward-compatibility guard
// that matters most: a deployment whose TOKEN contains a ':' must keep
// authenticating. If the "<bucket>:<secret>" split ran first, such a token would
// be read as a bucket credential and every request would start failing.
func TestResolvePrincipalGeneralTokenWithColon(t *testing.T) {
	const colonToken = "tedarik:not-really-a-bucket-token"
	t.Setenv("TOKEN", colonToken)
	loadTestTokens(t)

	p, err := resolve(t, "Bearer "+colonToken)
	if err != nil {
		t.Fatalf("general token containing ':' rejected: %v", err)
	}
	if p.Scoped {
		t.Fatalf("general token containing ':' was read as a bucket token: %+v", p)
	}
}

func TestResolvePrincipalBucketToken(t *testing.T) {
	t.Setenv("TOKEN", "the-general-token")
	loadTestTokens(t)

	p, err := resolve(t, "Bearer tedarik:"+testBucketSecret)
	if err != nil {
		t.Fatalf("valid bucket token rejected: %v", err)
	}
	if !p.Scoped {
		t.Fatal("bucket token did not produce a scoped principal")
	}
	if p.Bucket != "tedarik" {
		t.Fatalf("Bucket = %q, want %q", p.Bucket, "tedarik")
	}
}

// TestResolvePrincipalPrefixGrantsNothing covers the central authorization
// property: the bucket prefix is attacker-controlled, so it must only ever be
// honoured together with the secret configured for that exact bucket.
func TestResolvePrincipalPrefixGrantsNothing(t *testing.T) {
	t.Setenv("TOKEN", "the-general-token")
	loadTestTokens(t)

	cases := []struct {
		name   string
		header string
	}{
		{"another bucket's secret", "Bearer tedarik:" + otherBucketSecret},
		{"no secret at all", "Bearer tedarik:"},
		{"wrong secret", "Bearer tedarik:wrong-secret-of-sufficient-length-x"},
		{"bucket without a configured token", "Bearer sovtajyeri:" + testBucketSecret},
		{"unknown bucket", "Bearer nope:" + testBucketSecret},
		{"no colon", "Bearer just-a-wrong-token"},
		{"empty credential", "Bearer "},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := resolve(t, c.header)
			if err == nil {
				t.Fatalf("credential accepted, principal %+v", p)
			}
			if p.Scoped || p.Bucket != "" {
				t.Fatalf("rejected credential still produced a principal: %+v", p)
			}
		})
	}
}

// TestResolvePrincipalSecretMayContainColon documents that only the first colon
// separates bucket from secret, so a secret is free to contain one.
func TestResolvePrincipalSecretMayContainColon(t *testing.T) {
	t.Setenv("TOKEN", "the-general-token")

	secret := "abcd:efgh:0123456789abcdef0123456789"
	path := filepath.Join(t.TempDir(), "tokens.json")
	if err := os.WriteFile(path, []byte(`{"buckets":[{"bucket":"tedarik","token":"`+secret+`"}]}`), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	if _, err := config.LoadBucketTokens(path); err != nil {
		t.Fatalf("load token file: %v", err)
	}

	p, err := resolve(t, "Bearer tedarik:"+secret)
	if err != nil {
		t.Fatalf("secret containing ':' rejected: %v", err)
	}
	if !p.Scoped || p.Bucket != "tedarik" {
		t.Fatalf("unexpected principal: %+v", p)
	}
}

func TestResolvePrincipalHeaderErrors(t *testing.T) {
	t.Setenv("TOKEN", "the-general-token")
	loadTestTokens(t)

	if _, err := resolve(t, ""); !errors.Is(err, ErrNoToken) {
		t.Errorf("missing header: err = %v, want ErrNoToken", err)
	}
	if _, err := resolve(t, "BadFormat"); !errors.Is(err, ErrInvalidAuthFormat) {
		t.Errorf("malformed header: err = %v, want ErrInvalidAuthFormat", err)
	}
}

// TestResolvePrincipalErrorsDoNotLeakSecrets keeps configured credentials out of
// responses and logs, which is where these errors end up.
func TestResolvePrincipalErrorsDoNotLeakSecrets(t *testing.T) {
	t.Setenv("TOKEN", "the-general-token")
	loadTestTokens(t)

	_, err := resolve(t, "Bearer tedarik:wrong")
	if err == nil {
		t.Fatal("wrong secret accepted")
	}
	for _, secret := range []string{"the-general-token", testBucketSecret, otherBucketSecret} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error %q leaks a configured secret", err.Error())
		}
	}
}

func TestPrincipalLocalsRoundTrip(t *testing.T) {
	app := fiber.New()
	var (
		fromEmpty  Principal
		fromStored Principal
	)
	app.Get("/empty", func(c *fiber.Ctx) error {
		fromEmpty = PrincipalFrom(c)
		return c.SendString("done")
	})
	app.Get("/stored", func(c *fiber.Ctx) error {
		StorePrincipal(c, Principal{Scoped: true, Bucket: "tedarik"})
		fromStored = PrincipalFrom(c)
		return c.SendString("done")
	})

	if _, err := app.Test(httptest.NewRequest("GET", "/empty", nil)); err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if _, err := app.Test(httptest.NewRequest("GET", "/stored", nil)); err != nil {
		t.Fatalf("request failed: %v", err)
	}

	// A route wired without the bucket middleware must look like the unrestricted
	// general token rather than a scoped principal for an empty bucket.
	if fromEmpty.Scoped || fromEmpty.Bucket != "" {
		t.Errorf("missing principal = %+v, want zero value", fromEmpty)
	}
	if !fromStored.Scoped || fromStored.Bucket != "tedarik" {
		t.Errorf("stored principal = %+v, want {true tedarik}", fromStored)
	}
}
