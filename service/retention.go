package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/mstgnz/cdn/pkg/config"
	"github.com/mstgnz/cdn/pkg/observability"
	"github.com/rs/zerolog"
)

// ObjectStore is the slice of MinIO the retention job uses. It exists so the
// deletion logic can be tested without a running MinIO: *minio.Client satisfies
// it as-is, and so does a fake. Given that this is the one component in the
// service allowed to delete user data, "cannot be tested" was not an option.
type ObjectStore interface {
	ListBuckets(ctx context.Context) ([]minio.BucketInfo, error)
	ListObjects(ctx context.Context, bucket string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo
	RemoveObject(ctx context.Context, bucket, object string, opts minio.RemoveObjectOptions) error
}

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

	for _, bucket := range buckets {
		select {
		case <-ctx.Done():
			return stats, ctx.Err()
		default:
		}

		objects := r.store.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true})
		for obj := range objects {
			if obj.Err != nil {
				stats.Errors++
				r.logger.Warn().Err(obj.Err).Str("bucket", bucket).Msg("retention: listing failed")
				continue
			}

			stats.Scanned++
			if !obj.LastModified.Before(cutoff) {
				continue
			}
			stats.Eligible++

			if !r.verifyArchived(ctx, bucket, obj, &stats) {
				continue
			}

			if r.dryRun {
				r.logger.Debug().
					Str("bucket", bucket).
					Str("object", obj.Key).
					Msg("retention: would delete")
				stats.Deleted++
				stats.BytesFreed += obj.Size
				continue
			}

			if err := r.store.RemoveObject(ctx, bucket, obj.Key, minio.RemoveObjectOptions{}); err != nil {
				stats.Errors++
				r.logger.Error().Err(err).
					Str("bucket", bucket).
					Str("object", obj.Key).
					Msg("retention: delete failed")
				continue
			}

			stats.Deleted++
			stats.BytesFreed += obj.Size
		}
	}

	return stats, nil
}

// verifyArchived is the gate every delete passes through. It answers "is this
// exact object, at this exact size, already in the archive?" and nothing else.
//
// The size comparison is not redundant with existence. The first version of the
// archive upload handed S3 a reader that the MinIO upload had already drained,
// so the archive filled up with zero-byte objects that existed by every other
// measure. Comparing sizes is what turns "there is something under that key"
// into "the object is safe to delete locally".
func (r *Retention) verifyArchived(ctx context.Context, bucket string, obj minio.ObjectInfo, stats *RetentionStats) bool {
	archivedSize, err := r.archive.Stat(ctx, bucket, obj.Key)
	if err != nil {
		if errors.Is(err, ErrArchiveNotFound) {
			stats.NotArchived++
			r.logger.Warn().
				Str("bucket", bucket).
				Str("object", obj.Key).
				Msg("retention: keeping object, no archived copy")
			return false
		}

		stats.Errors++
		r.logger.Error().Err(err).
			Str("bucket", bucket).
			Str("object", obj.Key).
			Msg("retention: keeping object, archive check failed")
		return false
	}

	if archivedSize != obj.Size {
		stats.SizeMismatch++
		r.logger.Warn().
			Str("bucket", bucket).
			Str("object", obj.Key).
			Int64("local_size", obj.Size).
			Int64("archived_size", archivedSize).
			Msg("retention: keeping object, archived copy differs in size")
		return false
	}

	return true
}

// targetBuckets resolves the configured bucket list, defaulting to everything
// MinIO knows about.
func (r *Retention) targetBuckets(ctx context.Context) ([]string, error) {
	if len(r.buckets) > 0 {
		return r.buckets, nil
	}

	infos, err := r.store.ListBuckets(ctx)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(infos))
	for _, b := range infos {
		names = append(names, b.Name)
	}
	return names, nil
}
