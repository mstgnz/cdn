package hostwatch

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPushHeartbeatSetsStatusAndKeepsPath(t *testing.T) {
	var got *url.URL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	hb, err := NewPushHeartbeat(srv.URL + "/api/push/fake-push-token?status=up&msg=OK&ping=")
	if err != nil {
		t.Fatal(err)
	}
	if err := hb.Beat(context.Background(), false, "hostwatch cannot deliver alert mail"); err != nil {
		t.Fatalf("Beat: %v", err)
	}
	q := got.Query()
	if got.Path != "/api/push/fake-push-token" || q.Get("status") != "down" || q.Get("msg") != "hostwatch cannot deliver alert mail" {
		t.Fatalf("request = %s", got)
	}

	if err := hb.Beat(context.Background(), true, strings.Repeat("m", 500)); err != nil {
		t.Fatal(err)
	}
	if q := got.Query(); q.Get("status") != "up" || len(q.Get("msg")) != 200 {
		t.Fatalf("status %q msg length %d", q.Get("status"), len(q.Get("msg")))
	}
}

func TestPushHeartbeatReportsRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"ok":false,"msg":"Monitor not found or not active."}`, http.StatusNotFound)
	}))
	defer srv.Close()

	hb, _ := NewPushHeartbeat(srv.URL + "/api/push/fake-push-token")
	err := hb.Beat(context.Background(), true, "OK")
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("err = %v", err)
	}
}

func TestPushHeartbeatErrorHidesToken(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	hb, _ := NewPushHeartbeat("http://" + addr + "/api/push/fake-push-token")
	err = hb.Beat(context.Background(), true, "OK")
	if err == nil {
		t.Fatal("beat to a closed port succeeded")
	}
	if strings.Contains(err.Error(), "fake-push-token") {
		t.Fatalf("error leaks the push token: %v", err)
	}
}

func TestNewPushHeartbeatRejectsGarbage(t *testing.T) {
	if _, err := NewPushHeartbeat("http://[::1"); err == nil {
		t.Fatal("accepted an unparsable URL")
	}
}
