package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// fakeAws implements only the three S3 calls the archive makes. Embedding the
// interface means any other method panics if it is ever called, which is the
// point: it keeps the fake honest about what the archive actually depends on.
type fakeAws struct {
	AwsService

	putBucket string
	putKey    string
	putBody   []byte
	putErr    error

	headSize int64
	headErr  error

	getBody []byte
	getErr  error

	headBucket    string
	headBucketErr error

	// createdBucket stays empty unless something calls a create, which is the
	// point: nothing should.
	createdBucket string
}

func (f *fakeAws) S3HeadBucket(_ context.Context, bucket string) error {
	f.headBucket = bucket
	return f.headBucketErr
}

func (f *fakeAws) S3PutObject(_ context.Context, bucket, key string, body io.Reader) (*manager.UploadOutput, error) {
	if f.putErr != nil {
		return nil, f.putErr
	}
	b, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	f.putBucket, f.putKey, f.putBody = bucket, key, b
	return &manager.UploadOutput{}, nil
}

func (f *fakeAws) S3HeadObject(_ context.Context, bucket, key string) (*s3.HeadObjectOutput, error) {
	if f.headErr != nil {
		return nil, f.headErr
	}
	f.putBucket, f.putKey = bucket, key
	return &s3.HeadObjectOutput{ContentLength: aws.Int64(f.headSize)}, nil
}

func (f *fakeAws) S3GetObject(_ context.Context, bucket, key string) (*s3.GetObjectOutput, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	f.putBucket, f.putKey = bucket, key
	return &s3.GetObjectOutput{
		Body:          io.NopCloser(bytes.NewReader(f.getBody)),
		ContentLength: aws.Int64(int64(len(f.getBody))),
	}, nil
}

// enableArchiveEnv sets the three variables that make the archive consider
// itself configured.
func enableArchiveEnv(t *testing.T) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret")
	t.Setenv("AWS_REGION", "eu-central-1")
	t.Setenv("ARCHIVE_ENABLED", "true")
	t.Setenv("ARCHIVE_BUCKET", "")
}

// The archive has to be genuinely optional: this project is deployed by people
// who run MinIO alone, and for them a missing AWS key is the normal state rather
// than a misconfiguration to complain about.
func TestArchiveDisabledWithoutCredentials(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_REGION", "")

	a := NewArchive(&fakeAws{})

	if a.Enabled() {
		t.Fatal("archive reported enabled with no credentials configured")
	}

	// Every operation must refuse cleanly rather than reaching for AWS.
	if err := a.Put(context.Background(), "b", "o", strings.NewReader("x")); !errors.Is(err, ErrArchiveDisabled) {
		t.Fatalf("Put: want ErrArchiveDisabled, got %v", err)
	}
	if _, _, err := a.Open(context.Background(), "b", "o"); !errors.Is(err, ErrArchiveDisabled) {
		t.Fatalf("Open: want ErrArchiveDisabled, got %v", err)
	}
	if _, err := a.Stat(context.Background(), "b", "o"); !errors.Is(err, ErrArchiveDisabled) {
		t.Fatalf("Stat: want ErrArchiveDisabled, got %v", err)
	}
}

// A partially configured deployment is treated as unconfigured. Enabling on
// "some credentials present" would produce an archive that accepts writes and
// fails them all, which is the state the retention job must never see.
func TestArchiveDisabledOnPartialCredentials(t *testing.T) {
	cases := []struct {
		name                  string
		keyID, secret, region string
	}{
		{"no key", "", "secret", "eu-central-1"},
		{"no secret", "key", "", "eu-central-1"},
		{"no region", "key", "secret", ""},
		{"whitespace only", "  ", "secret", "eu-central-1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AWS_ACCESS_KEY_ID", tc.keyID)
			t.Setenv("AWS_SECRET_ACCESS_KEY", tc.secret)
			t.Setenv("AWS_REGION", tc.region)

			if NewArchive(&fakeAws{}).Enabled() {
				t.Fatal("archive reported enabled on incomplete credentials")
			}
		})
	}
}

// ARCHIVE_ENABLED=false is the escape hatch for a deployment that has AWS
// credentials for some other purpose and does not want them used for this. The
// switch is checked before the credentials, so it has to win over a fully
// configured set of them rather than merely agreeing with an empty one.
func TestArchiveExplicitOffSwitchBeatsValidCredentials(t *testing.T) {
	enableArchiveEnv(t) // all three credentials present and valid
	t.Setenv("ARCHIVE_ENABLED", "false")

	a := NewArchive(&fakeAws{})

	if a.Enabled() {
		t.Fatal("ARCHIVE_ENABLED=false did not disable the archive")
	}

	// And it must refuse the same way as an unconfigured archive, so callers have
	// one condition to handle rather than two.
	f := &fakeAws{}
	if err := NewArchive(f).Put(context.Background(), "b", "o", strings.NewReader("x")); !errors.Is(err, ErrArchiveDisabled) {
		t.Fatalf("Put: want ErrArchiveDisabled, got %v", err)
	}
	if f.putKey != "" {
		t.Fatalf("an upload reached AWS while archiving was switched off: %q", f.putKey)
	}
}

// Bucket-name parity is the default layout: the object keeps its key and the S3
// bucket carries the MinIO bucket's name, so no index is needed to find it again.
func TestArchivePutUsesBucketParityByDefault(t *testing.T) {
	enableArchiveEnv(t)
	f := &fakeAws{}

	if err := NewArchive(f).Put(context.Background(), "photos", "2024/cat.jpg", strings.NewReader("bytes")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if f.putBucket != "photos" {
		t.Errorf("bucket: want photos, got %q", f.putBucket)
	}
	if f.putKey != "2024/cat.jpg" {
		t.Errorf("key: want 2024/cat.jpg, got %q", f.putKey)
	}
	if string(f.putBody) != "bytes" {
		t.Errorf("body: want bytes, got %q", f.putBody)
	}
}

// ARCHIVE_BUCKET collapses every MinIO bucket into one, distinguished by prefix.
// The prefix must be the bucket name so the mapping stays reversible.
func TestArchivePutUsesSingleBucketWithPrefix(t *testing.T) {
	enableArchiveEnv(t)
	t.Setenv("ARCHIVE_BUCKET", "cold-store")
	f := &fakeAws{}

	if err := NewArchive(f).Put(context.Background(), "photos", "2024/cat.jpg", strings.NewReader("bytes")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if f.putBucket != "cold-store" {
		t.Errorf("bucket: want cold-store, got %q", f.putBucket)
	}
	if f.putKey != "photos/2024/cat.jpg" {
		t.Errorf("key: want photos/2024/cat.jpg, got %q", f.putKey)
	}
}

// Open is what makes an aged-out object still serveable. Glacier Instant
// Retrieval answers a plain GET, so there is no restore step to model here.
func TestArchiveOpenReturnsContentAndSize(t *testing.T) {
	enableArchiveEnv(t)
	f := &fakeAws{getBody: []byte("hello world")}

	rc, size, err := NewArchive(f).Open(context.Background(), "photos", "cat.jpg")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "hello world" {
		t.Errorf("body: want %q, got %q", "hello world", got)
	}
	if size != int64(len("hello world")) {
		t.Errorf("size: want %d, got %d", len("hello world"), size)
	}
}

// S3 reports a missing object two different ways depending on the verb, and both
// have to reach the caller as ErrArchiveNotFound: the retention job branches on
// exactly that error to decide whether a local copy is safe to delete.
func TestArchiveTranslatesNotFound(t *testing.T) {
	enableArchiveEnv(t)

	t.Run("Stat maps NotFound", func(t *testing.T) {
		a := NewArchive(&fakeAws{headErr: &s3types.NotFound{}})
		if _, err := a.Stat(context.Background(), "b", "o"); !errors.Is(err, ErrArchiveNotFound) {
			t.Fatalf("want ErrArchiveNotFound, got %v", err)
		}
	})

	t.Run("Open maps NoSuchKey", func(t *testing.T) {
		a := NewArchive(&fakeAws{getErr: &s3types.NoSuchKey{}})
		if _, _, err := a.Open(context.Background(), "b", "o"); !errors.Is(err, ErrArchiveNotFound) {
			t.Fatalf("want ErrArchiveNotFound, got %v", err)
		}
	})
}

// A transport failure is not a missing object. Confusing the two would let the
// retention job read "S3 is down" as "nothing archived here", which is the
// harmless direction, but it would also hide an outage behind a routine warning.
func TestArchiveKeepsRealErrorsDistinctFromNotFound(t *testing.T) {
	enableArchiveEnv(t)
	boom := errors.New("connection reset")

	a := NewArchive(&fakeAws{headErr: boom})
	_, err := a.Stat(context.Background(), "b", "o")

	if errors.Is(err, ErrArchiveNotFound) {
		t.Fatal("a transport error was reported as a missing object")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("underlying error was lost: %v", err)
	}
}

// The promise the whole feature makes to callers: a URL handed out at upload
// time keeps working after the object has been aged out of MinIO. That holds
// only if writing and reading resolve the same (bucket, key) pair to the same
// archive location, since the URL carries nothing else. Both layouts are checked,
// because switching between them is precisely what would break it.
func TestArchiveReadResolvesToTheSameLocationAsWrite(t *testing.T) {
	const bucket, object = "sovtajyeri", "ihale/2026/144545/67f65bd1.jpg"

	layouts := []struct {
		name          string
		archiveBucket string
	}{
		{"bucket parity", ""},
		{"single bucket with prefix", "cold-store"},
	}

	for _, layout := range layouts {
		t.Run(layout.name, func(t *testing.T) {
			enableArchiveEnv(t)
			t.Setenv("ARCHIVE_BUCKET", layout.archiveBucket)

			f := &fakeAws{getBody: []byte("original bytes")}
			a := NewArchive(f)

			if err := a.Put(context.Background(), bucket, object, strings.NewReader("original bytes")); err != nil {
				t.Fatalf("Put: %v", err)
			}
			wroteTo := f.putBucket + "|" + f.putKey

			rc, _, err := a.Open(context.Background(), bucket, object)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer rc.Close()
			readFrom := f.putBucket + "|" + f.putKey

			if wroteTo != readFrom {
				t.Fatalf("archived to %q but read from %q: the upload URL would 404 once MinIO no longer has the object", wroteTo, readFrom)
			}
		})
	}
}

// Nothing creates the destination bucket. Bucket creation carries decisions that
// belong to whoever owns the AWS account (region, public access blocking,
// encryption), and a bucket of user uploads created with SDK defaults is a worse
// outcome than a clear error. Reachable is how a caller finds out once instead
// of once per object.
func TestArchiveReachableChecksTheResolvedDestination(t *testing.T) {
	t.Run("parity checks the same-named bucket", func(t *testing.T) {
		enableArchiveEnv(t)
		f := &fakeAws{}

		if err := NewArchive(f).Reachable(context.Background(), "photos"); err != nil {
			t.Fatalf("Reachable: %v", err)
		}
		if f.headBucket != "photos" {
			t.Errorf("checked bucket %q, want photos", f.headBucket)
		}
	})

	t.Run("single-bucket layout checks the archive bucket", func(t *testing.T) {
		enableArchiveEnv(t)
		t.Setenv("ARCHIVE_BUCKET", "cold-store")
		f := &fakeAws{}

		if err := NewArchive(f).Reachable(context.Background(), "photos"); err != nil {
			t.Fatalf("Reachable: %v", err)
		}
		if f.headBucket != "cold-store" {
			t.Errorf("checked bucket %q, want cold-store", f.headBucket)
		}
	})

	t.Run("a missing bucket is reported, not created", func(t *testing.T) {
		enableArchiveEnv(t)
		f := &fakeAws{headBucketErr: errors.New("NotFound")}

		err := NewArchive(f).Reachable(context.Background(), "photos")
		if err == nil {
			t.Fatal("expected an error for a missing destination bucket")
		}
		if !strings.Contains(err.Error(), "photos") {
			t.Errorf("error should name the destination, got %v", err)
		}
		if f.createdBucket != "" {
			t.Fatalf("the archive created bucket %q on its own", f.createdBucket)
		}
	})

	t.Run("disabled archive reports as such", func(t *testing.T) {
		t.Setenv("AWS_ACCESS_KEY_ID", "")
		t.Setenv("AWS_SECRET_ACCESS_KEY", "")
		t.Setenv("AWS_REGION", "")

		if err := NewArchive(&fakeAws{}).Reachable(context.Background(), "photos"); !errors.Is(err, ErrArchiveDisabled) {
			t.Fatalf("want ErrArchiveDisabled, got %v", err)
		}
	})
}

// ARCHIVE_ONLY_BUCKETS narrows which buckets are archived. It defaults to all,
// because a deployment that switches the archive on almost always wants its data
// covered, and an opt-in list would leave buckets silently unprotected until
// someone remembered to add them.
func TestArchiveScope(t *testing.T) {
	t.Run("defaults to every bucket", func(t *testing.T) {
		enableArchiveEnv(t)
		t.Setenv("ARCHIVE_ONLY_BUCKETS", "")

		a := NewArchive(&fakeAws{})
		for _, b := range []string{"photos", "documents", "anything"} {
			if !a.InScope(b) {
				t.Errorf("%s should be in scope by default", b)
			}
		}
	})

	t.Run("limits to the listed buckets", func(t *testing.T) {
		enableArchiveEnv(t)
		t.Setenv("ARCHIVE_ONLY_BUCKETS", "photos, documents ")

		a := NewArchive(&fakeAws{})
		if !a.InScope("photos") || !a.InScope("documents") {
			t.Error("listed buckets should be in scope, whitespace and all")
		}
		if a.InScope("videos") {
			t.Error("an unlisted bucket is in scope")
		}
	})

	t.Run("an out-of-scope write is refused before it reaches AWS", func(t *testing.T) {
		enableArchiveEnv(t)
		t.Setenv("ARCHIVE_ONLY_BUCKETS", "photos")

		f := &fakeAws{}
		err := NewArchive(f).Put(context.Background(), "videos", "clip.mp4", strings.NewReader("x"))

		if !errors.Is(err, ErrArchiveNotInScope) {
			t.Fatalf("want ErrArchiveNotInScope, got %v", err)
		}
		if f.putKey != "" {
			t.Fatalf("an out-of-scope object was uploaded as %q", f.putKey)
		}
	})

	// The important one. Scope gates writing, never reading: narrowing it must not
	// be able to break URLs for objects archived and evicted while the bucket was
	// still included.
	t.Run("reads are not scoped", func(t *testing.T) {
		enableArchiveEnv(t)
		t.Setenv("ARCHIVE_ONLY_BUCKETS", "photos")

		f := &fakeAws{getBody: []byte("still here"), headSize: 10}
		a := NewArchive(f)

		rc, _, err := a.Open(context.Background(), "videos", "clip.mp4")
		if err != nil {
			t.Fatalf("an out-of-scope bucket could not be read back: %v", err)
		}
		_ = rc.Close()

		if _, err := a.Stat(context.Background(), "videos", "clip.mp4"); err != nil {
			t.Fatalf("an out-of-scope bucket could not be stat'd: %v", err)
		}
	})

	// The scenario the read/write asymmetry exists for. A deployment archives two
	// buckets for a year, evicting local copies as it goes, then narrows the list
	// to one. Everything already archived from the dropped bucket has to keep
	// serving, because for a lot of it the archive is now the only copy.
	t.Run("narrowing the scope does not orphan what was already archived", func(t *testing.T) {
		enableArchiveEnv(t)

		// A year of archiving both buckets.
		t.Setenv("ARCHIVE_ONLY_BUCKETS", "sovtajyeri,dos")
		f := &fakeAws{getBody: []byte("archived last year"), headSize: 18}
		before := NewArchive(f)

		if !before.InScope("dos") {
			t.Fatal("dos should have been in scope")
		}
		if err := before.Put(context.Background(), "dos", "2025/invoice.pdf", strings.NewReader("archived last year")); err != nil {
			t.Fatalf("Put: %v", err)
		}
		archivedAt := f.putBucket + "|" + f.putKey

		// Someone drops dos from the list.
		t.Setenv("ARCHIVE_ONLY_BUCKETS", "sovtajyeri")
		after := NewArchive(f)

		if after.InScope("dos") {
			t.Fatal("dos should no longer be in scope")
		}

		// New writes stop, as intended.
		if err := after.Put(context.Background(), "dos", "2026/new.pdf", strings.NewReader("x")); !errors.Is(err, ErrArchiveNotInScope) {
			t.Fatalf("a write to an out-of-scope bucket was accepted: %v", err)
		}

		// Reads do not stop. This is the part that keeps URLs alive.
		rc, _, err := after.Open(context.Background(), "dos", "2025/invoice.pdf")
		if err != nil {
			t.Fatalf("an object archived before the change is no longer readable: %v", err)
		}
		defer rc.Close()

		readFrom := f.putBucket + "|" + f.putKey
		if readFrom != archivedAt {
			t.Fatalf("read resolved to %q but it was archived at %q", readFrom, archivedAt)
		}
	})

	t.Run("scope is irrelevant when the archive is off", func(t *testing.T) {
		t.Setenv("AWS_ACCESS_KEY_ID", "")
		t.Setenv("AWS_SECRET_ACCESS_KEY", "")
		t.Setenv("AWS_REGION", "")
		t.Setenv("ARCHIVE_ONLY_BUCKETS", "photos")

		if NewArchive(&fakeAws{}).InScope("photos") {
			t.Fatal("a listed bucket is in scope while the archive is disabled")
		}
	})
}

func TestArchiveStatReturnsSize(t *testing.T) {
	enableArchiveEnv(t)

	size, err := NewArchive(&fakeAws{headSize: 4096}).Stat(context.Background(), "b", "o")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if size != 4096 {
		t.Errorf("size: want 4096, got %d", size)
	}
}
