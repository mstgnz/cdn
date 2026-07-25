package config

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mstgnz/cdn/pkg/bucket"
)

// MinBucketTokenLength is the minimum accepted length of a bucket-scoped token
// secret. Bucket tokens are machine-generated credentials, so there is no reason
// for a short one and a weak entry would silently widen the write surface.
const MinBucketTokenLength = 32

// BucketToken is one entry of the token file.
type BucketToken struct {
	Bucket string `json:"bucket"`
	Token  string `json:"token"`
	// Label is an optional free-text note (who or what uses this token). It is
	// never used for authorization.
	Label string `json:"label,omitempty"`
}

// TokenConfig is the on-disk shape of the token file.
type TokenConfig struct {
	Buckets []BucketToken `json:"buckets"`
}

// bucketTokens maps a bucket name to the SHA-256 digest of its secret. Secrets
// are never kept in plaintext once loaded. The map is populated by
// LoadBucketTokens before the HTTP server starts accepting requests and is
// read-only from then on, so it needs no lock.
var bucketTokens = map[string][sha256.Size]byte{}

// LoadBucketTokens reads bucket-scoped tokens from path and returns how many
// were loaded.
//
// A missing file is not an error: it is the normal state of a deployment that
// uses only the general TOKEN, and boot must succeed unchanged in that case. A
// file that exists but cannot be parsed or fails validation IS an error, because
// silently serving with zero bucket tokens after an operator wrote the file is
// the worse failure: callers would fall back to "invalid token" with no clue why.
func LoadBucketTokens(path string) (int, error) {
	bucketTokens = map[string][sha256.Size]byte{}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read bucket token file %q: %w", path, err)
	}

	var cfg TokenConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return 0, fmt.Errorf("parse bucket token file %q: %w", path, err)
	}

	loaded := make(map[string][sha256.Size]byte, len(cfg.Buckets))
	for idx, entry := range cfg.Buckets {
		name := strings.TrimSpace(entry.Bucket)
		secret := strings.TrimSpace(entry.Token)

		if err := bucket.Validate(name); err != nil {
			return 0, fmt.Errorf("bucket token file %q entry %d: %w", path, idx, err)
		}
		if len(secret) < MinBucketTokenLength {
			return 0, fmt.Errorf("bucket token file %q entry %d (bucket %q): token must be at least %d characters", path, idx, name, MinBucketTokenLength)
		}
		if _, duplicate := loaded[name]; duplicate {
			return 0, fmt.Errorf("bucket token file %q entry %d: duplicate bucket %q", path, idx, name)
		}

		loaded[name] = sha256.Sum256([]byte(secret))
	}

	bucketTokens = loaded
	return len(loaded), nil
}

// BucketTokenMatches reports whether secret is the configured token for
// bucketName, compared in constant time.
//
// An unknown bucket still runs a full comparison against a zero digest so that
// "no token configured for this bucket" and "wrong secret" cost the same.
func BucketTokenMatches(bucketName, secret string) bool {
	want, known := bucketTokens[bucketName]
	got := sha256.Sum256([]byte(secret))
	equal := subtle.ConstantTimeCompare(got[:], want[:]) == 1
	return known && equal
}

// BucketTokenCount returns the number of loaded bucket-scoped tokens.
func BucketTokenCount() int {
	return len(bucketTokens)
}
