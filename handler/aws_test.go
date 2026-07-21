package handler

import (
	"bytes"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/glacier"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/gofiber/fiber/v2"
	"github.com/mstgnz/cdn/service"
)

// mockAwsService is a test double for service.AwsService. Only the behaviours
// the handler tests assert on are configurable; everything else returns zero
// values, which is enough because the validation paths short-circuit first.
type mockAwsService struct {
	bucketExists   bool
	listBuckets    []s3types.Bucket
	listBucketsErr error
	vaultList      *glacier.ListVaultsOutput
}

var _ service.AwsService = (*mockAwsService)(nil)

func (m *mockAwsService) BucketExists(string) bool { return m.bucketExists }
func (m *mockAwsService) ListBuckets() ([]s3types.Bucket, error) {
	return m.listBuckets, m.listBucketsErr
}
func (m *mockAwsService) GlacierVaultList() *glacier.ListVaultsOutput { return m.vaultList }
func (m *mockAwsService) IsConnected() bool                           { return true }

func (m *mockAwsService) GlacierUploadArchive(string, []byte) (*glacier.UploadArchiveOutput, error) {
	return nil, nil
}
func (m *mockAwsService) GlacierInitiateRetrieval(string, string, string) (*glacier.InitiateJobOutput, error) {
	return nil, nil
}
func (m *mockAwsService) GlacierListJobs(string) (*glacier.ListJobsOutput, error) { return nil, nil }
func (m *mockAwsService) GlacierGetJobOutput(string, string) (*glacier.GetJobOutputOutput, error) {
	return nil, nil
}
func (m *mockAwsService) GlacierDescribeJob(string, string) (*glacier.DescribeJobOutput, error) {
	return nil, nil
}
func (m *mockAwsService) GlacierInventoryRetrieval(string) (*glacier.InitiateJobOutput, error) {
	return nil, nil
}
func (m *mockAwsService) GlacierDownloadToMinio(string, string, string, string) error { return nil }
func (m *mockAwsService) GlacierDownloadToLocal(string, string, string) error         { return nil }
func (m *mockAwsService) S3PutObject(string, string, io.Reader) (*manager.UploadOutput, error) {
	return nil, nil
}
func (m *mockAwsService) DeleteObjects(string, []string) error { return nil }

func newAwsApp(mock service.AwsService) *fiber.App {
	h := NewAwsHandler(mock)
	app := fiber.New()
	app.Get("/aws/:bucket/exists", h.BucketExists)
	app.Get("/aws/bucket-list", h.BucketList)
	app.Post("/aws/glacier/:vault/jobs/:jobId/async-download", h.GlacierInitiateAsyncDownload)
	app.Get("/aws/glacier/downloads/:downloadJobId/status", h.GlacierCheckDownloadStatus)
	app.Get("/aws/glacier/:vault/jobs/:jobId/status", h.GlacierJobStatus)
	return app
}

// TestAwsHandler_JobStatus_NilResultNoPanic guards the nil-deref hardening:
// the mock returns (nil, nil) from GlacierDescribeJob, so the handler must
// respond 500 gracefully instead of panicking on a nil pointer deref.
func TestAwsHandler_JobStatus_NilResultNoPanic(t *testing.T) {
	app := newAwsApp(&mockAwsService{})
	resp := doReq(t, app, "GET", "/aws/glacier/myvault/jobs/job123/status")
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (no panic on nil job status)", resp.StatusCode)
	}
}

func TestAwsHandler_BucketExists(t *testing.T) {
	t.Run("exists", func(t *testing.T) {
		app := newAwsApp(&mockAwsService{bucketExists: true})
		resp := doReq(t, app, "GET", "/aws/mybucket/exists")
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if !decodeBody(t, resp).Success {
			t.Fatal("expected success=true")
		}
	})

	t.Run("missing", func(t *testing.T) {
		app := newAwsApp(&mockAwsService{bucketExists: false})
		resp := doReq(t, app, "GET", "/aws/mybucket/exists")
		if resp.StatusCode != fiber.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
		if decodeBody(t, resp).Success {
			t.Fatal("expected success=false")
		}
	})
}

func TestAwsHandler_BucketList(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		name := "b1"
		app := newAwsApp(&mockAwsService{listBuckets: []s3types.Bucket{{Name: &name}}})
		resp := doReq(t, app, "GET", "/aws/bucket-list")
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if !decodeBody(t, resp).Success {
			t.Fatal("expected success=true")
		}
	})

	t.Run("error reported as success=false", func(t *testing.T) {
		app := newAwsApp(&mockAwsService{listBucketsErr: io.ErrUnexpectedEOF})
		resp := doReq(t, app, "GET", "/aws/bucket-list")
		if decodeBody(t, resp).Success {
			t.Fatal("expected success=false on list error")
		}
	})
}

func TestAwsHandler_AsyncDownloadValidation(t *testing.T) {
	app := newAwsApp(&mockAwsService{})
	target := "/aws/glacier/myvault/jobs/job123/async-download"

	cases := []struct {
		name string
		body string
	}{
		{"invalid json", "{not-json"},
		{"missing target path", `{"type":"local","targetPath":""}`},
		{"invalid type", `{"type":"ftp","targetPath":"out.bin"}`},
		{"minio without bucket", `{"type":"minio","targetPath":"out.bin"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", target, bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req, -1)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != fiber.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
}

func TestAwsHandler_CheckDownloadStatus_NotFound(t *testing.T) {
	app := newAwsApp(&mockAwsService{})
	resp := doReq(t, app, "GET", "/aws/glacier/downloads/does-not-exist/status")
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestResolveLocalDownloadPath guards the Glacier path-traversal fix. Traversal
// is neutralized by containment: "../" segments are collapsed against a virtual
// root so the result can never escape base. Absolute/empty targets are rejected.
func TestResolveLocalDownloadPath(t *testing.T) {
	const base = "/tmp/glacier_downloads"
	cases := []struct {
		name    string
		target  string
		want    string
		wantErr bool
	}{
		{"absolute", "/etc/passwd", "", true},
		{"empty", "", "", true},
		{"traversal contained", "../../etc/passwd", base + "/etc/passwd", false},
		{"nested traversal contained", "a/../../x", base + "/x", false},
		{"valid nested", "a/b/c.bin", base + "/a/b/c.bin", false},
		{"valid inner dotdot", "a/../b.bin", base + "/b.bin", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveLocalDownloadPath(base, tc.target)
			if (err != nil) != tc.wantErr {
				t.Fatalf("resolveLocalDownloadPath(%q) err=%v, wantErr=%v", tc.target, err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if got != tc.want {
				t.Fatalf("resolveLocalDownloadPath(%q) = %q, want %q", tc.target, got, tc.want)
			}
			// Security invariant: the result must always stay within base.
			if got != base && !strings.HasPrefix(got, base+"/") {
				t.Fatalf("resolveLocalDownloadPath(%q) escaped base: %q", tc.target, got)
			}
		})
	}
}
