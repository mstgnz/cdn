// Package hostwatch measures the host's disk, inode and memory usage and mails
// an operator when one of them crosses a threshold. It runs as its own
// container (cmd/hostwatch) so that it keeps reporting when the API does not.
package hostwatch

import (
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Rule holds the thresholds for one kind of measurement, in percent.
type Rule struct {
	Warn float64
	Crit float64
	// For is how long a level must hold before it is raised; zero raises at once.
	For time.Duration
}

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     *mail.Address
	To       []*mail.Address
}

type Config struct {
	Interval       time.Duration
	Hostname       string
	RootFS         string
	Rules          map[Kind]Rule
	Hysteresis     float64
	RemindEvery    time.Duration
	ExcludeFSTypes map[string]bool
	ExcludeMounts  map[string]bool
	SMTP           SMTPConfig
	HeartbeatURL   string
}

// LoadConfig reads HOSTWATCH_* variables through getenv. Unlike pkg/config it
// rejects a malformed value instead of falling back, since a typo in a
// threshold would otherwise silently disable the alert it was meant to set.
func LoadConfig(getenv func(string) string) (Config, error) {
	p := parser{getenv: getenv}

	cfg := Config{
		Interval:       p.duration("HOSTWATCH_INTERVAL", time.Minute),
		Hostname:       strings.TrimSpace(getenv("HOSTWATCH_HOSTNAME")),
		RootFS:         p.str("HOSTWATCH_ROOTFS", "/"),
		Hysteresis:     p.float("HOSTWATCH_HYSTERESIS", 3),
		RemindEvery:    p.duration("HOSTWATCH_REMIND_EVERY", 6*time.Hour),
		ExcludeFSTypes: p.set("HOSTWATCH_EXCLUDE_FSTYPES", "squashfs,iso9660"),
		ExcludeMounts:  p.set("HOSTWATCH_EXCLUDE_MOUNTS", ""),
		HeartbeatURL:   strings.TrimSpace(getenv("HOSTWATCH_HEARTBEAT_URL")),
		Rules: map[Kind]Rule{
			KindDisk:   p.rule("DISK", 0),
			KindInode:  p.rule("INODE", 0),
			KindMemory: p.rule("MEMORY", 5*time.Minute),
		},
		SMTP: SMTPConfig{
			Host:     p.required("HOSTWATCH_SMTP_HOST"),
			Port:     p.integer("HOSTWATCH_SMTP_PORT", 587),
			Username: getenv("HOSTWATCH_SMTP_USERNAME"),
			Password: getenv("HOSTWATCH_SMTP_PASSWORD"),
			From:     p.address("HOSTWATCH_SMTP_FROM"),
			To:       p.addressList("HOSTWATCH_SMTP_TO"),
		},
	}

	if cfg.Interval < 10*time.Second {
		p.fail("HOSTWATCH_INTERVAL must be at least 10s")
	}
	if cfg.RemindEvery < cfg.Interval {
		p.fail("HOSTWATCH_REMIND_EVERY must not be shorter than HOSTWATCH_INTERVAL")
	}
	if cfg.Hysteresis < 0 || cfg.Hysteresis >= 50 {
		p.fail("HOSTWATCH_HYSTERESIS must be between 0 and 50")
	}
	if cfg.SMTP.Port <= 0 || cfg.SMTP.Port > 65535 {
		p.fail("HOSTWATCH_SMTP_PORT is out of range")
	}
	if (cfg.SMTP.Username == "") != (cfg.SMTP.Password == "") {
		p.fail("HOSTWATCH_SMTP_USERNAME and HOSTWATCH_SMTP_PASSWORD must be set together")
	}
	if cfg.HeartbeatURL != "" {
		u, err := url.Parse(cfg.HeartbeatURL)
		// The value is never echoed: a push URL carries its token in the path.
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			p.fail("HOSTWATCH_HEARTBEAT_URL must be an absolute http(s) URL")
		}
	}

	if len(p.errs) > 0 {
		return Config{}, fmt.Errorf("hostwatch config: %w", errors.Join(p.errs...))
	}
	return cfg, nil
}

type parser struct {
	getenv func(string) string
	errs   []error
}

func (p *parser) fail(format string, args ...any) {
	p.errs = append(p.errs, fmt.Errorf(format, args...))
}

func (p *parser) str(key, def string) string {
	if v := strings.TrimSpace(p.getenv(key)); v != "" {
		return v
	}
	return def
}

func (p *parser) required(key string) string {
	v := strings.TrimSpace(p.getenv(key))
	if v == "" {
		p.fail("%s is required", key)
	}
	return v
}

func (p *parser) duration(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(p.getenv(key))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		p.fail("%s: %q is not a duration such as 90s or 5m", key, v)
		return def
	}
	return d
}

func (p *parser) float(key string, def float64) float64 {
	v := strings.TrimSpace(p.getenv(key))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		p.fail("%s: %q is not a number", key, v)
		return def
	}
	return f
}

func (p *parser) integer(key string, def int) int {
	v := strings.TrimSpace(p.getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		p.fail("%s: %q is not an integer", key, v)
		return def
	}
	return n
}

func (p *parser) rule(name string, defFor time.Duration) Rule {
	r := Rule{
		Warn: p.float("HOSTWATCH_"+name+"_WARN", 80),
		Crit: p.float("HOSTWATCH_"+name+"_CRIT", 90),
		For:  p.duration("HOSTWATCH_"+name+"_FOR", defFor),
	}
	if !(r.Warn > 0 && r.Warn < r.Crit && r.Crit <= 100) {
		p.fail("HOSTWATCH_%s_WARN and _CRIT must satisfy 0 < warn < crit <= 100", name)
	}
	return r
}

func (p *parser) set(key, def string) map[string]bool {
	out := map[string]bool{}
	for _, item := range strings.Split(p.str(key, def), ",") {
		if item = strings.TrimSpace(item); item != "" {
			out[item] = true
		}
	}
	return out
}

func (p *parser) address(key string) *mail.Address {
	v := p.required(key)
	if v == "" {
		return nil
	}
	a, err := mail.ParseAddress(v)
	if err != nil {
		p.fail("%s: %q is not a valid address", key, v)
		return nil
	}
	return a
}

func (p *parser) addressList(key string) []*mail.Address {
	v := p.required(key)
	if v == "" {
		return nil
	}
	list, err := mail.ParseAddressList(v)
	if err != nil {
		p.fail("%s: %q is not a comma separated address list", key, v)
		return nil
	}
	return list
}
