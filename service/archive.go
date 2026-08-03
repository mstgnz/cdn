package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/mstgnz/cdn/pkg/config"
	"github.com/mstgnz/cdn/pkg/observability"
)

// Named once so operator-facing messages can quote the variable someone would
// actually edit, without the name drifting from the check.
const (
	envArchiveEnabled = "ARCHIVE_ENABLED"

	// envArchiveOnlyBuckets is deliberately not "ARCHIVE_BUCKETS": that is one
	// letter away from ARCHIVE_BUCKET, which sets the destination, and a typo
	// between the two would configure something entirely different without
	// looking wrong.
	envArchiveOnlyBuckets = "ARCHIVE_ONLY_BUCKETS"
)

// parseBucketScope reads a comma-separated allowlist. Empty means no limit.
func parseBucketScope(raw string) map[string]struct{} {
	scope := map[string]struct{}{}
	for _, b := range strings.Split(raw, ",") {
		if b = strings.TrimSpace(b); b != "" {
			scope[b] = struct{}{}
		}
	}
	if len(scope) == 0 {
		return nil
	}
	return scope
}

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

	// Reachable reports whether objects from this local bucket have somewhere to
	// go: it resolves the destination and checks it exists and is writable.
	//
	// Callers use it to fail once instead of per object. Nothing creates the
	// destination automatically, so without this a misconfigured deployment
	// discovers the problem one failed upload at a time, several million times.
	Reachable(ctx context.Context, bucket string) error

	// InScope reports whether this bucket is archived at all. Callers use it to
	// skip work rather than to handle a rejection: a bucket left out of the scope
	// is not an error anywhere.
	InScope(bucket string) bool

	// VerifyDestination makes one real call to the archive to find out whether
	// the credentials work and the destination exists. It answers the question a
	// list of known placeholder strings cannot: "are these credentials wrong",
	// as opposed to "were they never filled in".
	//
	// Returns ErrArchiveDisabled when there is nothing to check, and
	// ErrArchiveDestinationPerBucket when the layout has no single destination to
	// test up front.
	VerifyDestination(ctx context.Context) error

	// Walk enumerates what the archive holds for a local bucket, translating
	// archive locations back into local object keys so the caller never has to
	// know which layout is in use. Returning an error from fn stops the walk.
	//
	// Not gated by scope, for the same reason reads are not: the case this exists
	// for includes pulling back a bucket that was removed from the scope.
	Walk(ctx context.Context, bucket string, fn func(key string, size int64) error) error
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

	// ErrArchiveNotInScope means the deployment archives some buckets but not
	// this one. Like ErrArchiveDisabled it describes a configuration, not a
	// fault, and callers must treat it as "skip" rather than report it.
	ErrArchiveNotInScope = errors.New("archive: bucket is not in the archive scope")

	// ErrArchiveDestinationPerBucket means there is no single destination to
	// check: under bucket-name parity each local bucket maps to its own S3
	// bucket, and which ones exist is not knowable before they are used.
	ErrArchiveDestinationPerBucket = errors.New("archive: destination is per bucket, checked on first use")
)

type archive struct {
	aws AwsService

	enabled bool

	// bucket, when set, collapses every MinIO bucket into one archive bucket and
	// distinguishes them by key prefix instead. Empty means bucket-name parity.
	bucket string

	// only limits which local buckets are archived. Nil means every bucket, which
	// is the default: a deployment that turns the archive on almost always wants
	// all of its data covered, and an opt-in list would leave buckets silently
	// unprotected until someone remembered to add them.
	only map[string]struct{}
}

// NewArchive builds the archive from the environment. The enabled decision is
// made once, here, rather than per request: whether this deployment has an
// archive is a property of the deployment, and re-deciding it per upload would
// let a transient credential problem silently drop objects that the retention
// job later expects to find.
func NewArchive(aws AwsService) Archive {
	a := &archive{
		aws:     aws,
		enabled: archiveConfigured(),
		bucket:  strings.TrimSpace(config.GetEnvOrDefault("ARCHIVE_BUCKET", "")),
		only:    parseBucketScope(config.GetEnvOrDefault(envArchiveOnlyBuckets, "")),
	}

	// Say which state we booted into, and when disabled, say which of the two
	// reasons it was. They are not the same thing to an operator: switched off is
	// a decision someone made, missing credentials may well be an oversight, and
	// a single message covering both sends people to check the wrong thing.
	log := observability.Logger()
	switch {
	case a.enabled:
		ev := log.Info().Str("layout", a.layout())
		if a.only == nil {
			ev = ev.Str("scope", "all buckets")
		} else {
			scoped := make([]string, 0, len(a.only))
			for b := range a.only {
				scoped = append(scoped, b)
			}
			sort.Strings(scoped)
			ev = ev.Strs("scope", scoped)
		}
		ev.Msg("archive enabled: uploads are mirrored to cold storage")
	case !config.GetEnvAsBoolOrDefault(envArchiveEnabled, true):
		log.Info().Msg("archive disabled by " + envArchiveEnabled + ": serving from MinIO only")
	default:
		log.Info().Msg("archive disabled: AWS credentials not configured, serving from MinIO only")
	}

	return a
}

// layout names the key mapping in use, so the boot log answers "where do my
// objects actually land" without a trip to the documentation.
func (a *archive) layout() string {
	if a.bucket != "" {
		return "single bucket " + a.bucket + " with <minio-bucket>/ prefix"
	}
	return "one S3 bucket per MinIO bucket, matching names"
}

// archiveConfigured reports whether the environment describes a usable archive.
//
// Credentials are checked rather than probed. A network probe at boot would make
// startup depend on AWS being reachable, and a deployment that has no AWS at all
// would pay a timeout for it on every boot. Nothing here logs the values.
func archiveConfigured() bool {
	// An explicit off-switch, so a deployment that has AWS credentials for some
	// other reason can still decline to archive. Checked before the credentials
	// so that it wins over them rather than merely agreeing with them.
	if !config.GetEnvAsBoolOrDefault(envArchiveEnabled, true) {
		return false
	}

	required := []string{
		config.GetEnvOrDefault("AWS_ACCESS_KEY_ID", ""),
		config.GetEnvOrDefault("AWS_SECRET_ACCESS_KEY", ""),
		config.GetEnvOrDefault("AWS_REGION", ""),
	}
	for _, v := range required {
		if strings.TrimSpace(v) == "" || isPlaceholderCredential(v) {
			return false
		}
	}
	return true
}

// placeholderCredentials are the values .env.example ships. They are not empty,
// which is the whole problem: a deployment that copied the example and never
// filled in AWS would otherwise report the archive as enabled at boot and then
// fail every single upload with an authentication error, sending whoever
// investigates after an AWS problem that does not exist. Treated as "not
// configured", which is what they actually mean.
var placeholderCredentials = map[string]struct{}{
	"your-aws-key":       {},
	"your-aws-secret":    {},
	"your-access-key":    {},
	"your-secret-key":    {},
	"your-aws-region":    {},
	"your-minio-user":    {},
	"your-minio-passwor": {},
	"replace_me":         {},
	"replace-me":         {},
	"changeme":           {},
	"change-me":          {},
}

func isPlaceholderCredential(v string) bool {
	_, ok := placeholderCredentials[strings.ToLower(strings.TrimSpace(v))]
	return ok
}

func (a *archive) Enabled() bool { return a.enabled }

// InScope reports whether this bucket is archived.
//
// Note what this does *not* gate: reading. Open and Stat answer for any bucket,
// deliberately. Scope decides what gets written to the archive, never what can
// be read back out of it, because narrowing the scope must not be able to break
// URLs for objects archived and evicted while the bucket was still included. The
// cost of that choice is one failed archive lookup per miss in an unarchived
// bucket, which the 404 caching in front of this absorbs.
func (a *archive) InScope(bucket string) bool {
	if !a.enabled {
		return false
	}
	if a.only == nil {
		return true
	}
	_, ok := a.only[bucket]
	return ok
}

// resolve maps a MinIO location onto its archive location.
//
// Two layouts, both supported, neither enforced. Bucket-name parity is the
// default because it needs no configuration and mirrors MinIO exactly. Setting
// ARCHIVE_BUCKET puts everything in one bucket under a "<minio-bucket>/" prefix,
// and is the recommended option: S3 bucket names are unique across every AWS
// account, while MinIO's are only unique locally, so an ordinary name cannot
// always be recreated on the S3 side. docs/archive.md has the full argument.
//
// Whichever is chosen, it has to stay chosen. This reads the configuration as it
// is now, so a deployment that switches layouts leaves everything already
// archived at keys nothing will ask for again.
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
	if !a.InScope(bucket) {
		return ErrArchiveNotInScope
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

// Reachable resolves where this bucket's objects would be archived and checks
// that the destination is there.
//
// The destination is never created for you. Bucket creation carries decisions
// this service has no business making on an operator's behalf: region, public
// access blocking, encryption, versioning, object lock. Creating one with SDK
// defaults would quietly produce a bucket full of user uploads without an
// explicit public-access block, which is a worse outcome than a clear error.
func (a *archive) Reachable(ctx context.Context, bucket string) error {
	if !a.enabled {
		return ErrArchiveDisabled
	}
	if !a.InScope(bucket) {
		return ErrArchiveNotInScope
	}

	target, _ := a.resolve(bucket, "")
	if err := a.aws.S3HeadBucket(ctx, target); err != nil {
		return fmt.Errorf("archive bucket %q is not reachable: %w", target, err)
	}
	return nil
}

// VerifyDestination proves the credentials work, rather than merely that
// somebody typed something into them.
//
// Deliberately not wired into Enabled(). Boot must not depend on AWS being
// reachable: a MinIO-only deployment would pay a timeout for it, and a transient
// outage during a restart would silently switch archiving off for the whole life
// of the process, which is a far worse failure than a loud log line. So this is
// something a caller runs and reports; it changes no behaviour.
func (a *archive) VerifyDestination(ctx context.Context) error {
	if !a.enabled {
		return ErrArchiveDisabled
	}
	if a.bucket == "" {
		return ErrArchiveDestinationPerBucket
	}
	return a.Reachable(ctx, "")
}

// ArchiveBucketFor reports where this local bucket's objects are archived, so an
// operator-facing message can name it without re-deriving the mapping.
func (a *archive) ArchiveBucketFor(bucket string) string {
	target, _ := a.resolve(bucket, "")
	return target
}

// Walk lists what the archive holds for a local bucket.
//
// It is the exact inverse of resolve: whichever layout is configured, the caller
// gets back plain local object keys. That is what lets a restore be written
// without any knowledge of where things physically sit, and it keeps the two
// directions of the mapping in one file where they can be read together.
func (a *archive) Walk(ctx context.Context, bucket string, fn func(key string, size int64) error) error {
	if !a.enabled {
		return ErrArchiveDisabled
	}

	s3Bucket, prefix := a.resolve(bucket, "")

	return a.aws.S3ListObjects(ctx, s3Bucket, prefix, func(s3Key string, size int64) error {
		key := strings.TrimPrefix(s3Key, prefix)
		if key == "" {
			// A directory-marker style key, nothing to restore.
			return nil
		}
		return fn(key, size)
	})
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
