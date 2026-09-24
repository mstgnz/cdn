package hostwatch

import (
	"strings"
	"testing"
	"time"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func minimalEnv() map[string]string {
	return map[string]string{
		"HOSTWATCH_SMTP_HOST":     "smtp.example.com",
		"HOSTWATCH_SMTP_USERNAME": "alerts@example.com",
		"HOSTWATCH_SMTP_PASSWORD": "not-a-real-password",
		"HOSTWATCH_SMTP_FROM":     "CDN Alerts <alerts@example.com>",
		"HOSTWATCH_SMTP_TO":       "ops@example.com, oncall@example.com",
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := LoadConfig(envOf(minimalEnv()))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Interval != time.Minute || cfg.RemindEvery != 6*time.Hour || cfg.Hysteresis != 3 {
		t.Errorf("timing defaults = %v %v %v", cfg.Interval, cfg.RemindEvery, cfg.Hysteresis)
	}
	for _, k := range []Kind{KindDisk, KindInode, KindMemory} {
		if r := cfg.Rules[k]; r.Warn != 80 || r.Crit != 90 {
			t.Errorf("%s thresholds = %v/%v, want 80/90", k, r.Warn, r.Crit)
		}
	}
	if cfg.Rules[KindMemory].For != 5*time.Minute || cfg.Rules[KindDisk].For != 0 {
		t.Errorf("For defaults: memory %v disk %v", cfg.Rules[KindMemory].For, cfg.Rules[KindDisk].For)
	}
	if !cfg.ExcludeFSTypes["squashfs"] || !cfg.ExcludeFSTypes["iso9660"] || len(cfg.ExcludeMounts) != 0 {
		t.Errorf("exclusions = %v %v", cfg.ExcludeFSTypes, cfg.ExcludeMounts)
	}
	if cfg.SMTP.Port != 587 || cfg.RootFS != "/" {
		t.Errorf("port %d rootfs %q", cfg.SMTP.Port, cfg.RootFS)
	}
	if cfg.SMTP.From.Address != "alerts@example.com" || len(cfg.SMTP.To) != 2 || cfg.SMTP.To[1].Address != "oncall@example.com" {
		t.Errorf("addresses = %v %v", cfg.SMTP.From, cfg.SMTP.To)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	env := minimalEnv()
	env["HOSTWATCH_INTERVAL"] = "30s"
	env["HOSTWATCH_DISK_WARN"] = "70"
	env["HOSTWATCH_DISK_CRIT"] = "85.5"
	env["HOSTWATCH_MEMORY_FOR"] = "0s"
	env["HOSTWATCH_EXCLUDE_MOUNTS"] = " /boot/efi , /mnt/scratch "
	env["HOSTWATCH_SMTP_PORT"] = "465"
	env["HOSTWATCH_HOSTNAME"] = "cdn-test"
	env["HOSTWATCH_HEARTBEAT_URL"] = "https://kuma.example.com/api/push/abc?status=up&msg=OK&ping="

	cfg, err := LoadConfig(envOf(env))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Interval != 30*time.Second || cfg.Rules[KindDisk] != (Rule{Warn: 70, Crit: 85.5}) || cfg.Rules[KindMemory].For != 0 {
		t.Errorf("overrides not applied: %+v", cfg)
	}
	if !cfg.ExcludeMounts["/boot/efi"] || !cfg.ExcludeMounts["/mnt/scratch"] {
		t.Errorf("exclude mounts = %v", cfg.ExcludeMounts)
	}
	if cfg.SMTP.Port != 465 || cfg.Hostname != "cdn-test" || cfg.HeartbeatURL == "" {
		t.Errorf("port %d host %q heartbeat %q", cfg.SMTP.Port, cfg.Hostname, cfg.HeartbeatURL)
	}
}

func TestLoadConfigRejects(t *testing.T) {
	cases := []struct {
		name string
		set  map[string]string
		want string
	}{
		{"missing host", map[string]string{"HOSTWATCH_SMTP_HOST": ""}, "HOSTWATCH_SMTP_HOST is required"},
		{"missing recipients", map[string]string{"HOSTWATCH_SMTP_TO": ""}, "HOSTWATCH_SMTP_TO is required"},
		{"bad from", map[string]string{"HOSTWATCH_SMTP_FROM": "not an address"}, "HOSTWATCH_SMTP_FROM"},
		{"bad recipient list", map[string]string{"HOSTWATCH_SMTP_TO": "ops@example.com; x"}, "HOSTWATCH_SMTP_TO"},
		{"threshold typo", map[string]string{"HOSTWATCH_DISK_WARN": "8O"}, "HOSTWATCH_DISK_WARN"},
		{"warn above crit", map[string]string{"HOSTWATCH_INODE_WARN": "95"}, "HOSTWATCH_INODE_WARN and _CRIT"},
		{"crit above 100", map[string]string{"HOSTWATCH_MEMORY_CRIT": "101"}, "HOSTWATCH_MEMORY_WARN and _CRIT"},
		{"zero warn", map[string]string{"HOSTWATCH_DISK_WARN": "0"}, "HOSTWATCH_DISK_WARN and _CRIT"},
		{"bad duration", map[string]string{"HOSTWATCH_INTERVAL": "5 minutes"}, "HOSTWATCH_INTERVAL"},
		{"negative duration", map[string]string{"HOSTWATCH_MEMORY_FOR": "-1m"}, "HOSTWATCH_MEMORY_FOR"},
		{"interval too short", map[string]string{"HOSTWATCH_INTERVAL": "5s"}, "at least 10s"},
		{"remind shorter than interval", map[string]string{"HOSTWATCH_INTERVAL": "10m", "HOSTWATCH_REMIND_EVERY": "5m"}, "HOSTWATCH_REMIND_EVERY"},
		{"hysteresis negative", map[string]string{"HOSTWATCH_HYSTERESIS": "-1"}, "HOSTWATCH_HYSTERESIS"},
		{"hysteresis not a number", map[string]string{"HOSTWATCH_HYSTERESIS": "x"}, "HOSTWATCH_HYSTERESIS"},
		{"port not a number", map[string]string{"HOSTWATCH_SMTP_PORT": "smtp"}, "HOSTWATCH_SMTP_PORT"},
		{"port out of range", map[string]string{"HOSTWATCH_SMTP_PORT": "70000"}, "out of range"},
		{"username without password", map[string]string{"HOSTWATCH_SMTP_PASSWORD": ""}, "set together"},
		{"heartbeat not http", map[string]string{"HOSTWATCH_HEARTBEAT_URL": "ftp://kuma.example.com/x"}, "HOSTWATCH_HEARTBEAT_URL"},
		{"heartbeat relative", map[string]string{"HOSTWATCH_HEARTBEAT_URL": "/api/push/x"}, "HOSTWATCH_HEARTBEAT_URL"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := minimalEnv()
			for k, v := range c.set {
				env[k] = v
			}
			_, err := LoadConfig(envOf(env))
			if err == nil {
				t.Fatal("accepted, want rejection")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not mention %q", err, c.want)
			}
		})
	}
}

func TestLoadConfigNeverEchoesHeartbeatURL(t *testing.T) {
	env := minimalEnv()
	env["HOSTWATCH_HEARTBEAT_URL"] = "kuma.example.com/api/push/secret-push-token"
	_, err := LoadConfig(envOf(env))
	if err == nil || strings.Contains(err.Error(), "secret-push-token") {
		t.Fatalf("error must reject without echoing the URL: %v", err)
	}
}

func TestLoadConfigReportsEveryProblem(t *testing.T) {
	_, err := LoadConfig(envOf(map[string]string{}))
	if err == nil {
		t.Fatal("empty environment accepted")
	}
	for _, key := range []string{"HOSTWATCH_SMTP_HOST", "HOSTWATCH_SMTP_FROM", "HOSTWATCH_SMTP_TO"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error does not mention %s: %v", key, err)
		}
	}
}
