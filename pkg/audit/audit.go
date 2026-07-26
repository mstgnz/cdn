// Package audit records security-relevant events to the application log:
// rejected credentials and denied bucket access.
//
// It sits in its own package because the events are emitted from two places
// that do not otherwise share code, the auth middlewares in cmd and the write
// handlers, and because it cannot live in pkg/observability: reaching the
// trusted client IP means importing pkg/middleware, and middleware imports
// service, which imports observability.
//
// Two rules hold for everything here:
//
//   - No credential material is ever logged, in any form, not truncated and not
//     hashed. Only the reason a request was rejected and the identity it
//     resolved to, which is a bucket name and never a secret.
//   - Successful authentication is not logged. On a CDN that is the hot path,
//     and the access log already covers it.
package audit

import (
	"github.com/gofiber/fiber/v2"
	"github.com/mstgnz/cdn/pkg/middleware"
	"github.com/mstgnz/cdn/pkg/observability"
)

// Event names are stable identifiers so log queries and alerts can key on them
// without matching free text.
const (
	EventAuthFailure        = "auth.failure"
	EventScopedTokenOnAdmin = "auth.scoped_token_on_operator_route"
	EventBucketAccessDenied = "auth.bucket_access_denied"
)

// AuthFailure records a credential that was rejected on an authenticated route.
// reason comes from the auth sentinels (no token, malformed header, invalid
// token), which describe the failure without echoing what was sent.
func AuthFailure(c *fiber.Ctx, reason string) {
	logger := observability.Logger()
	logger.Warn().
		Str("event", EventAuthFailure).
		Str("reason", reason).
		Str("method", c.Method()).
		Str("path", c.Path()).
		Str("ip", middleware.ClientIP(c)).
		Msg("authentication failed")
}

// ScopedTokenOnOperatorRoute records a valid bucket-scoped token presented to a
// route that only accepts the general token. This is logged apart from a plain
// failure because it is almost always a misconfigured client rather than an
// attack, and the two need different responses from an operator.
func ScopedTokenOnOperatorRoute(c *fiber.Ctx, tokenBucket string) {
	logger := observability.Logger()
	logger.Warn().
		Str("event", EventScopedTokenOnAdmin).
		Str("token_bucket", tokenBucket).
		Str("method", c.Method()).
		Str("path", c.Path()).
		Str("ip", middleware.ClientIP(c)).
		Msg("bucket-scoped token rejected on an operator route")
}

// BucketAccessDenied records a bucket-scoped token reaching for a bucket it does
// not own. Both bucket names are logged: the pair is what makes the event
// actionable, since it separates a client pointed at the wrong bucket from
// someone probing for other tenants.
func BucketAccessDenied(c *fiber.Ctx, tokenBucket, requestedBucket string) {
	logger := observability.Logger()
	logger.Warn().
		Str("event", EventBucketAccessDenied).
		Str("token_bucket", tokenBucket).
		Str("requested_bucket", requestedBucket).
		Str("method", c.Method()).
		Str("path", c.Path()).
		Str("ip", middleware.ClientIP(c)).
		Msg("bucket-scoped token denied access to another bucket")
}
