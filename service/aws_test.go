package service

import (
	"os"
	"testing"
)

// TestNewAwsService is a construction smoke test. NewAwsService builds the AWS
// config/credentials without making any network call, so it must return a
// non-nil service and not panic, even when AWS is unconfigured.
func TestNewAwsService(t *testing.T) {
	if NewAwsService() == nil {
		t.Fatal("NewAwsService() returned nil")
	}
}

// TestAwsService_ListBuckets_Integration exercises a real (read-only) AWS call.
// It is skipped unless AWS_TEST_ENABLED=1 with valid credentials/region in the
// environment, so it never fails in CI or on machines without AWS access. The
// concrete awsService wraps the AWS SDK directly and cannot be mocked, so the
// live SDK path is the only meaningful coverage for these methods.
func TestAwsService_ListBuckets_Integration(t *testing.T) {
	if os.Getenv("AWS_TEST_ENABLED") != "1" {
		t.Skip("set AWS_TEST_ENABLED=1 with valid AWS credentials to run AWS integration tests")
	}
	svc := NewAwsService()
	if _, err := svc.ListBuckets(); err != nil {
		t.Fatalf("ListBuckets: %v", err)
	}
}
