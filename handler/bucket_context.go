package handler

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"
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
func resolveBucket(c *fiber.Ctx, requested string) (string, error) {
	requested = strings.TrimSpace(requested)

	p := service.PrincipalFrom(c)
	if !p.Scoped {
		return requested, nil
	}
	if requested == "" || requested == p.Bucket {
		return p.Bucket, nil
	}
	return "", errBucketForbidden
}

// bucketForbidden writes the 403 for a bucket mismatch. The message names no
// bucket, so a scoped token cannot be used to probe which buckets exist.
func bucketForbidden(c *fiber.Ctx) error {
	return service.Response(c, fiber.StatusForbidden, false, errBucketForbidden.Error(), nil)
}
