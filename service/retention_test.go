package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

// fakeStore stands in for MinIO. It records deletions rather than performing
// them, which is what lets these tests assert the one property that matters:
// exactly which objects the job decided to remove.
type fakeStore struct {
	// mu guards the recorded mutations: the retention sweep and the batch tools
	// call these from a worker pool, so an unsynchronised append here would make
	// the tests race rather than the code.
	mu sync.Mutex

	buckets    []string
	objects    map[string][]minio.ObjectInfo
	removed    []string
	removeErr  error
	listBucket error

	// contents backs StatObject and OpenObject, keyed "bucket/object". Only the
	// tiering tests populate it; the retention tests drive everything through
	// ListObjects and never read an object's bytes.
	contents map[string][]byte
	openErr  error
	putErr   error
}

func (f *fakeStore) StatObject(_ context.Context, bucket, object string, _ minio.StatObjectOptions) (minio.ObjectInfo, error) {
	if body, ok := f.contents[bucket+"/"+object]; ok {
		return minio.ObjectInfo{Key: object, Size: int64(len(body))}, nil
	}
	return minio.ObjectInfo{}, errors.New("object not found")
}

func (f *fakeStore) PutObject(_ context.Context, bucket, object string, r io.Reader, _ int64, _ minio.PutObjectOptions) (minio.UploadInfo, error) {
	if f.putErr != nil {
		return minio.UploadInfo{}, f.putErr
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return minio.UploadInfo{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.contents == nil {
		f.contents = map[string][]byte{}
	}
	f.contents[bucket+"/"+object] = b
	return minio.UploadInfo{Size: int64(len(b))}, nil
}

func (f *fakeStore) MakeBucket(_ context.Context, bucket string, _ minio.MakeBucketOptions) error {
	f.buckets = append(f.buckets, bucket)
	return nil
}

func (f *fakeStore) BucketExists(_ context.Context, bucket string) (bool, error) {
	for _, b := range f.buckets {
		if b == bucket {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeStore) OpenObject(_ context.Context, bucket, object string) (io.ReadCloser, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	body, ok := f.contents[bucket+"/"+object]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(body)), nil
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
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, bucket+"/"+object)
	return nil
}

// removedKeys returns the recorded deletions in a stable order, since a
// concurrent sweep does not finish them in listing order.
func (f *fakeStore) removedKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string(nil), f.removed...)
	sort.Strings(out)
	return out
}

// fakeArchive answers Stat from a fixed table. Note that Archive has no delete
// method at all: the retention job structurally cannot remove an archived copy,
// which is a stronger guarantee than a test could give.
type fakeArchive struct {
	mu sync.Mutex

	enabled bool
	sizes   map[string]int64
	errs    map[string]error

	// bodies is what Open hands back, keyed "bucket/object". Only the restore
	// tests need it; everything else works from sizes alone.
	bodies map[string][]byte

	putErr       error
	putBodies    []string
	reachableErr error

	// scope nil means every bucket, matching the real archive's default.
	scope map[string]struct{}

	// putCorrupts reproduces the drained-reader bug: the write succeeds and the
	// key exists, but nothing arrived. It is the exact shape the size check has
	// to catch before anything is deleted locally.
	putCorrupts bool
}

func (f *fakeArchive) Enabled() bool { return f.enabled }

func (f *fakeArchive) Reachable(context.Context, string) error {
	if !f.enabled {
		return ErrArchiveDisabled
	}
	return f.reachableErr
}

// Walk lists whatever the fake was seeded with, so a restore can be driven
// without S3.
func (f *fakeArchive) Walk(_ context.Context, bucket string, fn func(string, int64) error) error {
	if !f.enabled {
		return ErrArchiveDisabled
	}
	prefix := bucket + "/"
	keys := make([]string, 0, len(f.sizes))
	for k := range f.sizes {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys) // deterministic order for assertions
	for _, k := range keys {
		if err := fn(strings.TrimPrefix(k, prefix), f.sizes[k]); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeArchive) InScope(bucket string) bool {
	if !f.enabled {
		return false
	}
	if f.scope == nil {
		return true
	}
	_, ok := f.scope[bucket]
	return ok
}

// Put records what actually arrived, so a test can prove the archive received
// the object's real bytes rather than an already-drained reader.
func (f *fakeArchive) Put(_ context.Context, bucket, object string, body io.Reader) error {
	if f.putErr != nil {
		return f.putErr
	}
	b, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sizes == nil {
		f.sizes = map[string]int64{}
	}
	if f.putCorrupts {
		f.sizes[bucket+"/"+object] = 0
	} else {
		f.sizes[bucket+"/"+object] = int64(len(b))
	}
	f.putBodies = append(f.putBodies, string(b))
	return nil
}

// Open serves from bodies when a test seeded them (restore cases) and otherwise
// reports the object missing, which is all the archive-side tests need.
func (f *fakeArchive) Open(_ context.Context, bucket, object string) (io.ReadCloser, int64, error) {
	if !f.enabled {
		return nil, 0, ErrArchiveDisabled
	}
	body, ok := f.bodies[bucket+"/"+object]
	if !ok {
		return nil, 0, ErrArchiveNotFound
	}
	return io.NopCloser(bytes.NewReader(body)), int64(len(body)), nil
}

func (f *fakeArchive) Stat(_ context.Context, bucket, object string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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

// The sweep fans its per-object work out across a worker pool, because each
// eligible object costs a network round trip to the archive and doing those one
// at a time makes a pass over a few million objects take days. Correctness must
// not depend on how the pool happens to interleave: every eligible and verified
// object is deleted exactly once, and every unverified one survives, whatever
// the ordering. Run with -race to catch shared state.
func TestRetentionIsCorrectUnderConcurrency(t *testing.T) {
	const total = 300

	old := time.Now().Add(-400 * 24 * time.Hour)
	objects := make([]minio.ObjectInfo, 0, total)
	sizes := map[string]int64{}
	wantDeleted := 0

	for i := 0; i < total; i++ {
		key := fmt.Sprintf("obj-%03d.bin", i)
		objects = append(objects, minio.ObjectInfo{Key: key, Size: int64(i + 1), LastModified: old})

		switch i % 3 {
		case 0: // archived at the right size: must be deleted
			sizes["photos/"+key] = int64(i + 1)
			wantDeleted++
		case 1: // archived at the wrong size: must survive
			sizes["photos/"+key] = 0
		case 2: // not archived at all: must survive
		}
	}

	store := &fakeStore{
		buckets: []string{"photos"},
		objects: map[string][]minio.ObjectInfo{"photos": objects},
	}
	archive := &fakeArchive{enabled: true, sizes: sizes}

	t.Setenv("RETENTION_MAX_CONCURRENT", "16")
	stats, err := newTestRetention(t, store, archive, 365, false).RunOnce(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	removed := store.removedKeys()
	if len(removed) != wantDeleted {
		t.Fatalf("deleted %d objects, want %d", len(removed), wantDeleted)
	}
	if stats.Scanned != total || stats.Eligible != total {
		t.Errorf("stats: scanned=%d eligible=%d, want %d for both", stats.Scanned, stats.Eligible, total)
	}
	if stats.Deleted != wantDeleted {
		t.Errorf("stats.Deleted=%d, want %d", stats.Deleted, wantDeleted)
	}

	// No duplicates, and nothing unverified slipped through.
	seen := map[string]bool{}
	for _, key := range removed {
		if seen[key] {
			t.Fatalf("%s was deleted twice", key)
		}
		seen[key] = true

		if _, archived := sizes[key]; !archived {
			t.Fatalf("%s was deleted despite having no archived copy", key)
		}
	}
	for key, size := range sizes {
		if size == 0 && seen[key] {
			t.Fatalf("%s was deleted despite a size mismatch", key)
		}
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
