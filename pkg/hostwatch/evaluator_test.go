package hostwatch

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)

func diskAt(p float64) []Sample {
	return []Sample{{Key: "disk:/", Kind: KindDisk, Label: "disk /", Percent: p}}
}

func newTestEvaluator(memFor time.Duration) *Evaluator {
	return NewEvaluator(map[Kind]Rule{
		KindDisk:   {Warn: 80, Crit: 90},
		KindMemory: {Warn: 80, Crit: 90, For: memFor},
	}, 3, 6*time.Hour)
}

type step struct {
	at     time.Duration
	pct    float64
	want   Level // the event's level, ignored when none is due
	reason Reason
	due    bool
	sent   bool // whether delivery succeeds; only meaningful when due
}

func run(t *testing.T, e *Evaluator, kind Kind, steps []step) {
	t.Helper()
	for i, s := range steps {
		now := t0.Add(s.at)
		samples := []Sample{{Key: string(kind), Kind: kind, Label: string(kind), Percent: s.pct}}
		events := e.Observe(now, samples)
		if !s.due {
			if len(events) != 0 {
				t.Fatalf("step %d (%.0f%% at %v): unexpected %s %s", i, s.pct, s.at, events[0].Level, events[0].Reason)
			}
			continue
		}
		if len(events) != 1 {
			t.Fatalf("step %d (%.0f%% at %v): got %d events, want 1", i, s.pct, s.at, len(events))
		}
		if ev := events[0]; ev.Level != s.want || ev.Reason != s.reason {
			t.Fatalf("step %d (%.0f%% at %v): got %s %s, want %s %s", i, s.pct, s.at, ev.Level, ev.Reason, s.want, s.reason)
		}
		if s.sent {
			e.MarkSent(now, events)
		}
	}
}

func TestEvaluatorLifecycle(t *testing.T) {
	run(t, newTestEvaluator(0), KindDisk, []step{
		{at: 0, pct: 50},
		{at: time.Minute, pct: 81, due: true, sent: true, want: LevelWarning, reason: ReasonRaised},
		{at: 2 * time.Minute, pct: 85},
		{at: 3 * time.Minute, pct: 91, due: true, sent: true, want: LevelCritical, reason: ReasonEscalated},
		{at: 4 * time.Minute, pct: 88}, // inside the hysteresis band below 90
		{at: 5 * time.Minute, pct: 86, due: true, sent: true, want: LevelWarning, reason: ReasonImproved},
		{at: 6 * time.Minute, pct: 78}, // inside the band below 80
		{at: 7 * time.Minute, pct: 70, due: true, sent: true, want: LevelOK, reason: ReasonRecovered},
		{at: 8 * time.Minute, pct: 70},
	})
}

func TestEvaluatorHysteresisStopsFlapping(t *testing.T) {
	steps := []step{{at: 0, pct: 90, due: true, sent: true, want: LevelCritical, reason: ReasonRaised}}
	for i := 1; i <= 20; i++ {
		pct := 90.0
		if i%2 == 1 {
			pct = 89.2
		}
		steps = append(steps, step{at: time.Duration(i) * time.Minute, pct: pct})
	}
	run(t, newTestEvaluator(0), KindDisk, steps)
}

func TestEvaluatorStraightToCriticalAndBack(t *testing.T) {
	run(t, newTestEvaluator(0), KindDisk, []step{
		{at: 0, pct: 97, due: true, sent: true, want: LevelCritical, reason: ReasonRaised},
		{at: time.Minute, pct: 10, due: true, sent: true, want: LevelOK, reason: ReasonRecovered},
	})
}

func TestEvaluatorReminder(t *testing.T) {
	run(t, newTestEvaluator(0), KindDisk, []step{
		{at: 0, pct: 92, due: true, sent: true, want: LevelCritical, reason: ReasonRaised},
		{at: 5*time.Hour + 59*time.Minute, pct: 93},
		{at: 6 * time.Hour, pct: 93, due: true, sent: true, want: LevelCritical, reason: ReasonReminder},
		{at: 11 * time.Hour, pct: 93},
		{at: 12 * time.Hour, pct: 93, due: true, sent: true, want: LevelCritical, reason: ReasonReminder},
	})
}

func TestEvaluatorRetriesUntilDelivered(t *testing.T) {
	run(t, newTestEvaluator(0), KindDisk, []step{
		{at: 0, pct: 95, due: true, sent: false, want: LevelCritical, reason: ReasonRaised},
		{at: time.Minute, pct: 95, due: true, sent: false, want: LevelCritical, reason: ReasonRaised},
		{at: 2 * time.Minute, pct: 95, due: true, sent: true, want: LevelCritical, reason: ReasonRaised},
		{at: 3 * time.Minute, pct: 95},
	})
}

func TestEvaluatorUndeliveredAlertThatClearsSendsNothing(t *testing.T) {
	run(t, newTestEvaluator(0), KindDisk, []step{
		{at: 0, pct: 95, due: true, sent: false, want: LevelCritical, reason: ReasonRaised},
		{at: time.Minute, pct: 40},
	})
}

func TestEvaluatorForDelaysRaise(t *testing.T) {
	run(t, newTestEvaluator(5*time.Minute), KindMemory, []step{
		{at: 0, pct: 95},
		{at: 2 * time.Minute, pct: 95},
		{at: 3 * time.Minute, pct: 60}, // a burst, not a trend: the timer resets
		{at: 4 * time.Minute, pct: 95},
		{at: 8 * time.Minute, pct: 95},
		{at: 9 * time.Minute, pct: 84, due: true, sent: true, want: LevelWarning, reason: ReasonRaised},
		// Dropping a level is immediate, For only delays raising.
		{at: 10 * time.Minute, pct: 50, due: true, sent: true, want: LevelOK, reason: ReasonRecovered},
	})
}

func TestEvaluatorOrdersMostSevereFirstAndIgnoresUnknownKinds(t *testing.T) {
	e := newTestEvaluator(0)
	events := e.Observe(t0, []Sample{
		{Key: "disk:/a", Kind: KindDisk, Percent: 82},
		{Key: "disk:/b", Kind: KindDisk, Percent: 95},
		{Key: "inode:/", Kind: KindInode, Percent: 99}, // no rule configured in this evaluator
	})
	if len(events) != 2 || events[0].Sample.Key != "disk:/b" || events[1].Sample.Key != "disk:/a" {
		t.Fatalf("events = %+v", events)
	}
}

func TestEvaluatorVanishedKeyKeepsState(t *testing.T) {
	e := newTestEvaluator(0)
	e.MarkSent(t0, e.Observe(t0, diskAt(95)))
	if ev := e.Observe(t0.Add(time.Minute), nil); len(ev) != 0 {
		t.Fatalf("a missing sample produced %+v", ev)
	}
	if ev := e.Observe(t0.Add(2*time.Minute), diskAt(95)); len(ev) != 0 {
		t.Fatalf("a returning sample re-alerted: %+v", ev)
	}
}

func TestEvaluatorSinceTracksLevelChange(t *testing.T) {
	e := newTestEvaluator(0)
	e.MarkSent(t0, e.Observe(t0, diskAt(85)))
	events := e.Observe(t0.Add(time.Hour), diskAt(95))
	if len(events) != 1 || !events[0].Since.Equal(t0.Add(time.Hour)) {
		t.Fatalf("events = %+v", events)
	}
}

func TestLevelAndReasonStrings(t *testing.T) {
	if LevelOK.String() != "OK" || LevelWarning.String() != "WARNING" || LevelCritical.String() != "CRITICAL" {
		t.Error("level strings")
	}
	if ReasonReminder.String() != "still active" || ReasonRecovered.String() != "recovered" {
		t.Error("reason strings")
	}
}
