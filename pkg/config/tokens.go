package config

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mstgnz/cdn/pkg/bucket"
)

// MinBucketTokenLength is the minimum accepted length of a bucket-scoped token
// secret. Bucket tokens are machine-generated credentials, so there is no reason
// for a short one and a weak entry would silently widen the write surface.
const MinBucketTokenLength = 32

// placeholderTokens are values that ship in templates, examples and tutorials.
// None of them is ever a real credential, so they are rejected whatever their
// length. Compared lowercased.
var placeholderTokens = map[string]struct{}{
	"your-token-here":   {},
	"your-secure-token": {},
	"your_token_here":   {},
	"replace_me":        {},
	"replace-me":        {},
	"changeme":          {},
	"change-me":         {},
	"token":             {},
	"secret":            {},
	"password":          {},
	"test":              {},
}

// ValidateGeneralToken reports whether the general TOKEN is fit to authenticate
// with. It is enforced at boot, not per request, so a bad value fails loudly at
// deploy time instead of quietly weakening every authenticated endpoint.
//
// The same floor as bucket-scoped tokens applies: MinBucketTokenLength. A general
// token is strictly more powerful than a scoped one, since it is accepted on the
// operator routes as well, so it cannot reasonably be held to a weaker standard.
//
// Errors never contain the token itself: they end up in the boot log.
func ValidateGeneralToken(token string) error {
	token = strings.TrimSpace(token)

	if token == "" {
		return errors.New("TOKEN must be set (authentication would otherwise be bypassable)")
	}
	if _, placeholder := placeholderTokens[strings.ToLower(token)]; placeholder {
		return errors.New("TOKEN is still set to a template placeholder value; generate one with: openssl rand -hex 32")
	}
	if len(token) < MinBucketTokenLength {
		return fmt.Errorf("TOKEN must be at least %d characters; generate one with: openssl rand -hex 32", MinBucketTokenLength)
	}
	return nil
}

// BucketToken is one entry of the token file.
type BucketToken struct {
	Bucket string `json:"bucket"`
	Token  string `json:"token"`
	// Label is an optional free-text note (who or what uses this token). It is
	// never used for authorization.
	Label string `json:"label,omitempty"`
	// ExpiresAt is an optional RFC 3339 timestamp. Empty means the token never
	// expires, which is what every entry written before this field existed means.
	ExpiresAt string `json:"expires_at,omitempty"`
}

// bucketCredential is the in-memory form of a BucketToken. The secret is kept
// only as a digest.
type bucketCredential struct {
	digest [sha256.Size]byte
	// expiresAt is the zero time when the token never expires.
	expiresAt time.Time
}

// TokenConfig is the on-disk shape of the token file.
type TokenConfig struct {
	Buckets []BucketToken `json:"buckets"`
}

// bucketTokens maps a bucket name to its credential. Secrets are never kept in
// plaintext once loaded. The map is populated by LoadBucketTokens before the HTTP
// server starts accepting requests and is read-only from then on, so it needs no
// lock.
var bucketTokens = map[string]bucketCredential{}

// LoadBucketTokens reads bucket-scoped tokens from path and returns how many
// were loaded.
//
// The line this draws is between "no scoped tokens" and "scoped tokens that
// cannot be read", not between "no file" and "some file".
//
// Boot succeeds with zero bucket tokens when the file is missing, empty, or
// holds no entries ({}, {"buckets":[]}, {"buckets":null}). All of those are an
// operator saying there are none yet, which is the normal state of a deployment
// running on the general TOKEN alone, and none of them can be hiding a
// credential.
//
// Boot fails when the file has content that cannot be parsed, or entries that
// fail validation. There the operator wrote definitions and believes they are
// active; starting anyway would serve with fewer credentials than configured and
// the callers using them would get "invalid token" with nothing to explain it.
//
// Neither outcome affects the general TOKEN, which is validated separately and
// is accepted on every route regardless of what this file contains.
func LoadBucketTokens(path string) (int, error) {
	bucketTokens = map[string]bucketCredential{}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read bucket token file %q: %w", path, err)
	}

	// An empty file is zero definitions, not a broken one. `touch
	// config/tokens.json`, or a file emptied while rotating credentials, says
	// there are no scoped tokens yet; there is nothing else it could mean. It is
	// only once the file has content that failing to read it becomes dangerous,
	// because then definitions may exist that the operator believes are active.
	if len(bytes.TrimSpace(raw)) == 0 {
		return 0, nil
	}

	var cfg TokenConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return 0, fmt.Errorf("parse bucket token file %q: %w", path, err)
	}

	loaded := make(map[string]bucketCredential, len(cfg.Buckets))
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

		// An unparseable timestamp fails the load rather than being treated as
		// "no expiry": silently granting a token unlimited life because its
		// expiry was mistyped is the wrong way to fail.
		var expiresAt time.Time
		if raw := strings.TrimSpace(entry.ExpiresAt); raw != "" {
			parsed, parseErr := time.Parse(time.RFC3339, raw)
			if parseErr != nil {
				return 0, fmt.Errorf("bucket token file %q entry %d (bucket %q): expires_at must be an RFC 3339 timestamp such as 2026-12-31T00:00:00Z", path, idx, name)
			}
			expiresAt = parsed
		}

		loaded[name] = bucketCredential{
			digest:    sha256.Sum256([]byte(secret)),
			expiresAt: expiresAt,
		}
	}

	bucketTokens = loaded
	return len(loaded), nil
}

// BucketTokenMatches reports whether secret is the configured, unexpired token
// for bucketName.
func BucketTokenMatches(bucketName, secret string) bool {
	return BucketTokenMatchesAt(bucketName, secret, time.Now())
}

// BucketTokenMatchesAt is BucketTokenMatches with an explicit clock, so expiry
// can be tested without waiting for it.
//
// Expiry is evaluated here, per request, rather than at load time. The file is
// only read at boot, so checking it there would mean a token stayed valid until
// the next restart; checking it here makes it stop working at the moment it
// expires.
//
// The comparison is constant time, and an unknown bucket still runs a full
// comparison against a zero digest so that "no token configured for this bucket"
// and "wrong secret" cost the same.
func BucketTokenMatchesAt(bucketName, secret string, now time.Time) bool {
	credential, known := bucketTokens[bucketName]
	got := sha256.Sum256([]byte(secret))
	equal := subtle.ConstantTimeCompare(got[:], credential.digest[:]) == 1
	live := credential.expiresAt.IsZero() || now.Before(credential.expiresAt)
	return known && equal && live
}

// BucketTokenExpiryWarnings reports which loaded tokens have already expired and
// which expire within the given window.
//
// Returned rather than logged so that this package stays free of a logging
// dependency; the caller logs them at boot. An expired token is not an error:
// the deployment is still safe, it is the callers who will start failing, and an
// operator should hear about that before they do.
func BucketTokenExpiryWarnings(now time.Time, within time.Duration) (expired []string, expiringSoon []string) {
	for name, credential := range bucketTokens {
		if credential.expiresAt.IsZero() {
			continue
		}
		switch {
		case !now.Before(credential.expiresAt):
			expired = append(expired, name)
		case credential.expiresAt.Sub(now) <= within:
			expiringSoon = append(expiringSoon, name)
		}
	}
	sort.Strings(expired)
	sort.Strings(expiringSoon)
	return expired, expiringSoon
}

// BucketTokenCount returns the number of loaded bucket-scoped tokens.
func BucketTokenCount() int {
	return len(bucketTokens)
}
