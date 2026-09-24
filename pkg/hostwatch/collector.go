package hostwatch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

type Kind string

const (
	KindDisk   Kind = "disk"
	KindInode  Kind = "inode"
	KindMemory Kind = "memory"
)

// Sample is one measurement. Key identifies it across ticks.
type Sample struct {
	Key     string
	Kind    Kind
	Label   string
	Percent float64
	Detail  string
}

type Collector interface {
	// Collect returns every sample it could take. A non-nil error with samples
	// means some measurements failed and the rest are still valid.
	Collect(ctx context.Context) ([]Sample, error)
}

// HostCollector reads the host through gopsutil. In a container, point
// HOST_PROC and HOST_PROC_MOUNTINFO at the host's /proc and RootFS at the
// host's / so that statfs lands on the host's filesystems.
type HostCollector struct {
	rootFS         string
	excludeFSTypes map[string]bool
	excludeMounts  map[string]bool

	partitions func(ctx context.Context) ([]disk.PartitionStat, error)
	usage      func(ctx context.Context, path string) (*disk.UsageStat, error)
	memory     func(ctx context.Context) (*mem.VirtualMemoryStat, error)
}

func NewHostCollector(cfg Config) *HostCollector {
	return &HostCollector{
		rootFS:         cfg.RootFS,
		excludeFSTypes: cfg.ExcludeFSTypes,
		excludeMounts:  cfg.ExcludeMounts,
		partitions: func(ctx context.Context) ([]disk.PartitionStat, error) {
			return disk.PartitionsWithContext(ctx, false)
		},
		usage:  disk.UsageWithContext,
		memory: mem.VirtualMemoryWithContext,
	}
}

func (c *HostCollector) Collect(ctx context.Context) ([]Sample, error) {
	var samples []Sample
	var errs []error

	if s, err := c.collectMemory(ctx); err != nil {
		errs = append(errs, err)
	} else {
		samples = append(samples, s)
	}

	disks, err := c.collectDisks(ctx)
	samples = append(samples, disks...)
	if err != nil {
		errs = append(errs, err)
	}

	return samples, errors.Join(errs...)
}

func (c *HostCollector) collectMemory(ctx context.Context) (Sample, error) {
	vm, err := c.memory(ctx)
	if err != nil {
		return Sample{}, fmt.Errorf("memory: %w", err)
	}
	if vm.Total == 0 || vm.Available > vm.Total {
		return Sample{}, fmt.Errorf("memory: implausible reading, total %d available %d", vm.Total, vm.Available)
	}
	// gopsutil's UsedPercent subtracts only buffers and cache; MemAvailable also
	// counts reclaimable slab and is what the kernel considers usable.
	used := vm.Total - vm.Available
	return Sample{
		Key:     string(KindMemory),
		Kind:    KindMemory,
		Label:   "memory",
		Percent: percent(used, vm.Total),
		Detail:  fmt.Sprintf("%s available of %s", formatBytes(vm.Available), formatBytes(vm.Total)),
	}, nil
}

func (c *HostCollector) collectDisks(ctx context.Context) ([]Sample, error) {
	parts, err := c.partitions(ctx)
	if err != nil {
		return nil, fmt.Errorf("partitions: %w", err)
	}
	// "/" first, then by path, so a device mounted twice is reported under its
	// shortest mountpoint.
	sort.SliceStable(parts, func(i, j int) bool { return parts[i].Mountpoint < parts[j].Mountpoint })

	var samples []Sample
	var errs []error
	seen := map[string]bool{}
	for _, p := range parts {
		if c.excludeFSTypes[p.Fstype] || c.excludeMounts[p.Mountpoint] || seen[p.Device] {
			continue
		}
		seen[p.Device] = true

		u, err := c.usage(ctx, filepath.Join(c.rootFS, p.Mountpoint))
		if err != nil {
			errs = append(errs, fmt.Errorf("statfs %s: %w", p.Mountpoint, err))
			continue
		}
		if u.Total == 0 {
			continue
		}
		samples = append(samples, Sample{
			Key:     "disk:" + p.Mountpoint,
			Kind:    KindDisk,
			Label:   "disk " + p.Mountpoint,
			Percent: u.UsedPercent,
			Detail:  fmt.Sprintf("%s free of %s (%s, %s)", formatBytes(u.Free), formatBytes(u.Total), p.Device, p.Fstype),
		})
		// btrfs and some network filesystems report no inode table at all.
		if u.InodesTotal > 0 {
			samples = append(samples, Sample{
				Key:     "inode:" + p.Mountpoint,
				Kind:    KindInode,
				Label:   "inode " + p.Mountpoint,
				Percent: u.InodesUsedPercent,
				Detail:  fmt.Sprintf("%d free of %d inodes", u.InodesFree, u.InodesTotal),
			})
		}
	}
	return samples, errors.Join(errs...)
}

// ReadHostname returns the host's name from rootFS/etc/hostname. The kernel
// hostname under /proc is per UTS namespace and would name the container.
func ReadHostname(rootFS string) string {
	if b, err := os.ReadFile(filepath.Join(rootFS, "etc", "hostname")); err == nil {
		if name := strings.TrimSpace(string(b)); name != "" {
			return name
		}
	}
	if name, err := os.Hostname(); err == nil {
		return name
	}
	return "unknown-host"
}

func percent(part, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

func formatBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit && exp < 5; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
