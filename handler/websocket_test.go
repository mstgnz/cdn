package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/mstgnz/cdn/pkg/httpx"
)

func TestHandshakeChecks(t *testing.T) {
	good := func() *http.Request {
		r := httptest.NewRequest("GET", "/ws", nil)
		r.Header.Set("Connection", "keep-alive, Upgrade")
		r.Header.Set("Upgrade", "WebSocket")
		r.Header.Set("Sec-WebSocket-Version", "13")
		r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
		return r
	}
	if !IsUpgrade(good()) || !validHandshake(good()) {
		t.Fatal("a browser handshake was refused")
	}

	cases := map[string]func(*http.Request){
		"no key":        func(r *http.Request) { r.Header.Del("Sec-WebSocket-Key") },
		"short key":     func(r *http.Request) { r.Header.Set("Sec-WebSocket-Key", "c2hvcnQ=") },
		"version 8":     func(r *http.Request) { r.Header.Set("Sec-WebSocket-Version", "8") },
		"not GET":       func(r *http.Request) { r.Method = http.MethodHead },
		"no upgrade":    func(r *http.Request) { r.Header.Del("Upgrade") },
		"no connection": func(r *http.Request) { r.Header.Set("Connection", "keep-alive") },
	}
	for name, spoil := range cases {
		r := good()
		spoil(r)
		if validHandshake(r) {
			t.Errorf("%s: accepted", name)
		}
	}
}

// A refused handshake answers what gofiber answered, not coder/websocket's text,
// and like fasthttp's upgrader it resets headers set before it.
func TestHandleWebSocketRefusesBadHandshake(t *testing.T) {
	h := NewWebSocketHandler(nil)
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	rec := httptest.NewRecorder()
	httpx.Wrap(0, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		httpx.Handler(h.HandleWebSocket).ServeHTTP(w, r)
	})).ServeHTTP(rec, req)
	if rec.Code != http.StatusUpgradeRequired || rec.Body.String() != "Upgrade Required" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Content-Type-Options") != "" {
		t.Fatal("a header set before the refusal survived it")
	}
}

func TestHandleWebSocketStreamsStats(t *testing.T) {
	h := NewWebSocketHandler(nil)
	srv := httptest.NewServer(httpx.Wrap(time.Second, httpx.Handler(h.HandleWebSocket)))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.CloseNow()
	if resp.Header.Get("Sec-Websocket-Accept") == "" {
		t.Fatalf("handshake headers: %v", resp.Header)
	}

	typ, msg, err := c.Read(ctx)
	if err != nil || typ != websocket.MessageText {
		t.Fatalf("first frame: %v %v", typ, err)
	}
	var stats MonitoringStats
	if err := json.Unmarshal(msg, &stats); err != nil || stats.Timestamp.IsZero() {
		t.Fatalf("frame is not a stats document: %s (%v)", msg, err)
	}
}

func TestMonitorStatsEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	httpx.Handler(NewWebSocketHandler(nil).MonitorStats).ServeHTTP(rec, httptest.NewRequest("GET", "/monitor", nil))
	var body struct {
		Success bool            `json:"success"`
		Message string          `json:"message"`
		Data    MonitoringStats `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || !body.Success || body.Message != "Current monitoring stats" {
		t.Fatalf("got %d %s (%v)", rec.Code, rec.Body.String(), err)
	}
}
