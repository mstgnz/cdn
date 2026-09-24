package middleware

import (
	"net"
	"net/http"
	"strings"

	"github.com/mstgnz/cdn/service"
)

// ClientIP returns the real originating client IP for rate limiting.
//
// Cloudflare sits in front of this service and sets CF-Connecting-IP to the
// originating client; a browser cannot forge it through Cloudflare. We do NOT
// trust X-Real-IP (the docker nginx hop overwrites it with the upstream hop's
// address) nor the leftmost X-Forwarded-For entry (attacker-controllable —
// Cloudflare appends the real client after any client-supplied value). The TCP
// peer is only a degenerate fallback for non-Cloudflare/internal access; without
// this helper every request collapses onto the proxy IP and shares one
// rate-limit bucket.
//
// Trust note: CF-Connecting-IP is only authoritative while the origin is
// reachable ONLY via Cloudflare. Lock the origin firewall to Cloudflare IP
// ranges so an attacker cannot hit the origin directly and spoof this header.
func ClientIP(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); ip != "" {
		return ip
	}
	// The TCP peer without its port, as fiber's c.IP() returned it.
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// RateLimitKey derives the rate-limit bucket for a request from the real client
// IP and the *verified* identity of its credential.
//
// Two properties matter here, and the previous "<ip>:<raw bearer>" key had
// neither:
//
//  1. No secret in the keyspace. The key is built from the identity a credential
//     resolves to (the general token, or a bucket name), never from the
//     credential itself, so token material never reaches Redis.
//  2. An unverified credential cannot mint a fresh counter. Because the old key
//     trusted the raw header, a client sending a different random token on every
//     request got a brand-new counter each time and was effectively never rate
//     limited. Anything that does not authenticate now shares the plain per-IP
//     bucket, which is the limit it was always meant to be under.
//
// Bucket names are validated at load time to lowercase letters, digits and '-',
// so a bucket name can never inject a separator into the key.
//
// The rate limiter is mounted before the auth middleware, so resolving here is
// deliberately read-only: rejecting a bad credential stays the auth middleware's
// job, this only decides which counter the request belongs to.
func RateLimitKey(r *http.Request) string {
	ip := ClientIP(r)

	p, err := service.ResolvePrincipal(r)
	if err != nil {
		return ip
	}
	if p.Scoped {
		return ip + "|bucket:" + p.Bucket
	}
	return ip + "|general"
}
