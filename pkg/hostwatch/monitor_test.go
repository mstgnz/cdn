package hostwatch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

type fakeCollector struct {
	samples []Sample
	err     error
}

func (f *fakeCollector) Collect(context.Context) ([]Sample, error) { return f.samples, f.err }

type fakeNotifier struct {
	sent []Message
	fail bool
}

func (f *fakeNotifier) Send(_ context.Context, m Message) error {
	if f.fail {
		return errors.New("connection refused")
	}
	f.sent = append(f.sent, m)
	return nil
}

type beat struct {
	up  bool
	msg string
}

type fakeHeartbeat struct {
	beats []beat
	err   error
}

func (f *fakeHeartbeat) Beat(_ context.Context, up bool, msg string) error {
	f.beats = append(f.beats, beat{up, msg})
	return f.err
}

type harness struct {
	m   *Monitor
	c   *fakeCollector
	n   *fakeNotifier
	hb  *fakeHeartbeat
	now time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	cfg := Config{
		Hostname:    "cdn-test",
		Hysteresis:  3,
		RemindEvery: 6 * time.Hour,
		Rules: map[Kind]Rule{
			KindDisk:   {Warn: 80, Crit: 90},
			KindInode:  {Warn: 80, Crit: 90},
			KindMemory: {Warn: 80, Crit: 90, For: 5 * time.Minute},
		},
	}
	h := &harness{c: &fakeCollector{}, n: &fakeNotifier{}, hb: &fakeHeartbeat{}, now: t0}
	h.m = NewMonitor(cfg, h.c, h.n, h.hb, zerolog.Nop())
	h.m.now = func() time.Time { return h.now }
	return h
}

func (h *harness) tick(at time.Duration, samples []Sample, err error) {
	h.now = t0.Add(at)
	h.c.samples, h.c.err = samples, err
	h.m.Tick(context.Background())
}

func (h *harness) lastSubject(t *testing.T) string {
	t.Helper()
	if len(h.n.sent) == 0 {
		t.Fatal("no mail sent")
	}
	return h.n.sent[len(h.n.sent)-1].Subject
}

func TestMonitorStartMailsReadingsAndThresholds(t *testing.T) {
	h := newHarness(t)
	h.c.samples = []Sample{{Key: "disk:/", Kind: KindDisk, Label: "disk /", Percent: 42, Detail: "58.0 GiB free of 100.0 GiB (/dev/sda1, ext4)"}}
	if err := h.m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	msg := h.n.sent[0]
	if msg.Subject != "[hostwatch] cdn-test: started" {
		t.Errorf("subject = %q", msg.Subject)
	}
	for _, want := range []string{"memory  warning 80%, critical 90%, held for 5m0s", "disk /", "42.0%", "/dev/sda1"} {
		if !strings.Contains(msg.Body, want) {
			t.Errorf("startup body lacks %q:\n%s", want, msg.Body)
		}
	}
}

func TestMonitorStartFailsFast(t *testing.T) {
	h := newHarness(t)
	h.c.err = errors.New("partitions: permission denied")
	if err := h.m.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "initial collection") {
		t.Fatalf("err = %v", err)
	}

	h = newHarness(t)
	h.n.fail = true
	if err := h.m.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "startup mail") {
		t.Fatalf("err = %v", err)
	}
}

func TestMonitorAlertLifecycle(t *testing.T) {
	h := newHarness(t)
	disk := func(p float64) []Sample {
		return []Sample{{Key: "disk:/", Kind: KindDisk, Label: "disk /", Percent: p, Detail: "detail"}}
	}

	h.tick(0, disk(50), nil)
	if len(h.n.sent) != 0 {
		t.Fatal("mailed while healthy")
	}

	h.tick(time.Minute, disk(91.25), nil)
	if got := h.lastSubject(t); got != "[CRITICAL] cdn-test: disk / 91.2%" {
		t.Fatalf("subject = %q", got)
	}
	body := h.n.sent[0].Body
	for _, want := range []string{"Host: cdn-test", "(raised; warning 80%, critical 90%)", "at this level since 2026-09-24 10:01 UTC", "Current readings:"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q:\n%s", want, body)
		}
	}

	h.tick(2*time.Minute, disk(91), nil)
	if len(h.n.sent) != 1 {
		t.Fatal("repeated an alert before the reminder was due")
	}

	h.tick(time.Minute+6*time.Hour, disk(92), nil)
	if got := h.lastSubject(t); got != "[CRITICAL] cdn-test: disk / 92.0% (still active)" {
		t.Fatalf("reminder subject = %q", got)
	}

	h.tick(7*time.Hour, disk(40), nil)
	if got := h.lastSubject(t); got != "[RESOLVED] cdn-test: disk / 40.0%" {
		t.Fatalf("recovery subject = %q", got)
	}
	for _, b := range h.hb.beats {
		if !b.up || b.msg != "OK" {
			t.Fatalf("heartbeat went down on a healthy tick: %+v", b)
		}
	}
}

func TestMonitorGroupsSimultaneousAlerts(t *testing.T) {
	h := newHarness(t)
	h.tick(0, []Sample{
		{Key: "disk:/", Kind: KindDisk, Label: "disk /", Percent: 85},
		{Key: "inode:/", Kind: KindInode, Label: "inode /", Percent: 97},
	}, nil)
	if len(h.n.sent) != 1 {
		t.Fatalf("sent %d mails, want one", len(h.n.sent))
	}
	if got := h.lastSubject(t); got != "[CRITICAL] cdn-test: 2 alerts changed" {
		t.Fatalf("subject = %q", got)
	}
}

func TestMonitorRetriesFailedMailAndMarksHeartbeatDown(t *testing.T) {
	h := newHarness(t)
	h.n.fail = true
	h.tick(0, diskAt(95), nil)
	if b := h.hb.beats[0]; b.up || !strings.Contains(b.msg, "cannot deliver") {
		t.Fatalf("heartbeat = %+v", b)
	}

	h.n.fail = false
	h.tick(time.Minute, diskAt(95), nil)
	if len(h.n.sent) != 1 || !strings.HasPrefix(h.lastSubject(t), "[CRITICAL]") {
		t.Fatalf("alert not retried: %+v", h.n.sent)
	}
	if !h.hb.beats[1].up {
		t.Fatal("heartbeat stayed down after delivery recovered")
	}
}

func TestMonitorCollectorErrors(t *testing.T) {
	h := newHarness(t)
	failure := errors.New("statfs /mnt/nas: stale file handle")

	h.tick(0, nil, failure)
	if got := h.lastSubject(t); got != "[ERROR] cdn-test: hostwatch cannot read every metric" {
		t.Fatalf("subject = %q", got)
	}
	if !strings.Contains(h.n.sent[0].Body, "stale file handle") {
		t.Fatalf("body = %s", h.n.sent[0].Body)
	}
	if b := h.hb.beats[0]; b.up || !strings.Contains(b.msg, "stale file handle") {
		t.Fatalf("heartbeat = %+v", b)
	}

	h.tick(time.Minute, nil, failure)
	if len(h.n.sent) != 1 {
		t.Fatal("collector error re-mailed before the reminder")
	}
	h.tick(6*time.Hour, nil, failure)
	if len(h.n.sent) != 2 {
		t.Fatal("collector error reminder missing")
	}

	h.tick(6*time.Hour+time.Minute, nil, nil)
	if got := h.lastSubject(t); got != "[RESOLVED] cdn-test: hostwatch reads every metric again" {
		t.Fatalf("subject = %q", got)
	}
	h.tick(6*time.Hour+2*time.Minute, nil, nil)
	if len(h.n.sent) != 3 {
		t.Fatal("resolved notice repeated")
	}
}

func TestMonitorWithoutHeartbeatAndFailingHeartbeat(t *testing.T) {
	h := newHarness(t)
	h.m.heartbeat = nil
	h.tick(0, diskAt(10), nil) // must not panic

	h = newHarness(t)
	h.hb.err = errors.New("timeout")
	h.tick(0, diskAt(95), nil)
	if len(h.n.sent) != 1 {
		t.Fatal("a failing heartbeat blocked the alert")
	}
}

func TestMonitorRunStopsOnCancel(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		h.m.Run(ctx, time.Hour)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if len(h.hb.beats) != 1 {
		t.Fatalf("Run must tick once immediately, got %d", len(h.hb.beats))
	}
}
