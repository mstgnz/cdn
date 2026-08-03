package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/mstgnz/cdn/pkg/config"
)

// Archive is the cold half of the storage story: MinIO holds what is recent,
// the archive holds everything, and the two are addressed by the same
// (bucket, object) pair so nothing has to be indexed to find an old file again.
//
// Why this exists as its own interface rather than more methods on AwsService:
// the handlers do not care that the archive happens to be S3. They care whether
// archiving is on, and how to read or write one object. Keeping that surface to
// four methods is also what makes it cheap to fake in tests, where AwsService's
// sixteen methods are not.
type Archive interface {
	// Enabled reports whether archiving is configured. Everything else on this
	// interface returns ErrArchiveDisabled when it is false.
	Enabled() bool

	// Put writes an object to the archive tier.
	Put(ctx context.Context, bucket, object string, body io.Reader) error

	// Open returns the archived object's contents and size. The caller owns the
	// reader and must close it.
	Open(ctx context.Context, bucket, object string) (io.ReadCloser, int64, error)

	// Stat returns the archived object's size without transferring it. This is
	// the proof the retention job requires before deleting the MinIO copy.
	Stat(ctx context.Context, bucket, object string) (int64, error)
}

var (
	// ErrArchiveDisabled means no AWS credentials were configured. This is a
	// normal state, not a failure: the project is deployed by people who run it
	// on MinIO alone, and for them the archive simply does not exist. Callers
	// must treat it as "skip", never as an error to report.
	ErrArchiveDisabled = errors.New("archive: not configured")

	// ErrArchiveNotFound means the object has no archived copy. For the retention
	// job this is the signal to keep the MinIO copy rather than delete it.
	ErrArchiveNotFound = errors.New("archive: object not found")
)

type archive struct {
	aws AwsService

	enabled bool

	// bucket, when set, collapses every MinIO bucket into one archive bucket and
	// distinguishes them by key prefix instead. Empty means bucket-name parity.
	bucket string
}

// NewArchive builds the archive from the environment. The enabled decision is
// made once, here, rather than per request: whether this deployment has an
// archive is a property of the deployment, and re-deciding it per upload would
// let a transient credential problem silently drop objects that the retention
// job later expects to find.
func NewArchive(aws AwsService) Archive {
	return &archive{
		aws:     aws,
		enabled: archiveConfigured(),
		bucket:  strings.TrimSpace(config.GetEnvOrDefault("ARCHIVE_BUCKET", "")),
	}
}

// archiveConfigured reports whether the environment describes a usable archive.
//
// Credentials are checked rather than probed. A network probe at boot would make
// startup depend on AWS being reachable, and a deployment that has no AWS at all
// would pay a timeout for it on every boot. Nothing here logs the values.
func archiveConfigured() bool {
	// An explicit off-switch, so a deployment that has AWS credentials for some
	// other reason can still decline to archive.
	if !config.GetEnvAsBoolOrDefault("ARCHIVE_ENABLED", true) {
		return false
	}

	required := []string{
		config.GetEnvOrDefault("AWS_ACCESS_KEY_ID", ""),
		config.GetEnvOrDefault("AWS_SECRET_ACCESS_KEY", ""),
		config.GetEnvOrDefault("AWS_REGION", ""),
	}
	for _, v := range required {
		if strings.TrimSpace(v) == "" {
			return false
		}
	}
	return true
}

func (a *archive) Enabled() bool { return a.enabled }

// resolve maps a MinIO location onto its archive location.
//
// The two layouts exist because they suit different scales. Bucket-name parity
// is the default and needs no configuration, but it means one S3 bucket, one
// lifecycle rule and one IAM statement per MinIO bucket. Setting ARCHIVE_BUCKET
// puts everything in one bucket under a "<minio-bucket>/" prefix, which is far
// easier to administer once there are more than a handful.
func (a *archive) resolve(bucket, object string) (string, string) {
	if a.bucket != "" {
		return a.bucket, bucket + "/" + object
	}
	return bucket, object
}

func (a *archive) Put(ctx context.Context, bucket, object string, body io.Reader) error {
	if !a.enabled {
		return ErrArchiveDisabled
	}

	s3Bucket, s3Key := a.resolve(bucket, object)
	if _, err := a.aws.S3PutObject(ctx, s3Bucket, s3Key, body); err != nil {
		return fmt.Errorf("archive put %s/%s: %w", s3Bucket, s3Key, err)
	}
	return nil
}

func (a *archive) Open(ctx context.Context, bucket, object string) (io.ReadCloser, int64, error) {
	if !a.enabled {
		return nil, 0, ErrArchiveDisabled
	}

	s3Bucket, s3Key := a.resolve(bucket, object)
	out, err := a.aws.S3GetObject(ctx, s3Bucket, s3Key)
	if err != nil {
		if isNotFound(err) {
			return nil, 0, ErrArchiveNotFound
		}
		return nil, 0, fmt.Errorf("archive open %s/%s: %w", s3Bucket, s3Key, err)
	}

	var size int64
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	return out.Body, size, nil
}

func (a *archive) Stat(ctx context.Context, bucket, object string) (int64, error) {
	if !a.enabled {
		return 0, ErrArchiveDisabled
	}

	s3Bucket, s3Key := a.resolve(bucket, object)
	out, err := a.aws.S3HeadObject(ctx, s3Bucket, s3Key)
	if err != nil {
		if isNotFound(err) {
			return 0, ErrArchiveNotFound
		}
		return 0, fmt.Errorf("archive stat %s/%s: %w", s3Bucket, s3Key, err)
	}

	if out.ContentLength == nil {
		// Treat an unreported size as unproven rather than as zero. The retention
		// job compares sizes, and a zero here would make every object look like a
		// mismatch, which fails safe, but saying so plainly is clearer.
		return 0, fmt.Errorf("archive stat %s/%s: no content length reported", s3Bucket, s3Key)
	}
	return *out.ContentLength, nil
}

// isNotFound recognises the two shapes S3 uses for a missing object: HeadObject
// answers with NotFound (it has no body to carry a richer error), GetObject with
// NoSuchKey.
func isNotFound(err error) bool {
	var notFound *s3types.NotFound
	if errors.As(err, &notFound) {
		return true
	}
	var noSuchKey *s3types.NoSuchKey
	return errors.As(err, &noSuchKey)
}
