package handler

import (
	"bytes"
	"io"
	"testing"
)

// fakeCloser records whether Close was called.
type fakeCloser struct {
	data   *bytes.Reader
	closed bool
}

func (f *fakeCloser) Read(p []byte) (int, error) { return f.data.Read(p) }
func (f *fakeCloser) Close() error               { f.closed = true; return nil }

// streamCloser must satisfy io.ReadCloser so fasthttp closes it after writing
// the response (which is what releases the MinIO object).
var _ io.ReadCloser = streamCloser{}

// TestStreamCloser_ReadsAllAndClosesUnderlying verifies the streamed body is
// read intact and that closing it propagates to the underlying object closer.
// This guards against the MinIO object being leaked (never closed) or closed
// too early on the non-resize streaming path.
func TestStreamCloser_ReadsAllAndClosesUnderlying(t *testing.T) {
	const payload = "the quick brown fox jumps over the lazy dog"
	underlying := &fakeCloser{data: bytes.NewReader([]byte(payload))}

	// Mirror the handler: a peeked head replayed in front of the rest.
	head := []byte(payload[:10])
	rest := bytes.NewReader([]byte(payload[10:]))
	sc := streamCloser{
		Reader: io.MultiReader(bytes.NewReader(head), rest),
		closer: underlying,
	}

	got, err := io.ReadAll(sc)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("stream content mismatch: got %q want %q", got, payload)
	}

	if underlying.closed {
		t.Fatal("underlying closer was closed before Close() was called")
	}
	if err := sc.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if !underlying.closed {
		t.Fatal("Close() did not close the underlying object (leak)")
	}
}
