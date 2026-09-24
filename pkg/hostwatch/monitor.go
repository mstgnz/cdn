package hostwatch

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// Monitor runs collect, evaluate, notify and heartbeat once per tick.
type Monitor struct {
	collector Collector
	eval      *Evaluator
	notifier  Notifier
	heartbeat Heartbeat // nil disables it
	rules     map[Kind]Rule
	hostname  string
	remind    time.Duration
	log       zerolog.Logger
	now       func() time.Time

	collectErrNotified bool
	collectErrSent     time.Time
}

func NewMonitor(cfg Config, c Collector, n Notifier, hb Heartbeat, log zerolog.Logger) *Monitor {
	return &Monitor{
		collector: c,
		eval:      NewEvaluator(cfg.Rules, cfg.Hysteresis, cfg.RemindEvery),
		notifier:  n,
		heartbeat: hb,
		rules:     cfg.Rules,
		hostname:  cfg.Hostname,
		remind:    cfg.RemindEvery,
		log:       log,
		now:       time.Now,
	}
}

// Start takes one full reading and mails it. Any failure is returned so the
// process exits and a broken setup shows up as a restart loop, not silence.
func (m *Monitor) Start(ctx context.Context) error {
	samples, err := m.collector.Collect(ctx)
	if err != nil {
		return fmt.Errorf("initial collection: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "hostwatch started on %s at %s.\n", m.hostname, m.now().UTC().Format(timeLayout))
	b.WriteString("Mail delivery works: this message is the proof.\n\n")
	b.WriteString("Thresholds:\n")
	for _, k := range []Kind{KindDisk, KindInode, KindMemory} {
		r := m.rules[k]
		fmt.Fprintf(&b, "  %-7s warning %g%%, critical %g%%", k, r.Warn, r.Crit)
		if r.For > 0 {
			fmt.Fprintf(&b, ", held for %s", r.For)
		}
		b.WriteString("\n")
	}
	writeReadings(&b, samples)

	msg := Message{Subject: fmt.Sprintf("[hostwatch] %s: started", m.hostname), Body: b.String()}
	if err := m.notifier.Send(ctx, msg); err != nil {
		return fmt.Errorf("startup mail: %w", err)
	}
	m.log.Info().Int("samples", len(samples)).Msg("hostwatch started, startup mail sent")
	return nil
}

// Run ticks immediately and then every interval until ctx is done.
func (m *Monitor) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		m.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (m *Monitor) Tick(ctx context.Context) {
	now := m.now()
	samples, cerr := m.collector.Collect(ctx)
	if cerr != nil {
		m.log.Error().Err(cerr).Int("samples", len(samples)).Msg("collection incomplete")
	}
	events := m.eval.Observe(now, samples)

	errDue := cerr != nil && (!m.collectErrNotified || now.Sub(m.collectErrSent) >= m.remind)
	errCleared := cerr == nil && m.collectErrNotified

	mailOK := true
	if len(events) > 0 || errDue || errCleared {
		msg := m.report(now, events, samples, cerr, errDue, errCleared)
		if err := m.notifier.Send(ctx, msg); err != nil {
			mailOK = false
			m.log.Error().Err(err).Str("subject", msg.Subject).Msg("alert mail failed, retrying next tick")
		} else {
			m.eval.MarkSent(now, events)
			if errDue {
				m.collectErrNotified, m.collectErrSent = true, now
			}
			if errCleared {
				m.collectErrNotified = false
			}
			m.log.Info().Str("subject", msg.Subject).Msg("alert mail sent")
		}
	}

	if m.heartbeat == nil {
		return
	}
	up, status := true, "OK"
	switch {
	case cerr != nil:
		up, status = false, "hostwatch cannot read every metric: "+cerr.Error()
	case !mailOK:
		up, status = false, "hostwatch cannot deliver alert mail"
	}
	if err := m.heartbeat.Beat(ctx, up, status); err != nil {
		m.log.Warn().Err(err).Msg("heartbeat failed")
	}
}

const timeLayout = "2006-01-02 15:04 UTC"

func (m *Monitor) report(now time.Time, events []Event, samples []Sample, cerr error, errDue, errCleared bool) Message {
	var b strings.Builder
	fmt.Fprintf(&b, "Host: %s\nTime: %s\n\n", m.hostname, now.UTC().Format(timeLayout))

	for _, ev := range events {
		r := m.rules[ev.Sample.Kind]
		fmt.Fprintf(&b, "%-9s %s  %.1f%%  (%s; warning %g%%, critical %g%%)\n",
			tag(ev), ev.Sample.Label, ev.Sample.Percent, ev.Reason, r.Warn, r.Crit)
		fmt.Fprintf(&b, "          %s\n", ev.Sample.Detail)
		if ev.Level > LevelOK {
			fmt.Fprintf(&b, "          at this level since %s\n", ev.Since.UTC().Format(timeLayout))
		}
		b.WriteString("\n")
	}
	if errDue {
		fmt.Fprintf(&b, "hostwatch could not read every metric, so some alerts may be missing:\n  %s\n\n", cerr)
	}
	if errCleared {
		b.WriteString("hostwatch reads every metric again.\n\n")
	}
	writeReadings(&b, samples)

	return Message{Subject: m.subject(events, errDue), Body: b.String()}
}

func (m *Monitor) subject(events []Event, errDue bool) string {
	switch {
	case len(events) == 1:
		ev := events[0]
		s := fmt.Sprintf("[%s] %s: %s %.1f%%", tag(ev), m.hostname, ev.Sample.Label, ev.Sample.Percent)
		if ev.Reason == ReasonReminder {
			s += " (still active)"
		}
		return s
	case len(events) > 1:
		// events are sorted most severe first.
		return fmt.Sprintf("[%s] %s: %d alerts changed", tag(events[0]), m.hostname, len(events))
	case errDue:
		return fmt.Sprintf("[ERROR] %s: hostwatch cannot read every metric", m.hostname)
	default:
		return fmt.Sprintf("[RESOLVED] %s: hostwatch reads every metric again", m.hostname)
	}
}

func tag(ev Event) string {
	if ev.Level == LevelOK {
		return "RESOLVED"
	}
	return ev.Level.String()
}

func writeReadings(b *strings.Builder, samples []Sample) {
	sorted := append([]Sample(nil), samples...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })
	b.WriteString("\nCurrent readings:\n")
	if len(sorted) == 0 {
		b.WriteString("  none\n")
	}
	for _, s := range sorted {
		fmt.Fprintf(b, "  %-24s %5.1f%%  %s\n", s.Label, s.Percent, s.Detail)
	}
}
