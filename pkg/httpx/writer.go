// Package httpx carries the request and response semantics the service had under
// fiber onto net/http, so the HTTP contract stays byte-identical across the move.
// scripts/e2e-golden is the contract; the comments here say which fiber
// behaviour each piece reproduces.
package httpx

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"slices"
	"time"
)

// Writer is the one ResponseWriter wrapper of a request, installed by Wrap.
//
// Late headers reproduce fiber middleware that set headers after c.Next(): they
// are applied when the status is written, innermost first, so an outer
// middleware's value wins exactly as it did when it ran last.
type Writer struct {
	http.ResponseWriter
	status       int
	late         []func(http.Header)
	writeTimeout time.Duration
}

// Wrap installs the Writer for every request. writeTimeout restarts the write
// deadline when the response starts, as fasthttp did: net/http's WriteTimeout
// alone runs from the end of the request headers, so a slow upload plus its
// processing would eat into the time left to answer.
func Wrap(writeTimeout time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&Writer{ResponseWriter: w, writeTimeout: writeTimeout}, r)
	})
}

func (w *Writer) restartWriteDeadline() {
	if w.writeTimeout > 0 {
		_ = http.NewResponseController(w.ResponseWriter).SetWriteDeadline(time.Now().Add(w.writeTimeout))
	}
}

// From returns the request's Writer, or nil when Wrap is not installed.
func From(w http.ResponseWriter) *Writer {
	for {
		switch t := w.(type) {
		case *Writer:
			return t
		case interface{ Unwrap() http.ResponseWriter }:
			w = t.Unwrap()
		default:
			return nil
		}
	}
}

// Late registers headers to apply when the status is written.
func Late(w http.ResponseWriter, fn func(http.Header)) {
	if x := From(w); x != nil {
		x.late = append(x.late, fn)
	}
}

// DropLate forgets registered late headers: a panic unwound past the fiber
// middleware that would have set them.
func DropLate(w http.ResponseWriter) {
	if x := From(w); x != nil {
		x.late = nil
	}
}

// Status is the status written so far, 0 before the first write.
func (w *Writer) Status() int { return w.status }

func (w *Writer) WriteHeader(code int) {
	if w.status != 0 && code >= 200 {
		return
	}
	for _, fn := range slices.Backward(w.late) {
		fn(w.Header())
	}
	if code >= 200 {
		w.status = code
		w.late = nil
		w.restartWriteDeadline()
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *Writer) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// ResetHeaders drops every header set so far, as fasthttp's file server did by
// resetting the response for 304 and 416. Late headers survive: fiber's
// limiter set them after c.Next() returned, so after the reset.
func (w *Writer) ResetHeaders() {
	for k := range w.Header() {
		delete(w.Header(), k)
	}
}

func (w *Writer) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *Writer) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack lets the websocket upgrade take the connection.
func (w *Writer) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("httpx: response writer cannot be hijacked")
	}
	return h.Hijack()
}
