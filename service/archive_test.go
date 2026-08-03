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
// credentials for some other purpose and does not want them used for this.
func TestArchiveExplicitOffSwitch(t *testing.T) {
	enableArchiveEnv(t)
	t.Setenv("ARCHIVE_ENABLED", "false")

	if NewArchive(&fakeAws{}).Enabled() {
		t.Fatal("ARCHIVE_ENABLED=false did not disable the archive")
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
