package validator

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"

	"github.com/mstgnz/cdn/pkg/config"
)

// allowPrivateTargets reports whether SSRF protection is intentionally disabled.
// Self-hosted deployments that fetch from internal hosts can opt out via
// UPLOAD_URL_ALLOW_PRIVATE=true; it defaults to false (protection on).
func allowPrivateTargets() bool {
	return config.GetEnvAsBoolOrDefault("UPLOAD_URL_ALLOW_PRIVATE", false)
}

// isDisallowedIP reports whether an IP must not be dialed by a server-side
// fetch. It blocks loopback, private, link-local (incl. the 169.254.169.254
// cloud metadata address), multicast and unspecified ranges for both IPv4 and
// the IPv4-mapped IPv6 form.
func isDisallowedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified()
}

// ValidateUploadURL enforces scheme and, for literal-IP hosts, IP-range policy
// before any network request is made. Hostname-based targets are additionally
// re-checked at connect time by NewSafeHTTPClient's dialer (defeats DNS
// rebinding), so this function only rejects the statically-detectable cases.
func ValidateUploadURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported url scheme: only http and https are allowed")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("invalid url: missing host")
	}
	if allowPrivateTargets() {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && isDisallowedIP(ip) {
		return fmt.Errorf("url resolves to a disallowed address")
	}
	return nil
}

// NewSafeHTTPClient returns an http.Client that rejects connections to
// disallowed IP ranges at dial time (no DNS TOCTOU window) and bounds redirects
// to 3 hops, re-validating the scheme on each. When UPLOAD_URL_ALLOW_PRIVATE is
// set, the IP check is skipped.
func NewSafeHTTPClient(timeout time.Duration) *http.Client {
	allowPrivate := allowPrivateTargets()

	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			if allowPrivate {
				return nil
			}
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return fmt.Errorf("invalid dial address")
			}
			ip := net.ParseIP(host)
			if isDisallowedIP(ip) {
				return fmt.Errorf("connection to disallowed address blocked")
			}
			return nil
		},
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("unsupported redirect scheme")
			}
			return nil
		},
	}
}
