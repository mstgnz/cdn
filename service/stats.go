package service

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
)

type StatsService struct {
	activeUploads int32
	uploadBytes   int64
	cacheHits     int32
	cacheMisses   int32
	errors        []string
	errorsMux     sync.RWMutex
	startTime     time.Time
}

func NewStatsService() *StatsService {
	return &StatsService{
		startTime: time.Now(),
		errors:    make([]string, 0),
	}
}

func (s *StatsService) IncrementActiveUploads() {
	atomic.AddInt32(&s.activeUploads, 1)
}

func (s *StatsService) DecrementActiveUploads() {
	atomic.AddInt32(&s.activeUploads, -1)
}

func (s *StatsService) AddUploadBytes(bytes int64) {
	atomic.AddInt64(&s.uploadBytes, bytes)
}

func (s *StatsService) IncrementCacheHit() {
	atomic.AddInt32(&s.cacheHits, 1)
}

func (s *StatsService) IncrementCacheMiss() {
	atomic.AddInt32(&s.cacheMisses, 1)
}

func (s *StatsService) LogError(err string) {
	s.errorsMux.Lock()
	defer s.errorsMux.Unlock()

	s.errors = append(s.errors, err)
	if len(s.errors) > 100 {
		s.errors = s.errors[1:]
	}
}

func (s *StatsService) GetActiveUploads() int {
	return int(atomic.LoadInt32(&s.activeUploads))
}

func (s *StatsService) GetUploadSpeed() float64 {
	totalBytes := atomic.LoadInt64(&s.uploadBytes)
	duration := time.Since(s.startTime).Seconds()
	if duration == 0 {
		return 0
	}
	return float64(totalBytes) / duration
}

func (s *StatsService) GetCacheHitRate() float64 {
	hits := atomic.LoadInt32(&s.cacheHits)
	misses := atomic.LoadInt32(&s.cacheMisses)
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total) * 100
}

func (s *StatsService) GetCPUUsage() float64 {
	percent, err := cpu.Percent(time.Second, false)
	if err != nil || len(percent) == 0 {
		return 0
	}
	return percent[0]
}

func (s *StatsService) GetMemoryUsage() float64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return float64(m.Alloc) / float64(m.Sys) * 100
}

func (s *StatsService) GetDiskUsage() map[string]int64 {
	usage := make(map[string]int64)

	partitions, err := disk.Partitions(false)
	if err != nil {
		return usage
	}

	for _, partition := range partitions {
		usage[partition.Mountpoint] = 0
		if stats, err := disk.Usage(partition.Mountpoint); err == nil {
			usage[partition.Mountpoint] = int64(stats.UsedPercent)
		}
	}

	return usage
}

func (s *StatsService) GetRecentErrors() []string {
	s.errorsMux.RLock()
	defer s.errorsMux.RUnlock()

	errors := make([]string, len(s.errors))
	copy(errors, s.errors)
	return errors
}
