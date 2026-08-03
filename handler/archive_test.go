package handler

import (
	"testing"
)

// Callers store the URL this service handed them at upload time, so the endpoint
// accepts either that or a bare key. What it must not accept is a URL pointing
// somewhere else: the field looks like free text, and a link belonging to another
// host or another tenant cannot be allowed to select the object.
func TestNormalizeObjectKey(t *testing.T) {
	t.Setenv("APP_URL", "https://cdn.example.com")

	cases := []struct {
		name    string
		input   string
		bucket  string
		want    string
		wantErr bool
	}{
		{
			name:   "bare key passes through",
			input:  "ihale/2026/144545/photo.jpg",
			bucket: "sovtajyeri",
			want:   "ihale/2026/144545/photo.jpg",
		},
		{
			name:   "leading slash is tolerated",
			input:  "/ihale/2026/photo.jpg",
			bucket: "sovtajyeri",
			want:   "ihale/2026/photo.jpg",
		},
		{
			name:   "full CDN URL is reduced to its key",
			input:  "https://cdn.example.com/sovtajyeri/ihale/2026/photo.jpg",
			bucket: "sovtajyeri",
			want:   "ihale/2026/photo.jpg",
		},
		{
			// Links are stored for years; a deployment that moved to TLS should not
			// invalidate every URL its callers wrote down. The host still has to match.
			name:   "scheme may differ from APP_URL",
			input:  "http://cdn.example.com/sovtajyeri/photo.jpg",
			bucket: "sovtajyeri",
			want:   "photo.jpg",
		},
		{
			name:   "query string is not part of the key",
			input:  "https://cdn.example.com/sovtajyeri/photo.jpg?width=900",
			bucket: "sovtajyeri",
			want:   "photo.jpg",
		},
		{
			name:    "another host is refused",
			input:   "https://evil.example.net/sovtajyeri/photo.jpg",
			bucket:  "sovtajyeri",
			wantErr: true,
		},
		{
			// The bucket has already been reconciled with the caller's token, so a
			// URL naming a different one is an attempt to reach outside it.
			name:    "another bucket is refused",
			input:   "https://cdn.example.com/dos/photo.jpg",
			bucket:  "sovtajyeri",
			wantErr: true,
		},
		{
			name:    "traversal key is refused",
			input:   "../../etc/passwd",
			bucket:  "sovtajyeri",
			wantErr: true,
		},
		{
			name:    "traversal smuggled through a URL is refused",
			input:   "https://cdn.example.com/sovtajyeri/../../etc/passwd",
			bucket:  "sovtajyeri",
			wantErr: true,
		},
		{
			name:    "empty reference is refused",
			input:   "   ",
			bucket:  "sovtajyeri",
			wantErr: true,
		},
		{
			name:    "URL with no key beyond the bucket is refused",
			input:   "https://cdn.example.com/sovtajyeri/",
			bucket:  "sovtajyeri",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeObjectKey(tc.input, tc.bucket)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected a rejection, got key %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("key: want %q, got %q", tc.want, got)
			}
		})
	}
}
