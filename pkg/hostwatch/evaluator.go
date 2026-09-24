package hostwatch

import (
	"sort"
	"time"
)

type Level int

const (
	LevelOK Level = iota
	LevelWarning
	LevelCritical
)

func (l Level) String() string {
	switch l {
	case LevelWarning:
		return "WARNING"
	case LevelCritical:
		return "CRITICAL"
	default:
		return "OK"
	}
}

type Reason int

const (
	ReasonRaised Reason = iota
	ReasonEscalated
	ReasonImproved
	ReasonRecovered
	ReasonReminder
)

func (r Reason) String() string {
	return [...]string{"raised", "escalated", "improved", "recovered", "still active"}[r]
}

// Event is a notification that is due for one sample.
type Event struct {
	Sample Sample
	Level  Level
	Reason Reason
	Since  time.Time
}

type keyState struct {
	sample       Sample
	level        Level
	since        time.Time
	pendingSince time.Time
	notified     Level
	lastSent     time.Time
}

// Evaluator turns samples into due notifications. It is not safe for
// concurrent use; the monitor loop owns it.
type Evaluator struct {
	rules      map[Kind]Rule
	hysteresis float64
	remind     time.Duration
	states     map[string]*keyState
}

func NewEvaluator(rules map[Kind]Rule, hysteresis float64, remind time.Duration) *Evaluator {
	return &Evaluator{rules: rules, hysteresis: hysteresis, remind: remind, states: map[string]*keyState{}}
}

// Observe records samples and returns the events due now, most severe first.
// Nothing is marked as sent; call MarkSent once delivery succeeded, so that a
// failed mail is retried on the next tick. A key absent from samples keeps its
// state untouched, so a mount that failed statfs once does not re-alert.
func (e *Evaluator) Observe(now time.Time, samples []Sample) []Event {
	var due []Event
	for _, s := range samples {
		rule, ok := e.rules[s.Kind]
		if !ok {
			continue
		}
		st := e.states[s.Key]
		if st == nil {
			st = &keyState{since: now}
			e.states[s.Key] = st
		}
		st.sample = s
		e.advance(st, rule, now)

		if ev, ok := e.dueEvent(st, now); ok {
			due = append(due, ev)
		}
	}
	sort.SliceStable(due, func(i, j int) bool { return due[i].Level > due[j].Level })
	return due
}

func (e *Evaluator) advance(st *keyState, rule Rule, now time.Time) {
	target := e.targetLevel(st.sample.Percent, st.level, rule)
	switch {
	case target > st.level:
		if st.pendingSince.IsZero() {
			st.pendingSince = now
		}
		if now.Sub(st.pendingSince) >= rule.For {
			st.level, st.since, st.pendingSince = target, now, time.Time{}
		}
	case target < st.level:
		st.level, st.since, st.pendingSince = target, now, time.Time{}
	default:
		st.pendingSince = time.Time{}
	}
}

// targetLevel holds the current level until the value drops a full hysteresis
// margin below that level's threshold, so a reading hovering at 90% does not
// alternate between critical and recovered mails.
func (e *Evaluator) targetLevel(p float64, current Level, rule Rule) Level {
	raw := LevelOK
	switch {
	case p >= rule.Crit:
		raw = LevelCritical
	case p >= rule.Warn:
		raw = LevelWarning
	}
	for l := current; l > raw; l-- {
		if p >= threshold(rule, l)-e.hysteresis {
			return l
		}
	}
	return raw
}

func threshold(rule Rule, l Level) float64 {
	if l == LevelCritical {
		return rule.Crit
	}
	return rule.Warn
}

func (e *Evaluator) dueEvent(st *keyState, now time.Time) (Event, bool) {
	ev := Event{Sample: st.sample, Level: st.level, Since: st.since}
	switch {
	case st.level == st.notified && st.level > LevelOK && now.Sub(st.lastSent) >= e.remind:
		ev.Reason = ReasonReminder
	case st.level == st.notified:
		return Event{}, false
	case st.notified == LevelOK:
		ev.Reason = ReasonRaised
	case st.level > st.notified:
		ev.Reason = ReasonEscalated
	case st.level == LevelOK:
		ev.Reason = ReasonRecovered
	default:
		ev.Reason = ReasonImproved
	}
	return ev, true
}

// MarkSent records that events were delivered at now.
func (e *Evaluator) MarkSent(now time.Time, events []Event) {
	for _, ev := range events {
		if st := e.states[ev.Sample.Key]; st != nil {
			st.notified, st.lastSent = ev.Level, now
		}
	}
}
