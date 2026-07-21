package validator

import (
	"net"
	"testing"
)

func TestValidateUploadURL(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"file scheme", "file:///etc/passwd", true},
		{"ftp scheme", "ftp://example.com/x", true},
		{"missing host", "http://", true},
		{"loopback v4", "http://127.0.0.1/a", true},
		{"loopback v6", "http://[::1]/a", true},
		{"private v4", "http://10.0.0.1/a", true},
		{"link-local metadata", "http://169.254.169.254/latest/meta-data", true},
		{"unspecified", "http://0.0.0.0/", true},
		{"public https", "https://example.com/a.png", false},
		{"public http", "http://example.com/a.png", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateUploadURL(tc.url)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateUploadURL(%q) err=%v, wantErr=%v", tc.url, err, tc.wantErr)
			}
		})
	}
}

func TestValidateUploadURL_AllowPrivateOptOut(t *testing.T) {
	t.Setenv("UPLOAD_URL_ALLOW_PRIVATE", "true")
	if err := ValidateUploadURL("http://127.0.0.1/a"); err != nil {
		t.Fatalf("with UPLOAD_URL_ALLOW_PRIVATE=true, loopback should pass, got %v", err)
	}
}

func TestIsDisallowedIP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},
		{"10.1.2.3", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true},
		{"0.0.0.0", true},
		{"::1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
	}
	for _, tc := range cases {
		if got := isDisallowedIP(net.ParseIP(tc.ip)); got != tc.want {
			t.Errorf("isDisallowedIP(%s) = %v, want %v", tc.ip, got, tc.want)
		}
	}
	if !isDisallowedIP(nil) {
		t.Error("nil IP must be treated as disallowed")
	}
}
