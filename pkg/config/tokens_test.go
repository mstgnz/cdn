package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const validSecret = "0123456789abcdef0123456789abcdef" // exactly MinBucketTokenLength

func writeTokenFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tokens.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	return path
}

func TestValidateGeneralToken(t *testing.T) {
	valid := validSecret // exactly MinBucketTokenLength

	cases := []struct {
		name  string
		token string
		ok    bool
	}{
		{"valid", valid, true},
		{"longer than the minimum", valid + "extra", true},
		{"padded but valid", "  " + valid + "  ", true},

		{"empty", "", false},
		{"whitespace only", "   ", false},
		{"one character short", valid[:len(valid)-1], false},
		{"short", "short-token", false},

		// Placeholders are rejected whatever their length.
		{"env example placeholder", "REPLACE_ME", false},
		{"lowercase placeholder", "replace_me", false},
		{"old env example value", "your-token-here", false},
		{"readme placeholder", "your-secure-token", false},
		{"changeme", "changeme", false},
		{"mixed case placeholder", "ChangeMe", false},
		{"the word token", "token", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateGeneralToken(c.token)
			if c.ok && err != nil {
				t.Fatalf("rejected: %v", err)
			}
			if !c.ok && err == nil {
				t.Fatal("accepted, want rejection")
			}
		})
	}
}

// TestValidateGeneralTokenErrorDoesNotLeakToken keeps the token out of the boot
// log, which is where this error is reported.
func TestValidateGeneralTokenErrorDoesNotLeakToken(t *testing.T) {
	token := "short-but-secret-value"

	err := ValidateGeneralToken(token)
	if err == nil {
		t.Fatal("short token accepted")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("error leaks the token: %q", err.Error())
	}
}

// TestValidateGeneralTokenSharesTheBucketTokenFloor pins the intent that the two
// kinds of token are held to the same minimum, so raising one raises both.
func TestValidateGeneralTokenSharesTheBucketTokenFloor(t *testing.T) {
	atFloor := strings.Repeat("a", MinBucketTokenLength)
	belowFloor := strings.Repeat("a", MinBucketTokenLength-1)

	if err := ValidateGeneralToken(atFloor); err != nil {
		t.Errorf("token at the floor rejected: %v", err)
	}
	if err := ValidateGeneralToken(belowFloor); err == nil {
		t.Error("token below the floor accepted")
	}
}

// TestLoadBucketTokensMissingFileIsNotAnError is the backward-compatibility
// guard: a deployment that only sets the general TOKEN has no token file, and
// boot must proceed with zero bucket tokens rather than failing.
func TestLoadBucketTokensMissingFileIsNotAnError(t *testing.T) {
	count, err := LoadBucketTokens(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("missing file reported as error: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}
	if BucketTokenCount() != 0 {
		t.Fatalf("BucketTokenCount() = %d, want 0", BucketTokenCount())
	}
}

func TestLoadBucketTokensValid(t *testing.T) {
	path := writeTokenFile(t, `{"buckets":[
		{"bucket":"tedarik","token":"`+validSecret+`","label":"supply app"},
		{"bucket":"atc-yazilim","token":"`+validSecret+`x"}
	]}`)

	count, err := LoadBucketTokens(path)
	if err != nil {
		t.Fatalf("valid file rejected: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}

	if !BucketTokenMatches("tedarik", validSecret) {
		t.Error("correct secret rejected for tedarik")
	}
	if BucketTokenMatches("tedarik", validSecret+"x") {
		t.Error("another bucket's secret accepted for tedarik")
	}
	if BucketTokenMatches("sovtajyeri", validSecret) {
		t.Error("secret accepted for a bucket with no configured token")
	}
	if BucketTokenMatches("tedarik", "") {
		t.Error("empty secret accepted")
	}
}

// TestLoadBucketTokensResetsPreviousState guards against a reload leaving
// revoked entries behind in the in-memory index.
func TestLoadBucketTokensResetsPreviousState(t *testing.T) {
	first := writeTokenFile(t, `{"buckets":[{"bucket":"tedarik","token":"`+validSecret+`"}]}`)
	if _, err := LoadBucketTokens(first); err != nil {
		t.Fatalf("first load failed: %v", err)
	}

	second := writeTokenFile(t, `{"buckets":[{"bucket":"tramer","token":"`+validSecret+`"}]}`)
	if _, err := LoadBucketTokens(second); err != nil {
		t.Fatalf("second load failed: %v", err)
	}

	if BucketTokenMatches("tedarik", validSecret) {
		t.Error("token from the previous file still matches after reload")
	}
	if !BucketTokenMatches("tramer", validSecret) {
		t.Error("token from the current file does not match")
	}
}

func TestLoadBucketTokensRejectsBadFiles(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"malformed json", `{"buckets":[`},
		{"short secret", `{"buckets":[{"bucket":"tedarik","token":"tooshort"}]}`},
		{"empty secret", `{"buckets":[{"bucket":"tedarik","token":""}]}`},
		{"missing secret", `{"buckets":[{"bucket":"tedarik"}]}`},
		{"empty bucket", `{"buckets":[{"bucket":"","token":"` + validSecret + `"}]}`},
		{"uppercase bucket", `{"buckets":[{"bucket":"Tedarik","token":"` + validSecret + `"}]}`},
		{"bucket with colon", `{"buckets":[{"bucket":"a:b","token":"` + validSecret + `"}]}`},
		{"duplicate bucket", `{"buckets":[
			{"bucket":"tedarik","token":"` + validSecret + `"},
			{"bucket":"tedarik","token":"` + validSecret + `y"}
		]}`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeTokenFile(t, c.body)
			count, err := LoadBucketTokens(path)
			if err == nil {
				t.Fatalf("bad file accepted (count=%d)", count)
			}
			if BucketTokenCount() != 0 {
				t.Fatalf("BucketTokenCount() = %d after a failed load, want 0", BucketTokenCount())
			}
		})
	}
}

// TestBucketTokenExpiry covers the whole expiry lifecycle through the public
// surface: no expiry keeps working forever, a future one is honoured until it
// passes, and the boundary counts as expired rather than as the last valid
// instant.
func TestBucketTokenExpiry(t *testing.T) {
	expiry := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	path := writeTokenFile(t, `{"buckets":[
		{"bucket":"tedarik","token":"`+validSecret+`","expires_at":"2026-12-31T00:00:00Z"},
		{"bucket":"tramer","token":"`+validSecret+`"}
	]}`)

	if _, err := LoadBucketTokens(path); err != nil {
		t.Fatalf("file with an expiry rejected: %v", err)
	}

	cases := []struct {
		name   string
		bucket string
		now    time.Time
		want   bool
	}{
		{"before expiry", "tedarik", expiry.Add(-time.Hour), true},
		{"one second before", "tedarik", expiry.Add(-time.Second), true},
		{"exactly at expiry", "tedarik", expiry, false},
		{"after expiry", "tedarik", expiry.Add(time.Hour), false},
		{"no expiry, far future", "tramer", expiry.AddDate(100, 0, 0), true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := BucketTokenMatchesAt(c.bucket, validSecret, c.now); got != c.want {
				t.Fatalf("BucketTokenMatchesAt(%q, ..., %s) = %v, want %v", c.bucket, c.now, got, c.want)
			}
		})
	}

	// An expired token must not become valid just because the secret is right.
	if BucketTokenMatchesAt("tedarik", validSecret, expiry.Add(time.Hour)) {
		t.Error("expired token accepted with the correct secret")
	}
	// ...nor should a wrong secret pass while the token is still live.
	if BucketTokenMatchesAt("tedarik", "wrong", expiry.Add(-time.Hour)) {
		t.Error("wrong secret accepted before expiry")
	}
}

func TestLoadBucketTokensRejectsMalformedExpiry(t *testing.T) {
	for _, value := range []string{"2026-12-31", "31/12/2026", "tomorrow", "2026-13-45T00:00:00Z"} {
		t.Run(value, func(t *testing.T) {
			path := writeTokenFile(t, `{"buckets":[{"bucket":"tedarik","token":"`+validSecret+`","expires_at":"`+value+`"}]}`)
			if _, err := LoadBucketTokens(path); err == nil {
				t.Fatal("malformed expires_at accepted; a mistyped expiry must not silently mean 'never expires'")
			}
		})
	}
}

func TestBucketTokenExpiryWarnings(t *testing.T) {
	now := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	path := writeTokenFile(t, `{"buckets":[
		{"bucket":"gone","token":"`+validSecret+`","expires_at":"2026-07-01T00:00:00Z"},
		{"bucket":"soon","token":"`+validSecret+`","expires_at":"2026-07-30T00:00:00Z"},
		{"bucket":"later","token":"`+validSecret+`","expires_at":"2027-01-01T00:00:00Z"},
		{"bucket":"never","token":"`+validSecret+`"}
	]}`)

	if _, err := LoadBucketTokens(path); err != nil {
		t.Fatalf("load failed: %v", err)
	}

	expired, expiringSoon := BucketTokenExpiryWarnings(now, 7*24*time.Hour)

	if len(expired) != 1 || expired[0] != "gone" {
		t.Errorf("expired = %v, want [gone]", expired)
	}
	if len(expiringSoon) != 1 || expiringSoon[0] != "soon" {
		t.Errorf("expiringSoon = %v, want [soon]", expiringSoon)
	}
}

// TestLoadBucketTokensErrorDoesNotLeakSecret keeps the token value out of the
// boot log, which is where a load error ends up.
func TestLoadBucketTokensErrorDoesNotLeakSecret(t *testing.T) {
	secret := "this-secret-must-never-appear-in-an-error-message"
	path := writeTokenFile(t, `{"buckets":[{"bucket":"BAD NAME","token":"`+secret+`"}]}`)

	_, err := LoadBucketTokens(path)
	if err == nil {
		t.Fatal("invalid entry accepted")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaks the token: %q", err.Error())
	}
}

// TestLoadBucketTokensTrimsFields documents that surrounding whitespace in the
// JSON is tolerated, so a hand-edited file does not fail confusingly.
func TestLoadBucketTokensTrimsFields(t *testing.T) {
	path := writeTokenFile(t, `{"buckets":[{"bucket":"  tedarik  ","token":"  `+validSecret+`  "}]}`)
	if _, err := LoadBucketTokens(path); err != nil {
		t.Fatalf("padded entry rejected: %v", err)
	}
	if !BucketTokenMatches("tedarik", validSecret) {
		t.Error("padded entry did not resolve to the trimmed bucket and secret")
	}
}
