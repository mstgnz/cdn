package handler

import (
	"net/http"
	"strings"
	"testing"
)

// This is the regression test for a stored-XSS hole that survived two security
// audits, because both looked at extensions rather than at what the bytes sniff
// as.
//
// Content validation accepts a file whose bytes are valid UTF-8 text, which is
// how .csv and .sql get through. http.DetectContentType reports text/html for
// anything opening with markup. Together they meant an attacker could upload
// `<script>…</script>` named evil.csv and have this service return it as
// text/html on its own origin, executing on every visitor who opened the link.
//
// X-Content-Type-Options: nosniff does not help. It stops a browser guessing;
// here the browser was not guessing, it was being told.
func TestInertContentTypeNeutralisesExecutableTypes(t *testing.T) {
	cases := []struct {
		name    string
		sniffed string
		want    string
	}{
		{"html with charset", "text/html; charset=utf-8", "text/plain; charset=utf-8"},
		{"bare html", "text/html", "text/plain; charset=utf-8"},
		{"uppercase html", "TEXT/HTML; charset=utf-8", "text/plain; charset=utf-8"},
		{"xhtml", "application/xhtml+xml", "text/plain; charset=utf-8"},
		{"text xml", "text/xml; charset=utf-8", "text/plain; charset=utf-8"},
		{"application xml", "application/xml", "text/plain; charset=utf-8"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := inertContentType(tc.sniffed); got != tc.want {
				t.Errorf("inertContentType(%q) = %q, want %q", tc.sniffed, got, tc.want)
			}
		})
	}
}

// Everything else has to pass through untouched. Downgrading legitimate types
// would break serving: an image that arrives as text/plain does not render, and
// a PDF that does is downloaded instead of opened.
func TestInertContentTypeLeavesSafeTypesAlone(t *testing.T) {
	for _, safe := range []string{
		"image/png",
		"image/jpeg",
		"image/gif",
		"image/webp",
		"application/pdf",
		"application/zip",
		"video/mp4",
		"audio/mpeg",
		"text/plain; charset=utf-8",
		"application/octet-stream",
	} {
		t.Run(safe, func(t *testing.T) {
			if got := inertContentType(safe); got != safe {
				t.Errorf("inertContentType(%q) = %q, want it unchanged", safe, got)
			}
		})
	}
}

// The end-to-end shape of the bug: these are payloads that pass content
// validation as text, and every one of them makes Go's sniffer say HTML.
func TestMarkupPayloadsNeverServeAsHTML(t *testing.T) {
	payloads := []struct {
		name    string
		content string
	}{
		{"script tag", "<script>alert(document.domain)</script>"},
		{"full document", "<html><body><script>alert(1)</script></body></html>"},
		{"doctype", "<!DOCTYPE html><html><head></head><body>x</body></html>"},
		{"leading whitespace", "\n\n   <html><script>alert(1)</script></html>"},
		{"head tag", "<head><title>x</title></head>"},
		{"iframe", "<iframe src=javascript:alert(1)></iframe>"},
	}

	for _, p := range payloads {
		t.Run(p.name, func(t *testing.T) {
			sniffed := http.DetectContentType([]byte(p.content))
			served := inertContentType(sniffed)

			if strings.Contains(strings.ToLower(served), "html") {
				t.Fatalf("payload would be served as %q (sniffed %q); a browser would execute it", served, sniffed)
			}
			if !strings.HasPrefix(served, "text/plain") {
				t.Errorf("served as %q, want text/plain so it renders as inert text", served)
			}
		})
	}
}

// A real CSV still has to serve as text, not be mangled into something else.
func TestOrdinaryTextStillServesAsText(t *testing.T) {
	csv := "id,name,total\n1,\"Ünal, Ayşe\",12.50\n"

	served := inertContentType(http.DetectContentType([]byte(csv)))
	if !strings.HasPrefix(served, "text/plain") {
		t.Errorf("a plain CSV serves as %q, want text/plain", served)
	}
}
