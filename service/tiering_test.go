package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

func newTestTiering(store ObjectStore, archive Archive) *Tiering {
	return NewTiering(store, archive)
}

// The ordinary path: the object is copied to the archive, the copy is verified,
// and only then does the local one go.
func TestArchiveObjectCopiesVerifiesThenEvicts(t *testing.T) {
	store := &fakeStore{contents: map[string][]byte{"photos/cat.jpg": []byte("image bytes")}}
	archive := &fakeArchive{enabled: true}

	res := newTestTiering(store, archive).ArchiveObject(context.Background(), "photos", "cat.jpg", true)

	if res.Outcome != TierArchived {
		t.Fatalf("outcome: want %q, got %q (err %v)", TierArchived, res.Outcome, res.Err)
	}
	if len(store.removed) != 1 || store.removed[0] != "photos/cat.jpg" {
		t.Fatalf("local copy was not removed: %v", store.removed)
	}
	// The bytes have to have actually arrived. Handing the archive an
	// already-drained reader is the failure this whole verification chain exists
	// to catch, and it produced a "successful" upload of nothing.
	if len(archive.putBodies) != 1 || archive.putBodies[0] != "image bytes" {
		t.Fatalf("archive received %q, want %q", archive.putBodies, "image bytes")
	}
	if res.Size != int64(len("image bytes")) {
		t.Errorf("size: want %d, got %d", len("image bytes"), res.Size)
	}
}

// The invariant. If what landed in the archive does not match what is on disk,
// the local copy stays put no matter how successful the upload looked.
func TestArchiveObjectKeepsLocalCopyWhenArchivedSizeDiffers(t *testing.T) {
	store := &fakeStore{contents: map[string][]byte{"photos/cat.jpg": []byte("image bytes")}}
	archive := &fakeArchive{enabled: true, putCorrupts: true}

	res := newTestTiering(store, archive).ArchiveObject(context.Background(), "photos", "cat.jpg", true)

	if res.Outcome != TierFailed {
		t.Fatalf("outcome: want %q, got %q", TierFailed, res.Outcome)
	}
	if len(store.removed) != 0 {
		t.Fatalf("deleted the local copy despite a failed verification: %v", store.removed)
	}
}

// An unreadable source must not be reported as archived either.
func TestArchiveObjectKeepsLocalCopyWhenReadFails(t *testing.T) {
	store := &fakeStore{
		contents: map[string][]byte{"photos/cat.jpg": []byte("image bytes")},
		openErr:  errors.New("disk error"),
	}
	archive := &fakeArchive{enabled: true}

	res := newTestTiering(store, archive).ArchiveObject(context.Background(), "photos", "cat.jpg", true)

	if res.Outcome != TierFailed {
		t.Fatalf("outcome: want %q, got %q", TierFailed, res.Outcome)
	}
	if len(store.removed) != 0 {
		t.Fatalf("deleted a local copy that was never archived: %v", store.removed)
	}
}

// evict=false is for callers who want a copy in cold storage without giving up
// the local one yet.
func TestArchiveObjectKeepsLocalCopyWhenEvictIsFalse(t *testing.T) {
	store := &fakeStore{contents: map[string][]byte{"photos/cat.jpg": []byte("image bytes")}}
	archive := &fakeArchive{enabled: true}

	res := newTestTiering(store, archive).ArchiveObject(context.Background(), "photos", "cat.jpg", false)

	if res.Outcome != TierArchivedKept {
		t.Fatalf("outcome: want %q, got %q", TierArchivedKept, res.Outcome)
	}
	if len(store.removed) != 0 {
		t.Fatalf("evict=false still deleted the local copy: %v", store.removed)
	}
	if len(archive.putBodies) != 1 {
		t.Fatalf("object was not archived: %v", archive.putBodies)
	}
}

// Calling again on an already-archived object must be a no-op, not an error.
// Callers retry batches, and a retry that reports failure for work already done
// is indistinguishable from real failure.
func TestArchiveObjectIsIdempotentAfterEviction(t *testing.T) {
	store := &fakeStore{contents: map[string][]byte{"photos/cat.jpg": []byte("image bytes")}}
	archive := &fakeArchive{enabled: true}
	tiering := newTestTiering(store, archive)

	first := tiering.ArchiveObject(context.Background(), "photos", "cat.jpg", true)
	if first.Outcome != TierArchived {
		t.Fatalf("first call: want %q, got %q", TierArchived, first.Outcome)
	}

	// Evicted, so the second call finds nothing locally.
	delete(store.contents, "photos/cat.jpg")

	second := tiering.ArchiveObject(context.Background(), "photos", "cat.jpg", true)
	if second.Outcome != TierAlreadyArchived {
		t.Fatalf("second call: want %q, got %q (err %v)", TierAlreadyArchived, second.Outcome, second.Err)
	}
}

// An object already in the archive at the right size is not uploaded again. On a
// re-run over a large batch this is the difference between one HeadObject and a
// full read plus write per object.
func TestArchiveObjectSkipsUploadWhenAlreadyArchived(t *testing.T) {
	store := &fakeStore{contents: map[string][]byte{"photos/cat.jpg": []byte("image bytes")}}
	archive := &fakeArchive{
		enabled: true,
		sizes:   map[string]int64{"photos/cat.jpg": int64(len("image bytes"))},
	}

	res := newTestTiering(store, archive).ArchiveObject(context.Background(), "photos", "cat.jpg", true)

	if res.Outcome != TierArchived {
		t.Fatalf("outcome: want %q, got %q", TierArchived, res.Outcome)
	}
	if len(archive.putBodies) != 0 {
		t.Fatalf("re-uploaded an object the archive already had: %v", archive.putBodies)
	}
	if len(store.removed) != 1 {
		t.Fatalf("verified object was not evicted: %v", store.removed)
	}
}

// Absent from both tiers is a plain not-found, not a failure to investigate.
func TestArchiveObjectReportsNotFound(t *testing.T) {
	store := &fakeStore{contents: map[string][]byte{}}
	archive := &fakeArchive{enabled: true}

	res := newTestTiering(store, archive).ArchiveObject(context.Background(), "photos", "ghost.jpg", true)

	if res.Outcome != TierNotFound {
		t.Fatalf("outcome: want %q, got %q", TierNotFound, res.Outcome)
	}
	if len(store.removed) != 0 {
		t.Fatalf("removed something for a missing object: %v", store.removed)
	}
}

// With no archive configured there is nowhere for the object to go, so the
// request must fail rather than quietly deleting the only copy.
func TestArchiveObjectRefusesWithoutArchive(t *testing.T) {
	store := &fakeStore{contents: map[string][]byte{"photos/cat.jpg": []byte("image bytes")}}

	tiering := newTestTiering(store, &fakeArchive{enabled: false})

	if tiering.Enabled() {
		t.Fatal("tiering reported enabled with the archive off")
	}

	res := tiering.ArchiveObject(context.Background(), "photos", "cat.jpg", true)
	if res.Outcome != TierFailed || !errors.Is(res.Err, ErrArchiveDisabled) {
		t.Fatalf("want failed/ErrArchiveDisabled, got %q / %v", res.Outcome, res.Err)
	}
	if len(store.removed) != 0 {
		t.Fatalf("deleted a local copy with no archive configured: %v", store.removed)
	}
}

// A bucket outside the archive scope has nowhere for its objects to go, so the
// one thing that must never happen is the local copy being removed anyway.
func TestArchiveObjectSkipsAndPreservesOutOfScopeBucket(t *testing.T) {
	store := &fakeStore{contents: map[string][]byte{"videos/clip.mp4": []byte("frames")}}
	archive := &fakeArchive{
		enabled: true,
		scope:   map[string]struct{}{"photos": {}},
	}

	res := newTestTiering(store, archive).ArchiveObject(context.Background(), "videos", "clip.mp4", true)

	if res.Outcome != TierNotInScope {
		t.Fatalf("outcome: want %q, got %q", TierNotInScope, res.Outcome)
	}
	if len(store.removed) != 0 {
		t.Fatalf("deleted the only copy of an object that is never archived: %v", store.removed)
	}
	if len(archive.putBodies) != 0 {
		t.Fatalf("uploaded an out-of-scope object: %v", archive.putBodies)
	}
}

// The retention sweep must not even walk a bucket it can never delete from:
// every object would produce the same "no archived copy" warning.
func TestRetentionSkipsOutOfScopeBuckets(t *testing.T) {
	old := time.Now().Add(-400 * 24 * time.Hour)
	store := &fakeStore{
		buckets: []string{"photos", "videos"},
		objects: map[string][]minio.ObjectInfo{
			"photos": {{Key: "cat.jpg", Size: 10, LastModified: old}},
			"videos": {{Key: "clip.mp4", Size: 10, LastModified: old}},
		},
	}
	archive := &fakeArchive{
		enabled: true,
		scope:   map[string]struct{}{"photos": {}},
		sizes: map[string]int64{
			"photos/cat.jpg":  10,
			"videos/clip.mp4": 10,
		},
	}

	stats, err := newTestRetention(t, store, archive, 365, false).RunOnce(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(store.removed) != 1 || store.removed[0] != "photos/cat.jpg" {
		t.Fatalf("removed: want only photos/cat.jpg, got %v", store.removed)
	}
	if stats.Scanned != 1 {
		t.Errorf("the out-of-scope bucket was walked anyway: scanned=%d", stats.Scanned)
	}
}

// The way out of cold storage. Without this, adopting the archive would be a
// one-way door.
func TestRestoreObjectCopiesBackToLocalStorage(t *testing.T) {
	store := &fakeStore{buckets: []string{"photos"}, contents: map[string][]byte{}}
	archive := &fakeArchive{
		enabled: true,
		sizes:   map[string]int64{"photos/cat.jpg": 11},
		bodies:  map[string][]byte{"photos/cat.jpg": []byte("image bytes")},
	}

	res := newTestTiering(store, archive).RestoreObject(context.Background(), "photos", "cat.jpg", 11)

	if res.Outcome != TierRestored {
		t.Fatalf("outcome: want %q, got %q (err %v)", TierRestored, res.Outcome, res.Err)
	}
	if got := string(store.contents["photos/cat.jpg"]); got != "image bytes" {
		t.Fatalf("local copy holds %q, want %q", got, "image bytes")
	}
}

// Restoring must never take anything out of the archive. Emptying S3 is a
// separate decision made once the operator is satisfied, and a bug in this
// direction would destroy the last remaining copy.
func TestRestoreObjectLeavesTheArchiveIntact(t *testing.T) {
	store := &fakeStore{buckets: []string{"photos"}, contents: map[string][]byte{}}
	archive := &fakeArchive{
		enabled: true,
		sizes:   map[string]int64{"photos/cat.jpg": 11},
		bodies:  map[string][]byte{"photos/cat.jpg": []byte("image bytes")},
	}

	newTestTiering(store, archive).RestoreObject(context.Background(), "photos", "cat.jpg", 11)

	if _, err := archive.Stat(context.Background(), "photos", "cat.jpg"); err != nil {
		t.Fatalf("the archived copy is gone after a restore: %v", err)
	}
}

// Rerunning a restore has to be cheap and harmless, because a transfer of this
// size will be interrupted at least once.
func TestRestoreObjectSkipsWhatIsAlreadyLocal(t *testing.T) {
	store := &fakeStore{
		buckets:  []string{"photos"},
		contents: map[string][]byte{"photos/cat.jpg": []byte("image bytes")},
	}
	archive := &fakeArchive{
		enabled: true,
		sizes:   map[string]int64{"photos/cat.jpg": 11},
		bodies:  map[string][]byte{"photos/cat.jpg": []byte("image bytes")},
	}

	res := newTestTiering(store, archive).RestoreObject(context.Background(), "photos", "cat.jpg", 11)

	if res.Outcome != TierAlreadyLocal {
		t.Fatalf("outcome: want %q, got %q", TierAlreadyLocal, res.Outcome)
	}
}

// A local copy of a different size is not the object, so it gets overwritten
// with what the archive holds rather than being trusted.
func TestRestoreObjectReplacesAMismatchedLocalCopy(t *testing.T) {
	store := &fakeStore{
		buckets:  []string{"photos"},
		contents: map[string][]byte{"photos/cat.jpg": []byte("truncated")},
	}
	archive := &fakeArchive{
		enabled: true,
		sizes:   map[string]int64{"photos/cat.jpg": 11},
		bodies:  map[string][]byte{"photos/cat.jpg": []byte("image bytes")},
	}

	res := newTestTiering(store, archive).RestoreObject(context.Background(), "photos", "cat.jpg", 11)

	if res.Outcome != TierRestored {
		t.Fatalf("outcome: want %q, got %q", TierRestored, res.Outcome)
	}
	if got := string(store.contents["photos/cat.jpg"]); got != "image bytes" {
		t.Fatalf("local copy holds %q, want the archived bytes", got)
	}
}

// Restore is deliberately not gated by ARCHIVE_ONLY_BUCKETS: pulling back a
// bucket that was dropped from the scope is one of the cases it exists for.
func TestRestoreObjectIgnoresTheArchiveScope(t *testing.T) {
	store := &fakeStore{buckets: []string{"videos"}, contents: map[string][]byte{}}
	archive := &fakeArchive{
		enabled: true,
		scope:   map[string]struct{}{"photos": {}},
		sizes:   map[string]int64{"videos/clip.mp4": 6},
		bodies:  map[string][]byte{"videos/clip.mp4": []byte("frames")},
	}

	res := newTestTiering(store, archive).RestoreObject(context.Background(), "videos", "clip.mp4", 6)

	if res.Outcome != TierRestored {
		t.Fatalf("an out-of-scope bucket could not be restored: %q (%v)", res.Outcome, res.Err)
	}
}

// A restore into a deployment whose buckets were removed still needs somewhere
// to land.
func TestEnsureBucketCreatesAMissingLocalBucket(t *testing.T) {
	store := &fakeStore{}
	tiering := newTestTiering(store, &fakeArchive{enabled: true})

	if err := tiering.EnsureBucket(context.Background(), "photos"); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}

	exists, _ := store.BucketExists(context.Background(), "photos")
	if !exists {
		t.Fatal("the local bucket was not created")
	}
}

// VerifyArchived is shared with the retention sweep, so its reasons are part of
// the contract rather than an implementation detail.
func TestVerifyArchivedReasons(t *testing.T) {
	ctx := context.Background()

	t.Run("matching size", func(t *testing.T) {
		a := &fakeArchive{enabled: true, sizes: map[string]int64{"b/o": 100}}
		ok, reason, err := newTestTiering(&fakeStore{}, a).VerifyArchived(ctx, "b", "o", 100)
		if !ok || reason != VerifyOK || err != nil {
			t.Fatalf("want ok, got ok=%v reason=%q err=%v", ok, reason, err)
		}
	})

	t.Run("absent", func(t *testing.T) {
		a := &fakeArchive{enabled: true, sizes: map[string]int64{}}
		ok, reason, _ := newTestTiering(&fakeStore{}, a).VerifyArchived(ctx, "b", "o", 100)
		if ok || reason != VerifyNotArchived {
			t.Fatalf("want not_archived, got ok=%v reason=%q", ok, reason)
		}
	})

	t.Run("size mismatch", func(t *testing.T) {
		a := &fakeArchive{enabled: true, sizes: map[string]int64{"b/o": 0}}
		ok, reason, _ := newTestTiering(&fakeStore{}, a).VerifyArchived(ctx, "b", "o", 100)
		if ok || reason != VerifySizeMismatch {
			t.Fatalf("want size_mismatch, got ok=%v reason=%q", ok, reason)
		}
	})

	t.Run("unreachable archive is not an empty archive", func(t *testing.T) {
		a := &fakeArchive{enabled: true, errs: map[string]error{"b/o": errors.New("connection reset")}}
		ok, reason, err := newTestTiering(&fakeStore{}, a).VerifyArchived(ctx, "b", "o", 100)
		if ok {
			t.Fatal("verified an object while the archive was unreachable")
		}
		if reason != VerifyError || err == nil {
			t.Fatalf("want an error reason, got reason=%q err=%v", reason, err)
		}
	})
}
