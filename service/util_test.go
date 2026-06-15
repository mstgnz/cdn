package service

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestIsInt(t *testing.T) {
	// IsInt reports true when at least one argument parses as an integer.
	cases := []struct {
		one, two string
		want     bool
	}{
		{"1", "2", true},
		{"1", "x", true},
		{"x", "2", true},
		{"x", "y", false},
		{"", "", false},
	}
	for _, c := range cases {
		if got := IsInt(c.one, c.two); got != c.want {
			t.Errorf("IsInt(%q,%q) = %v, want %v", c.one, c.two, got, c.want)
		}
	}
}

func TestSetWidthToHeight(t *testing.T) {
	cases := []struct {
		w, h, wantW, wantH string
	}{
		{"100", "", "100", "100"},
		{"", "100", "100", "100"},
		{"100", "50", "100", "50"},
		{"", "", "", ""},
	}
	for _, c := range cases {
		gotW, gotH := SetWidthToHeight(c.w, c.h)
		if gotW != c.wantW || gotH != c.wantH {
			t.Errorf("SetWidthToHeight(%q,%q) = (%q,%q), want (%q,%q)", c.w, c.h, gotW, gotH, c.wantW, c.wantH)
		}
	}
}

func TestStreamToByte(t *testing.T) {
	if got := StreamToByte(strings.NewReader("hello world")); string(got) != "hello world" {
		t.Errorf("StreamToByte = %q, want %q", got, "hello world")
	}
	if got := StreamToByte(strings.NewReader("")); len(got) != 0 {
		t.Errorf("StreamToByte(empty) = %q, want empty", got)
	}
}

func TestRandomName(t *testing.T) {
	got := RandomName(10)
	if got == "" {
		t.Fatal("RandomName returned empty string")
	}
	if !strings.Contains(got, "_") {
		t.Errorf("RandomName = %q, want '<micros>_<n>' format", got)
	}
}

func TestImageToByte(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob.bin")
	want := []byte("some file bytes")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if got := ImageToByte(path); string(got) != string(want) {
		t.Errorf("ImageToByte = %q, want %q", got, want)
	}
}

func TestDownloadFile(t *testing.T) {
	const payload = "downloaded-content"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.txt")
	if err := DownloadFile(dest, srv.URL); err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(got) != payload {
		t.Errorf("downloaded = %q, want %q", got, payload)
	}
}

func TestSanitizeObjectName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"a*b|c\\d&e", "abcde"},   // dangerous characters stripped
		{"foo bar", "foo_bar"},    // whitespace -> underscore
		{"şğıöçü", "sgiocu"},      // turkish characters transliterated
		{"a   b", "a_b"},          // collapsed underscores
		{"", "file"},              // empty -> fallback
		{"__lead.trail__", "lead.trail"}, // trimmed underscores/dots
	}
	for _, c := range cases {
		if got := SanitizeObjectName(c.in); got != c.want {
			t.Errorf("SanitizeObjectName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRatioWidthHeight(t *testing.T) {
	// 200x100 source. Asking for width only should derive proportional height.
	if w, h := RatioWidthHeight(200, 100, 100, 0); w != 100 || h != 50 {
		t.Errorf("width-only: got (%d,%d), want (100,50)", w, h)
	}
	// Asking for height only should derive proportional width.
	if w, h := RatioWidthHeight(200, 100, 0, 50); w != 100 || h != 50 {
		t.Errorf("height-only: got (%d,%d), want (100,50)", w, h)
	}
}

func TestGetWidthAndHeight_Query(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error {
		resize, w, h := GetWidthAndHeight(c, QueryType)
		return c.JSON(fiber.Map{"resize": resize, "w": w, "h": h})
	})

	// With a width param -> resize true.
	resp, err := app.Test(httptest.NewRequest("GET", "/?width=100", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	// Without params -> resize false.
	resp2, _ := app.Test(httptest.NewRequest("GET", "/", nil))
	if resp2.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d", resp2.StatusCode)
	}
}

// TestCheckToken covers the auth helper, including the regression guard that a
// failed check must NOT leak the configured server token in its error.
func TestCheckToken(t *testing.T) {
	const serverToken = "super-secret-token-value"
	t.Setenv("TOKEN", serverToken)

	run := func(authHeader string) error {
		app := fiber.New()
		var captured error
		app.Get("/", func(c *fiber.Ctx) error {
			captured = CheckToken(c)
			return c.SendString("done")
		})
		req := httptest.NewRequest("GET", "/", nil)
		if authHeader != "" {
			req.Header.Set("Authorization", authHeader)
		}
		if _, err := app.Test(req); err != nil {
			t.Fatalf("request failed: %v", err)
		}
		return captured
	}

	if err := run("Bearer " + serverToken); err != nil {
		t.Errorf("valid token rejected: %v", err)
	}
	if err := run(""); err == nil {
		t.Error("missing header accepted")
	}
	if err := run("BadFormat"); err == nil {
		t.Error("malformed header accepted")
	}
	err := run("Bearer wrong-token")
	if err == nil {
		t.Fatal("wrong token accepted")
	}
	if strings.Contains(err.Error(), serverToken) {
		t.Fatalf("error leaks the server token: %q", err.Error())
	}
}
