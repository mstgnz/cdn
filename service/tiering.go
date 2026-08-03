package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/minio/minio-go/v7"
	"github.com/rs/zerolog"

	"github.com/mstgnz/cdn/pkg/observability"
)

// ObjectStore is the slice of MinIO that the tiering and retention paths use.
// It exists so the code allowed to delete user data can be tested without a
// running MinIO. MinioStore adapts the real client to it.
type ObjectStore interface {
	ListBuckets(ctx context.Context) ([]minio.BucketInfo, error)
	ListObjects(ctx context.Context, bucket string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo
	StatObject(ctx context.Context, bucket, object string, opts minio.StatObjectOptions) (minio.ObjectInfo, error)
	RemoveObject(ctx context.Context, bucket, object string, opts minio.RemoveObjectOptions) error
	PutObject(ctx context.Context, bucket, object string, reader io.Reader, size int64, opts minio.PutObjectOptions) (minio.UploadInfo, error)
	MakeBucket(ctx context.Context, bucket string, opts minio.MakeBucketOptions) error
	BucketExists(ctx context.Context, bucket string) (bool, error)

	// OpenObject reads an object's contents. It is deliberately not GetObject:
	// the minio-go signature returns the concrete *minio.Object, which cannot be
	// constructed by a test, and this is the one method a fake has to provide.
	OpenObject(ctx context.Context, bucket, object string) (io.ReadCloser, error)
}

// MinioStore adapts *minio.Client to ObjectStore. Embedding supplies every
// method but OpenObject, which narrows the concrete return type to an interface.
type MinioStore struct {
	*minio.Client
}

func (m MinioStore) OpenObject(ctx context.Context, bucket, object string) (io.ReadCloser, error) {
	obj, err := m.Client.GetObject(ctx, bucket, object, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	return obj, nil
}

// TierOutcome is what happened to one object.
type TierOutcome string

const (
	// TierArchived: copied to the archive and removed from MinIO.
	TierArchived TierOutcome = "archived"
	// TierArchivedKept: copied to the archive, local copy deliberately kept.
	TierArchivedKept TierOutcome = "archived_kept"
	// TierAlreadyArchived: the archive already held it at the right size.
	TierAlreadyArchived TierOutcome = "already_archived"
	// TierNotInScope: the deployment archives some buckets, but not this one.
	TierNotInScope TierOutcome = "not_in_scope"
	// TierRestored: copied back from the archive into local storage.
	TierRestored TierOutcome = "restored"
	// TierAlreadyLocal: local storage already had it at the right size.
	TierAlreadyLocal TierOutcome = "already_local"
	// TierNotFound: neither tier has it.
	TierNotFound TierOutcome = "not_found"
	// TierFailed: something went wrong; the local copy was not touched.
	TierFailed TierOutcome = "failed"
)

// TierResult reports one object's fate.
type TierResult struct {
	Key     string
	Outcome TierOutcome
	Size    int64
	Err     error
}

// VerifyReason explains a verification result in terms the caller can log or
// return without re-deriving it.
type VerifyReason string

const (
	VerifyOK           VerifyReason = "ok"
	VerifyNotArchived  VerifyReason = "not_archived"
	VerifySizeMismatch VerifyReason = "size_mismatch"
	VerifyError        VerifyReason = "error"
)

// Tiering moves objects between the local store and the archive.
//
// It owns the one rule that must not be duplicated anywhere: a local copy is
// removed only after the archive has been asked for that object by name and has
// answered with a matching size. Both the scheduled sweep and the on-demand API
// go through here, so there is a single place where that decision is made and a
// single place to get it wrong.
type Tiering struct {
	store   ObjectStore
	archive Archive
	logger  zerolog.Logger
}

func NewTiering(store ObjectStore, archive Archive) *Tiering {
	return &Tiering{
		store:   store,
		archive: archive,
		logger:  observability.Logger(),
	}
}

// Enabled reports whether tiering can do anything at all.
func (t *Tiering) Enabled() bool {
	return t.archive != nil && t.archive.Enabled()
}

// InScope reports whether this bucket is archived on this deployment.
func (t *Tiering) InScope(bucket string) bool {
	return t.archive != nil && t.archive.InScope(bucket)
}

// VerifyArchived answers the only question that authorises a delete: is this
// exact object, at this exact size, already in the archive?
//
// The size comparison is not redundant with existence. An earlier version of the
// archive upload handed S3 a reader that the MinIO upload had already drained,
// so the archive filled with zero-byte objects that existed by every other
// measure. Comparing sizes is what turns "there is something under that key"
// into "the object is safe to remove locally".
//
// Every failure mode returns false. An archive that cannot be reached is not an
// archive that is empty, and a transient outage must postpone a delete rather
// than license it.
func (t *Tiering) VerifyArchived(ctx context.Context, bucket, key string, localSize int64) (bool, VerifyReason, error) {
	archivedSize, err := t.archive.Stat(ctx, bucket, key)
	if err != nil {
		if errors.Is(err, ErrArchiveNotFound) {
			return false, VerifyNotArchived, nil
		}
		return false, VerifyError, err
	}

	if archivedSize != localSize {
		return false, VerifySizeMismatch, nil
	}
	return true, VerifyOK, nil
}

// ArchiveObject copies one object to the archive and, when evict is set, removes
// the local copy once the archive is verified to hold it.
//
// This is what the on-demand endpoint calls. Age never enters into it: only the
// application that owns the content knows when it stops needing to be local, and
// on a CDN that objects were migrated into, the stored timestamps describe the
// migration rather than the content.
//
// Idempotent by construction. Calling it again on an already-evicted object
// finds nothing locally, confirms the archive still has it, and reports
// TierAlreadyArchived rather than an error.
func (t *Tiering) ArchiveObject(ctx context.Context, bucket, key string, evict bool) TierResult {
	if !t.Enabled() {
		return TierResult{Key: key, Outcome: TierFailed, Err: ErrArchiveDisabled}
	}

	info, statErr := t.store.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if statErr != nil {
		// Nothing locally. That is the expected state for an object evicted by an
		// earlier call, so confirm the archive still has it before calling it lost.
		if _, archiveErr := t.archive.Stat(ctx, bucket, key); archiveErr == nil {
			return TierResult{Key: key, Outcome: TierAlreadyArchived}
		}
		return TierResult{Key: key, Outcome: TierNotFound, Err: statErr}
	}

	return t.ArchiveKnownObject(ctx, bucket, key, info.Size, evict)
}

// ArchiveKnownObject is ArchiveObject for a caller that already knows the
// object's local size, which lets it skip the StatObject round trip.
//
// This is not a micro-optimisation at the scale it exists for: a backfill walks
// millions of objects and gets each one's size from the listing it is already
// paginating through, so re-statting every one of them would double the calls
// into local storage for information already in hand.
func (t *Tiering) ArchiveKnownObject(ctx context.Context, bucket, key string, localSize int64, evict bool) TierResult {
	res := TierResult{Key: key, Size: localSize}

	if !t.Enabled() {
		res.Outcome = TierFailed
		res.Err = ErrArchiveDisabled
		return res
	}

	// A bucket left out of the scope is a configuration choice, not a failure, and
	// crucially not a licence to delete: an object with nowhere to be archived
	// must keep its only copy.
	if !t.archive.InScope(bucket) {
		res.Outcome = TierNotInScope
		return res
	}

	// Skip the upload when the archive already holds this exact object. On a
	// re-run over a large batch this turns almost every object into one HeadObject
	// instead of a full read and write, which is what makes an interrupted
	// backfill cheap to resume.
	verified, _, verifyErr := t.VerifyArchived(ctx, bucket, key, localSize)
	if verifyErr != nil {
		res.Outcome = TierFailed
		res.Err = verifyErr
		return res
	}

	if !verified {
		if err := t.copyToArchive(ctx, bucket, key); err != nil {
			res.Outcome = TierFailed
			res.Err = err
			return res
		}

		// Re-verify against the archive rather than trusting the write. A short
		// write, a truncated reader or a proxy that swallowed the body all produce
		// a successful-looking PUT, and this is the last point before a delete.
		verified, reason, err := t.VerifyArchived(ctx, bucket, key, localSize)
		if err != nil {
			res.Outcome = TierFailed
			res.Err = err
			return res
		}
		if !verified {
			res.Outcome = TierFailed
			res.Err = fmt.Errorf("archive verification failed after upload: %s", reason)
			return res
		}
	}

	if !evict {
		res.Outcome = TierArchivedKept
		return res
	}

	if err := t.store.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{}); err != nil {
		// The archive holds it, so nothing is lost; the local copy just stays.
		res.Outcome = TierFailed
		res.Err = fmt.Errorf("archived but local copy could not be removed: %w", err)
		return res
	}

	res.Outcome = TierArchived
	return res
}

// RestoreObject copies an object back out of the archive into local storage.
//
// This is the way out. A deployment that adopted cold storage and later wants to
// stop using it needs to be able to pull everything home; without that, turning
// the archive on would be a one-way door, and a one-way door is a bad thing to
// ask an operator to walk through.
//
// It never deletes from the archive. Emptying S3 is a separate decision, made
// with the AWS console or CLI once the operator is satisfied that local storage
// holds everything, and deliberately not automated here: a bug in this direction
// would destroy the only remaining copy.
//
// Not gated by ARCHIVE_ONLY_BUCKETS, because pulling back a bucket that was
// removed from the scope is exactly one of the cases this exists for.
func (t *Tiering) RestoreObject(ctx context.Context, bucket, key string, archivedSize int64) TierResult {
	res := TierResult{Key: key, Size: archivedSize}

	if !t.Enabled() {
		res.Outcome = TierFailed
		res.Err = ErrArchiveDisabled
		return res
	}

	// Already local at the right size: nothing to do. This is what makes an
	// interrupted restore cheap to resume and safe to run twice.
	if info, err := t.store.StatObject(ctx, bucket, key, minio.StatObjectOptions{}); err == nil && info.Size == archivedSize {
		res.Outcome = TierAlreadyLocal
		return res
	}

	body, size, err := t.archive.Open(ctx, bucket, key)
	if err != nil {
		if errors.Is(err, ErrArchiveNotFound) {
			res.Outcome = TierNotFound
			return res
		}
		res.Outcome = TierFailed
		res.Err = err
		return res
	}
	defer body.Close()

	// Sniff the type from the first bytes and replay them in front of the rest.
	// Serving does not depend on this (the read path sniffs anyway), but an
	// object restored as application/octet-stream looks broken to anyone browsing
	// the bucket, and the cost is one 512-byte read.
	head := make([]byte, 512)
	n, readErr := io.ReadFull(body, head)
	if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
		res.Outcome = TierFailed
		res.Err = fmt.Errorf("read archived object: %w", readErr)
		return res
	}
	head = head[:n]

	if _, err := t.store.PutObject(ctx, bucket, key,
		io.MultiReader(bytes.NewReader(head), body), size,
		minio.PutObjectOptions{ContentType: http.DetectContentType(head)},
	); err != nil {
		res.Outcome = TierFailed
		res.Err = fmt.Errorf("write to local storage: %w", err)
		return res
	}

	res.Size = size
	res.Outcome = TierRestored
	return res
}

// EnsureBucket creates the local bucket if it is missing, so a restore into a
// deployment whose buckets were removed still has somewhere to land.
func (t *Tiering) EnsureBucket(ctx context.Context, bucket string) error {
	exists, err := t.store.BucketExists(ctx, bucket)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return t.store.MakeBucket(ctx, bucket, minio.MakeBucketOptions{})
}

// Walk exposes the archive listing to callers that already hold a Tiering.
func (t *Tiering) Walk(ctx context.Context, bucket string, fn func(key string, size int64) error) error {
	return t.archive.Walk(ctx, bucket, fn)
}

// copyToArchive streams the object from the local store into the archive.
func (t *Tiering) copyToArchive(ctx context.Context, bucket, key string) error {
	body, err := t.store.OpenObject(ctx, bucket, key)
	if err != nil {
		return fmt.Errorf("read %s/%s: %w", bucket, key, err)
	}
	defer body.Close()

	if err := t.archive.Put(ctx, bucket, key, body); err != nil {
		return err
	}
	return nil
}
