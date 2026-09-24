package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/mstgnz/cdn/pkg/audit"
	"github.com/mstgnz/cdn/service"
)

// errBucketForbidden is returned when a bucket-scoped token names a bucket other
// than the one it is scoped to.
var errBucketForbidden = errors.New("token is not allowed to access this bucket")

// resolveBucket reconciles the bucket named by the request with the bucket the
// caller's token is scoped to:
//
//   - general token: the requested bucket is returned unchanged, which is the
//     pre-existing behaviour of every write endpoint
//   - scoped token, no bucket in the request: the token's bucket is used, so the
//     form or body field becomes optional for bucket-scoped callers
//   - scoped token, same bucket: allowed
//   - scoped token, different bucket: rejected
//
// An empty result is only reachable with the general token and stays the
// caller's problem to report, since each endpoint words that error differently.
func resolveBucket(r *http.Request, requested string) (string, error) {
	requested = strings.TrimSpace(requested)

	p := service.PrincipalFrom(r)
	if !p.Scoped {
		return requested, nil
	}
	if requested == "" || requested == p.Bucket {
		return p.Bucket, nil
	}

	// Logged here rather than in bucketForbidden because this is the only place
	// that knows both sides of the mismatch, and it covers every write handler
	// at once.
	audit.BucketAccessDenied(r, p.Bucket, requested)
	return "", errBucketForbidden
}

// bucketForbidden writes the 403 for a bucket mismatch. The message names no
// bucket, so a scoped token cannot be used to probe which buckets exist.
func bucketForbidden(w http.ResponseWriter) error {
	return service.Response(w, http.StatusForbidden, false, errBucketForbidden.Error(), nil)
}
