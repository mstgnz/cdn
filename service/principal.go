package service

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/mstgnz/cdn/pkg/config"
)

// Auth failures are deliberately coarse: the caller learns that the credential
// was not accepted and nothing about which part of it failed.
var (
	ErrNoToken           = errors.New("no token provided")
	ErrInvalidAuthFormat = errors.New("invalid authorization format")
	ErrInvalidToken      = errors.New("invalid token")
)

type principalKey struct{}

// Principal is the identity behind an authenticated request.
type Principal struct {
	// Scoped is true when the caller presented a bucket-scoped token. The general
	// TOKEN yields the zero Principal, which is not restricted to one bucket.
	Scoped bool
	// Bucket is the single bucket a scoped principal may write to. Empty when
	// Scoped is false.
	Bucket string
}

// BearerToken extracts the raw bearer credential from the Authorization header.
func BearerToken(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", ErrNoToken
	}

	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 {
		return "", ErrInvalidAuthFormat
	}
	return parts[1], nil
}

// ResolvePrincipal authenticates a request against either the general TOKEN or a
// bucket-scoped token.
//
// Order matters. The general TOKEN is compared first and as a whole string,
// because a general token is free to contain a ':' and must never be
// reinterpreted as a "<bucket>:<secret>" pair. Parsing first would break every
// existing deployment whose TOKEN happens to contain a colon.
//
// For a bucket-scoped token the bucket prefix is attacker-controlled input and
// grants nothing on its own: the secret is compared, in constant time, against
// the entry for that exact bucket and no other.
func ResolvePrincipal(r *http.Request) (Principal, error) {
	raw, err := BearerToken(r)
	if err != nil {
		return Principal{}, err
	}

	if TokenValid(raw) {
		return Principal{}, nil
	}

	name, secret, found := strings.Cut(strings.TrimSpace(raw), ":")
	if !found || !config.BucketTokenMatches(name, secret) {
		return Principal{}, ErrInvalidToken
	}
	return Principal{Scoped: true, Bucket: name}, nil
}

// WithPrincipal returns r carrying p for downstream handlers.
func WithPrincipal(r *http.Request, p Principal) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), principalKey{}, p))
}

// PrincipalFrom returns the principal stored by the auth middleware. A missing
// principal yields the zero value, i.e. the unrestricted general token, so a
// route wired without the bucket-aware middleware keeps its previous behaviour.
func PrincipalFrom(r *http.Request) Principal {
	if p, ok := r.Context().Value(principalKey{}).(Principal); ok {
		return p
	}
	return Principal{}
}
