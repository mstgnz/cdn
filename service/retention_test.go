package service

import (
	"context"
	"errors"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

// fakeStore stands in for MinIO. It records deletions rather than performing
// them, which is what lets these tests assert the one property that matters:
// exactly which objects the job decided to remove.
type fakeStore struct {
	buckets    []string
	objects    map[string][]minio.ObjectInfo
	removed    []string
	removeErr  error
	listBucket error
}

func (f *fakeStore) ListBuckets(context.Context) ([]minio.BucketInfo, error) {
	if f.listBucket != nil {
		return nil, f.listBucket
	}
	out := make([]minio.BucketInfo, 0, len(f.buckets))
	for _, b := range f.buckets {
		out = append(out, minio.BucketInfo{Name: b})
	}
	return out, nil
}

func (f *fakeStore) ListObjects(_ context.Context, bucket string, _ minio.ListObjectsOptions) <-chan minio.ObjectInfo {
	ch := make(chan minio.ObjectInfo)
	go func() {
		defer close(ch)
		for _, o := range f.objects[bucket] {
			ch <- o
		}
	}()
	return ch
}

func (f *fakeStore) RemoveObject(_ context.Context, bucket, object string, _ minio.RemoveObjectOptions) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removed = append(f.removed, bucket+"/"+object)
	return nil
}

// fakeArchive answers Stat from a fixed table. Note that Archive has no delete
// method at all: the retention job structurally cannot remove an archived copy,
// which is a stronger guarantee than a test could give.
type fakeArchive struct {
	enabled bool
	sizes   map[string]int64
	errs    map[string]error
}

func (f *fakeArchive) Enabled() bool { return f.enabled }

func (f *fakeArchive) Put(context.Context, string, string, io.Reader) error {
	return nil
}

func (f *fakeArchive) Open(context.Context, string, string) (io.ReadCloser, int64, error) {
	return nil, 0, ErrArchiveNotFound
}

func (f *fakeArchive) Stat(_ context.Context, bucket, object string) (int64, error) {
	key := bucket + "/" + object
	if err, ok := f.errs[key]; ok {
		return 0, err
	}
	if size, ok := f.sizes[key]; ok {
		return size, nil
	}
	return 0, ErrArchiveNotFound
}

// newTestRetention builds a job with the schedule already switched on, so each
// test only has to describe the state it cares about.
func newTestRetention(t *testing.T, store ObjectStore, archive Archive, days int, dryRun bool) *Retention {
	t.Helper()
	t.Setenv("RETENTION_ENABLED", "true")
	t.Setenv("RETENTION_DRY_RUN", strconv.FormatBool(dryRun))
	t.Setenv("RETENTION_DAYS", strconv.Itoa(days))
	t.Setenv("RETENTION_BUCKETS", "")
	return NewRetention(store, archive)
}

// The single happy path: old enough, archived, sizes agree, so the local copy
// goes and the disk is freed.
func TestRetentionDeletesArchivedObjectPastWindow(t *testing.T) {
	old := time.Now().Add(-400 * 24 * time.Hour)
	store := &fakeStore{
		buckets: []string{"photos"},
		objects: map[string][]minio.ObjectInfo{
			"photos": {{Key: "2023/cat.jpg", Size: 1024, LastModified: old}},
		},
	}
	archive := &fakeArchive{enabled: true, sizes: map[string]int64{"photos/2023/cat.jpg": 1024}}

	stats, err := newTestRetention(t, store, archive, 365, false).RunOnce(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(store.removed) != 1 || store.removed[0] != "photos/2023/cat.jpg" {
		t.Fatalf("removed: want [photos/2023/cat.jpg], got %v", store.removed)
	}
	if stats.Deleted != 1 || stats.BytesFreed != 1024 {
		t.Errorf("stats: want 1 deleted / 1024 bytes, got %d / %d", stats.Deleted, stats.BytesFreed)
	}
}

// The invariant the whole design rests on. An object with no archived copy is
// old, eligible, and must still survive, because deleting it would be the only
// unrecoverable act this service can perform.
func TestRetentionKeepsObjectWithNoArchivedCopy(t *testing.T) {
	old := time.Now().Add(-400 * 24 * time.Hour)
	store := &fakeStore{
		buckets: []string{"photos"},
		objects: map[string][]minio.ObjectInfo{
			"photos": {{Key: "2023/orphan.jpg", Size: 2048, LastModified: old}},
		},
	}
	archive := &fakeArchive{enabled: true, sizes: map[string]int64{}}

	stats, err := newTestRetention(t, store, archive, 365, false).RunOnce(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(store.removed) != 0 {
		t.Fatalf("deleted an object with no archived copy: %v", store.removed)
	}
	if stats.Eligible != 1 || stats.NotArchived != 1 || stats.Deleted != 0 {
		t.Errorf("stats: want eligible=1 notArchived=1 deleted=0, got %+v", stats)
	}
}

// A size mismatch means the archived copy is not the object. This is not
// hypothetical: the first version of the archive upload handed S3 an already
// drained reader, so every archived object was zero bytes while existing
// perfectly well. Existence alone would have authorised deleting all of them.
func TestRetentionKeepsObjectWhenArchivedSizeDiffers(t *testing.T) {
	old := time.Now().Add(-400 * 24 * time.Hour)
	store := &fakeStore{
		buckets: []string{"photos"},
		objects: map[string][]minio.ObjectInfo{
			"photos": {{Key: "2023/truncated.jpg", Size: 5000, LastModified: old}},
		},
	}
	archive := &fakeArchive{enabled: true, sizes: map[string]int64{"photos/2023/truncated.jpg": 0}}

	stats, err := newTestRetention(t, store, archive, 365, false).RunOnce(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(store.removed) != 0 {
		t.Fatalf("deleted an object whose archived copy was a different size: %v", store.removed)
	}
	if stats.SizeMismatch != 1 {
		t.Errorf("want sizeMismatch=1, got %+v", stats)
	}
}

// An archive that cannot be reached is not an archive that is empty. A transient
// S3 outage must postpone the sweep, never license it.
func TestRetentionKeepsObjectWhenArchiveCheckFails(t *testing.T) {
	old := time.Now().Add(-400 * 24 * time.Hour)
	store := &fakeStore{
		buckets: []string{"photos"},
		objects: map[string][]minio.ObjectInfo{
			"photos": {{Key: "2023/cat.jpg", Size: 1024, LastModified: old}},
		},
	}
	archive := &fakeArchive{
		enabled: true,
		errs:    map[string]error{"photos/2023/cat.jpg": errors.New("connection reset")},
	}

	stats, err := newTestRetention(t, store, archive, 365, false).RunOnce(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(store.removed) != 0 {
		t.Fatalf("deleted an object while the archive was unreachable: %v", store.removed)
	}
	if stats.Errors != 1 {
		t.Errorf("want errors=1, got %+v", stats)
	}
}

// The age boundary. An object exactly at the window is inside it and stays.
func TestRetentionRespectsWindowBoundary(t *testing.T) {
	now := time.Now()
	store := &fakeStore{
		buckets: []string{"photos"},
		objects: map[string][]minio.ObjectInfo{
			"photos": {
				{Key: "just-inside.jpg", Size: 10, LastModified: now.Add(-365 * 24 * time.Hour)},
				{Key: "just-outside.jpg", Size: 10, LastModified: now.Add(-366 * 24 * time.Hour)},
			},
		},
	}
	archive := &fakeArchive{enabled: true, sizes: map[string]int64{
		"photos/just-inside.jpg":  10,
		"photos/just-outside.jpg": 10,
	}}

	stats, err := newTestRetention(t, store, archive, 365, false).RunOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(store.removed) != 1 || store.removed[0] != "photos/just-outside.jpg" {
		t.Fatalf("removed: want only just-outside.jpg, got %v", store.removed)
	}
	if stats.Scanned != 2 || stats.Eligible != 1 {
		t.Errorf("stats: want scanned=2 eligible=1, got %+v", stats)
	}
}

// Dry run is the default the first time retention is switched on. It must report
// what it would do and touch nothing.
func TestRetentionDryRunDeletesNothing(t *testing.T) {
	old := time.Now().Add(-400 * 24 * time.Hour)
	store := &fakeStore{
		buckets: []string{"photos"},
		objects: map[string][]minio.ObjectInfo{
			"photos": {{Key: "2023/cat.jpg", Size: 1024, LastModified: old}},
		},
	}
	archive := &fakeArchive{enabled: true, sizes: map[string]int64{"photos/2023/cat.jpg": 1024}}

	stats, err := newTestRetention(t, store, archive, 365, true).RunOnce(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(store.removed) != 0 {
		t.Fatalf("dry run deleted objects: %v", store.removed)
	}
	if stats.Deleted != 1 {
		t.Errorf("dry run should still report what it would delete, got %+v", stats)
	}
}

// Retention without an archive is just deletion, so the archive being off has to
// disable the job outright regardless of RETENTION_ENABLED.
func TestRetentionRefusesToRunWithoutArchive(t *testing.T) {
	old := time.Now().Add(-400 * 24 * time.Hour)
	store := &fakeStore{
		buckets: []string{"photos"},
		objects: map[string][]minio.ObjectInfo{
			"photos": {{Key: "2023/cat.jpg", Size: 1024, LastModified: old}},
		},
	}

	r := newTestRetention(t, store, &fakeArchive{enabled: false}, 365, false)

	if r.Enabled() {
		t.Fatal("retention reported enabled with the archive off")
	}

	_, err := r.RunOnce(context.Background(), time.Now())
	if !errors.Is(err, ErrArchiveDisabled) {
		t.Fatalf("want ErrArchiveDisabled, got %v", err)
	}
	if len(store.removed) != 0 {
		t.Fatalf("deleted objects with no archive configured: %v", store.removed)
	}
}

// RETENTION_ENABLED=false is the shipped default, so a deployment that pulls a
// new version never starts deleting on a timer it did not ask for.
func TestRetentionDisabledByDefault(t *testing.T) {
	t.Setenv("RETENTION_ENABLED", "")
	t.Setenv("RETENTION_DRY_RUN", "")

	r := NewRetention(&fakeStore{}, &fakeArchive{enabled: true})
	if r.Enabled() {
		t.Fatal("retention is enabled by default; it must be opt-in")
	}
}

// Scoping to named buckets must not fall back to sweeping everything.
func TestRetentionOnlyTouchesConfiguredBuckets(t *testing.T) {
	old := time.Now().Add(-400 * 24 * time.Hour)
	store := &fakeStore{
		buckets: []string{"photos", "documents"},
		objects: map[string][]minio.ObjectInfo{
			"photos":    {{Key: "cat.jpg", Size: 10, LastModified: old}},
			"documents": {{Key: "deed.pdf", Size: 10, LastModified: old}},
		},
	}
	archive := &fakeArchive{enabled: true, sizes: map[string]int64{
		"photos/cat.jpg":     10,
		"documents/deed.pdf": 10,
	}}

	t.Setenv("RETENTION_ENABLED", "true")
	t.Setenv("RETENTION_DRY_RUN", "false")
	t.Setenv("RETENTION_DAYS", "365")
	t.Setenv("RETENTION_BUCKETS", "photos")

	if _, err := NewRetention(store, archive).RunOnce(context.Background(), time.Now()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(store.removed) != 1 || store.removed[0] != "photos/cat.jpg" {
		t.Fatalf("removed: want only photos/cat.jpg, got %v", store.removed)
	}
}
