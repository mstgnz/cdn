package hostwatch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

type fakeHost struct {
	parts    []disk.PartitionStat
	partsErr error
	usage    map[string]*disk.UsageStat
	memory   *mem.VirtualMemoryStat
	memErr   error
	statted  []string
}

func (f *fakeHost) collector(rootFS string, excludeFS, excludeMounts map[string]bool) *HostCollector {
	return &HostCollector{
		rootFS:         rootFS,
		excludeFSTypes: excludeFS,
		excludeMounts:  excludeMounts,
		partitions: func(context.Context) ([]disk.PartitionStat, error) {
			return f.parts, f.partsErr
		},
		usage: func(_ context.Context, path string) (*disk.UsageStat, error) {
			f.statted = append(f.statted, path)
			if u, ok := f.usage[path]; ok {
				return u, nil
			}
			return nil, errors.New("no such file or directory")
		},
		memory: func(context.Context) (*mem.VirtualMemoryStat, error) { return f.memory, f.memErr },
	}
}

const gib = 1 << 30

func TestHostCollectorCollects(t *testing.T) {
	f := &fakeHost{
		parts: []disk.PartitionStat{
			{Device: "/dev/sdb1", Mountpoint: "/var/lib/docker", Fstype: "xfs"},
			{Device: "/dev/sda1", Mountpoint: "/", Fstype: "ext4"},
			{Device: "/dev/sda1", Mountpoint: "/var/snap/bind", Fstype: "ext4"}, // same device again
			{Device: "/dev/loop3", Mountpoint: "/snap/core/1", Fstype: "squashfs"},
			{Device: "/dev/sda15", Mountpoint: "/boot/efi", Fstype: "vfat"},
			{Device: "/dev/sdc1", Mountpoint: "/mnt/btrfs", Fstype: "btrfs"},
		},
		usage: map[string]*disk.UsageStat{
			"/host/root":                {Total: 100 * gib, Free: 8 * gib, UsedPercent: 92, InodesTotal: 1000, InodesFree: 900, InodesUsedPercent: 10},
			"/host/root/var/lib/docker": {Total: 50 * gib, Free: 40 * gib, UsedPercent: 20, InodesTotal: 100, InodesFree: 5, InodesUsedPercent: 95},
			"/host/root/mnt/btrfs":      {Total: 10 * gib, Free: 5 * gib, UsedPercent: 50},
		},
		memory: &mem.VirtualMemoryStat{Total: 12 * gib, Available: 3 * gib, Free: gib, UsedPercent: 30},
	}
	c := f.collector("/host/root", map[string]bool{"squashfs": true}, map[string]bool{"/boot/efi": true})

	samples, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	got := map[string]float64{}
	for _, s := range samples {
		got[s.Key] = s.Percent
	}
	want := map[string]float64{
		"memory":                75, // (12-3)/12, not gopsutil's 30
		"disk:/":                92,
		"inode:/":               10,
		"disk:/var/lib/docker":  20,
		"inode:/var/lib/docker": 95,
		"disk:/mnt/btrfs":       50,
	}
	if len(got) != len(want) {
		t.Fatalf("samples = %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	for _, p := range f.statted {
		if strings.Contains(p, "snap") || strings.Contains(p, "efi") {
			t.Errorf("excluded or duplicate mount was statted: %s", p)
		}
	}
	if samples[0].Detail != "3.0 GiB available of 12.0 GiB" {
		t.Errorf("memory detail = %q", samples[0].Detail)
	}
}

func TestHostCollectorKeepsGoodSamplesWhenOneMountFails(t *testing.T) {
	f := &fakeHost{
		parts: []disk.PartitionStat{
			{Device: "/dev/sda1", Mountpoint: "/", Fstype: "ext4"},
			{Device: "nas:/share", Mountpoint: "/mnt/nas", Fstype: "nfs4"},
		},
		usage:  map[string]*disk.UsageStat{"/": {Total: gib, UsedPercent: 40, InodesTotal: 10, InodesUsedPercent: 1}},
		memory: &mem.VirtualMemoryStat{Total: gib, Available: gib / 2},
	}
	samples, err := f.collector("/", nil, nil).Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "statfs /mnt/nas") {
		t.Fatalf("err = %v", err)
	}
	if len(samples) != 3 {
		t.Fatalf("samples = %+v", samples)
	}
}

func TestHostCollectorFailures(t *testing.T) {
	cases := []struct {
		name string
		host fakeHost
		want string
	}{
		{"partitions", fakeHost{partsErr: errors.New("permission denied"), memory: &mem.VirtualMemoryStat{Total: 1}}, "partitions: permission denied"},
		{"memory error", fakeHost{memErr: errors.New("boom")}, "memory: boom"},
		{"memory zero", fakeHost{memory: &mem.VirtualMemoryStat{}}, "implausible"},
		{"memory available above total", fakeHost{memory: &mem.VirtualMemoryStat{Total: 1, Available: 2}}, "implausible"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := c.host.collector("/", nil, nil).Collect(context.Background())
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

func TestHostCollectorSkipsEmptyFilesystem(t *testing.T) {
	f := &fakeHost{
		parts:  []disk.PartitionStat{{Device: "/dev/sda1", Mountpoint: "/", Fstype: "ext4"}},
		usage:  map[string]*disk.UsageStat{"/": {}},
		memory: &mem.VirtualMemoryStat{Total: 1},
	}
	samples, err := f.collector("/", nil, nil).Collect(context.Background())
	if err != nil || len(samples) != 1 {
		t.Fatalf("samples %+v err %v", samples, err)
	}
}

func TestReadHostname(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc", "hostname"), []byte("cdn-test-host\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ReadHostname(root); got != "cdn-test-host" {
		t.Fatalf("ReadHostname = %q", got)
	}
	if got := ReadHostname(t.TempDir()); got == "" {
		t.Fatal("fallback returned an empty name")
	}
}

func TestFormatBytes(t *testing.T) {
	for in, want := range map[uint64]string{
		0:        "0 B",
		1023:     "1023 B",
		1536:     "1.5 KiB",
		10 * gib: "10.0 GiB",
		11 << 40: "11.0 TiB",
	} {
		if got := formatBytes(in); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
