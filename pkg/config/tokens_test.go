package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
