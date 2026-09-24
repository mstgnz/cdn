//go:build linux

package hostwatch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Exercises the real gopsutil wiring the container relies on: HOST_PROC and
// HOST_PROC_MOUNTINFO pointing at a foreign /proc, statfs under RootFS.
func TestHostCollectorReadsForeignProc(t *testing.T) {
	proc := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(proc, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("1/mountinfo", ""+
		"29 1 8:1 / / rw,relatime shared:1 - ext4 /dev/sda1 rw\n"+
		"30 29 7:0 / /snap/core/1 ro,nodev shared:2 - squashfs /dev/loop0 ro\n"+
		"31 29 0:25 / /run rw,nosuid shared:3 - tmpfs tmpfs rw\n")
	write("filesystems", "\text4\nnodev\ttmpfs\n\tsquashfs\n")
	write("meminfo", ""+
		"MemTotal:       12000000 kB\n"+
		"MemFree:         1000000 kB\n"+
		"MemAvailable:    3000000 kB\n"+
		"Buffers:          100000 kB\n"+
		"Cached:          1500000 kB\n")
	t.Setenv("HOST_PROC", proc)
	t.Setenv("HOST_PROC_MOUNTINFO", filepath.Join(proc, "1", "mountinfo"))

	cfg := Config{RootFS: t.TempDir(), ExcludeFSTypes: map[string]bool{"squashfs": true}}
	samples, err := NewHostCollector(cfg).Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	keys := map[string]float64{}
	for _, s := range samples {
		keys[s.Key] = s.Percent
	}
	if keys["memory"] != 75 {
		t.Errorf("memory = %v, want 75 from MemAvailable", keys["memory"])
	}
	if _, ok := keys["disk:/"]; !ok {
		t.Errorf("root filesystem missing: %v", keys)
	}
	if _, ok := keys["disk:/snap/core/1"]; ok {
		t.Error("excluded squashfs was reported")
	}
	if _, ok := keys["disk:/run"]; ok {
		t.Error("tmpfs was reported")
	}
}

func TestHostCollectorFailsLoudlyWithoutMountinfo(t *testing.T) {
	proc := t.TempDir()
	t.Setenv("HOST_PROC", proc)
	t.Setenv("HOST_PROC_MOUNTINFO", filepath.Join(proc, "1", "mountinfo"))
	if _, err := NewHostCollector(Config{RootFS: "/"}).Collect(context.Background()); err == nil {
		t.Fatal("an unreadable mountinfo must be an error, not a silent fallback to /proc/self")
	}
}
