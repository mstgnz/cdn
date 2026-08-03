package service

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/mstgnz/cdn/pkg/config"
	"github.com/mstgnz/cdn/pkg/observability"
	"github.com/rs/zerolog"
)

// RetentionStats is the outcome of one pass, both for the log line and for tests
// to assert against.
type RetentionStats struct {
	Scanned      int
	Eligible     int
	Deleted      int
	BytesFreed   int64
	NotArchived  int
	SizeMismatch int
	Errors       int
}

// Retention frees local disk by removing objects that the archive already holds.
//
// The rule the whole design rests on: an object is deleted from MinIO only after
// the archive has been asked for it by name and has answered with a matching
// size. Age alone never authorises a delete. That ordering is what makes the
// window safe to shorten and the job safe to run unattended, and it is why an
// upload whose archive write failed simply keeps occupying disk instead of
// disappearing.
//
// The job never deletes from the archive. Removing an object for good is a
// separate, deliberate act (see DeleteImage), not something a scheduled sweep
// should ever do.
type Retention struct {
	store   ObjectStore
	archive Archive
	tiering *Tiering
	logger  zerolog.Logger

	enabled  bool
	dryRun   bool
	window   time.Duration
	interval time.Duration
	buckets  []string
}

// NewRetention reads the schedule from the environment.
//
// Both switches default to the cautious setting: the job is off unless asked
// for, and the first time it is asked for it only reports. Anything else would
// mean that pulling a new version of this project could start deleting a
// stranger's files on a timer.
func NewRetention(store ObjectStore, archive Archive) *Retention {
	days := config.GetEnvAsIntOrDefault("RETENTION_DAYS", 365)
	if days < 1 {
		days = 1
	}
	hours := config.GetEnvAsIntOrDefault("RETENTION_INTERVAL_HOURS", 24)
	if hours < 1 {
		hours = 1
	}

	var buckets []string
	for _, b := range strings.Split(config.GetEnvOrDefault("RETENTION_BUCKETS", ""), ",") {
		if b = strings.TrimSpace(b); b != "" {
			buckets = append(buckets, b)
		}
	}

	return &Retention{
		store:    store,
		archive:  archive,
		tiering:  NewTiering(store, archive),
		logger:   observability.Logger(),
		enabled:  config.GetEnvAsBoolOrDefault("RETENTION_ENABLED", false),
		dryRun:   config.GetEnvAsBoolOrDefault("RETENTION_DRY_RUN", true),
		window:   time.Duration(days) * 24 * time.Hour,
		interval: time.Duration(hours) * time.Hour,
		buckets:  buckets,
	}
}

// Enabled reports whether the job will do anything. Retention without an archive
// is just deletion, so the archive being off disables it regardless of
// RETENTION_ENABLED.
func (r *Retention) Enabled() bool {
	return r.enabled && r.archive != nil && r.archive.Enabled()
}

// Start runs the job on its interval until ctx is cancelled. It deliberately
// does not sweep immediately on boot: a restart loop would otherwise turn into a
// delete loop, and the first pass after a deploy is the one most likely to be
// running with a misconfigured window.
func (r *Retention) Start(ctx context.Context) {
	if !r.Enabled() {
		r.logger.Info().
			Bool("retention_enabled", r.enabled).
			Msg("retention job not started")
		return
	}

	r.logger.Info().
		Dur("window", r.window).
		Dur("interval", r.interval).
		Bool("dry_run", r.dryRun).
		Strs("buckets", r.buckets).
		Msg("retention job started")

	go func() {
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				stats, err := r.RunOnce(ctx, time.Now())
				ev := r.logger.Info()
				if err != nil {
					ev = r.logger.Error().Err(err)
				}
				ev.
					Int("scanned", stats.Scanned).
					Int("eligible", stats.Eligible).
					Int("deleted", stats.Deleted).
					Int64("bytes_freed", stats.BytesFreed).
					Int("not_archived", stats.NotArchived).
					Int("size_mismatch", stats.SizeMismatch).
					Int("errors", stats.Errors).
					Bool("dry_run", r.dryRun).
					Msg("retention pass complete")
			}
		}
	}()
}

// RunOnce performs a single sweep. now is a parameter rather than a call to
// time.Now so the age boundary can be tested exactly.
func (r *Retention) RunOnce(ctx context.Context, now time.Time) (RetentionStats, error) {
	var stats RetentionStats

	if !r.Enabled() {
		return stats, ErrArchiveDisabled
	}

	buckets, err := r.targetBuckets(ctx)
	if err != nil {
		return stats, err
	}

	cutoff := now.Add(-r.window)
	var counters retentionCounters

	// Listing stays on this goroutine and the per-object work fans out.
	//
	// The work is almost entirely a network round trip: every eligible object
	// costs one HeadObject against the archive before it can be deleted. Done one
	// at a time, a store of a few million objects takes days per pass, which is
	// not a slow sweep so much as a sweep that never finishes. The listing itself
	// is a cheap local stream and is left sequential; the cap keeps the fan-out
	// from turning a routine sweep into a denial of service against the archive.
	workers := config.GetEnvAsIntOrDefault("RETENTION_MAX_CONCURRENT", 8)
	if workers < 1 {
		workers = 1
	}

	for _, bucket := range buckets {
		if ctx.Err() != nil {
			return counters.snapshot(), ctx.Err()
		}

		queue := make(chan minio.ObjectInfo, workers*4)
		var wg sync.WaitGroup

		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for obj := range queue {
					r.process(ctx, bucket, obj, &counters)
				}
			}()
		}

		for obj := range r.store.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true}) {
			if obj.Err != nil {
				counters.errors.Add(1)
				r.logger.Warn().Err(obj.Err).Str("bucket", bucket).Msg("retention: listing failed")
				continue
			}

			counters.scanned.Add(1)
			if !obj.LastModified.Before(cutoff) {
				continue
			}
			counters.eligible.Add(1)
			queue <- obj
		}

		close(queue)
		wg.Wait()
	}

	return counters.snapshot(), nil
}

// process handles one eligible object: verify, then delete if verified.
func (r *Retention) process(ctx context.Context, bucket string, obj minio.ObjectInfo, c *retentionCounters) {
	if !r.verifyArchived(ctx, bucket, obj, c) {
		return
	}

	if r.dryRun {
		r.logger.Debug().
			Str("bucket", bucket).
			Str("object", obj.Key).
			Msg("retention: would delete")
		c.deleted.Add(1)
		c.bytesFreed.Add(obj.Size)
		return
	}

	if err := r.store.RemoveObject(ctx, bucket, obj.Key, minio.RemoveObjectOptions{}); err != nil {
		c.errors.Add(1)
		r.logger.Error().Err(err).
			Str("bucket", bucket).
			Str("object", obj.Key).
			Msg("retention: delete failed")
		return
	}

	c.deleted.Add(1)
	c.bytesFreed.Add(obj.Size)
}

// retentionCounters is the concurrent form of RetentionStats. The exported type
// stays a plain struct so callers and tests keep reading ordinary fields.
type retentionCounters struct {
	scanned      atomic.Int64
	eligible     atomic.Int64
	deleted      atomic.Int64
	bytesFreed   atomic.Int64
	notArchived  atomic.Int64
	sizeMismatch atomic.Int64
	errors       atomic.Int64
}

func (c *retentionCounters) snapshot() RetentionStats {
	return RetentionStats{
		Scanned:      int(c.scanned.Load()),
		Eligible:     int(c.eligible.Load()),
		Deleted:      int(c.deleted.Load()),
		BytesFreed:   c.bytesFreed.Load(),
		NotArchived:  int(c.notArchived.Load()),
		SizeMismatch: int(c.sizeMismatch.Load()),
		Errors:       int(c.errors.Load()),
	}
}

// verifyArchived is the gate every delete passes through. The decision itself
// lives in Tiering so that the scheduled sweep and the on-demand archive
// endpoint cannot drift apart on the one rule that protects the data; what is
// left here is turning the answer into this job's counters and log lines.
func (r *Retention) verifyArchived(ctx context.Context, bucket string, obj minio.ObjectInfo, c *retentionCounters) bool {
	ok, reason, err := r.tiering.VerifyArchived(ctx, bucket, obj.Key, obj.Size)
	if ok {
		return true
	}

	switch reason {
	case VerifyNotArchived:
		c.notArchived.Add(1)
		r.logger.Warn().
			Str("bucket", bucket).
			Str("object", obj.Key).
			Msg("retention: keeping object, no archived copy")
	case VerifySizeMismatch:
		c.sizeMismatch.Add(1)
		r.logger.Warn().
			Str("bucket", bucket).
			Str("object", obj.Key).
			Int64("local_size", obj.Size).
			Msg("retention: keeping object, archived copy differs in size")
	default:
		c.errors.Add(1)
		r.logger.Error().Err(err).
			Str("bucket", bucket).
			Str("object", obj.Key).
			Msg("retention: keeping object, archive check failed")
	}

	return false
}

// targetBuckets resolves the configured bucket list, defaulting to everything
// MinIO knows about.
func (r *Retention) targetBuckets(ctx context.Context) ([]string, error) {
	candidates := r.buckets
	if len(candidates) == 0 {
		infos, err := r.store.ListBuckets(ctx)
		if err != nil {
			return nil, err
		}
		for _, b := range infos {
			candidates = append(candidates, b.Name)
		}
	}

	// A bucket outside the archive scope has nowhere for its objects to go, so
	// sweeping it could only ever produce "keeping object, no archived copy" for
	// every object in it. Drop it here rather than walking millions of objects to
	// reach a conclusion already known.
	names := make([]string, 0, len(candidates))
	for _, b := range candidates {
		if r.archive.InScope(b) {
			names = append(names, b)
		}
	}
	return names, nil
}
