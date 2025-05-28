package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/glacier"
	"github.com/aws/aws-sdk-go-v2/service/glacier/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/mstgnz/cdn/pkg/circuitbreaker"
	cnf "github.com/mstgnz/cdn/pkg/config"
)

// https://docs.aws.amazon.com/amazonglacier/latest/dev/introduction.html
// https://docs.aws.amazon.com/sdk-for-go/v1/developer-guide/using-glacier-with-go-sdk.html
// https://docs.aws.amazon.com/cli/latest/reference/glacier/index.html

type AwsService interface {
	GlacierVaultList() *glacier.ListVaultsOutput
	GlacierUploadArchive(vaultName string, fileBuffer []byte) (*glacier.UploadArchiveOutput, error)
	GlacierInitiateRetrieval(vaultName, archiveId string, retrievalType string) (*glacier.InitiateJobOutput, error)
	GlacierListJobs(vaultName string) (*glacier.ListJobsOutput, error)
	GlacierGetJobOutput(vaultName, jobId string) (*glacier.GetJobOutputOutput, error)
	GlacierDescribeJob(vaultName, jobId string) (*glacier.DescribeJobOutput, error)
	GlacierInventoryRetrieval(vaultName string) (*glacier.InitiateJobOutput, error)
	GlacierDownloadToMinio(vaultName, jobId, targetBucket, targetPath string) error
	GlacierDownloadToLocal(vaultName, jobId, localPath string) error
	S3PutObject(bucketName string, objectName string, fileBuffer io.Reader) (*manager.UploadOutput, error)
	ListBuckets() ([]s3types.Bucket, error)
	BucketExists(bucketName string) bool
	DeleteObjects(bucketName string, objectKeys []string) error
	IsConnected() bool
}

type awsService struct {
	cfg aws.Config
	cb  *circuitbreaker.CircuitBreaker
}

func NewAwsService() AwsService {
	cfg, _ := config.LoadDefaultConfig(context.TODO(), config.WithCredentialsProvider(
		credentials.NewStaticCredentialsProvider(cnf.GetEnvOrDefault("AWS_ACCESS_KEY_ID", ""), cnf.GetEnvOrDefault("AWS_SECRET_ACCESS_KEY", ""), "")))

	cb := circuitbreaker.NewCircuitBreaker(
		"aws-service",
		5,              // 5 failures to open
		3,              // 3 successes to close
		10*time.Second, // 10 second timeout
		100,            // max 100 concurrent requests
	)

	return &awsService{
		cfg: cfg,
		cb:  cb,
	}
}

func (as *awsService) S3PutObject(bucketName string, objectName string, fileBuffer io.Reader) (*manager.UploadOutput, error) {
	client := s3.NewFromConfig(as.cfg)
	uploader := manager.NewUploader(client)
	return uploader.Upload(context.TODO(), &s3.PutObjectInput{
		Bucket:       aws.String(bucketName),
		Key:          aws.String(objectName),
		Body:         fileBuffer,
		StorageClass: s3types.StorageClassGlacier,
	})
}

func (as *awsService) ListBuckets() ([]s3types.Bucket, error) {
	client := s3.NewFromConfig(as.cfg)
	result, err := client.ListBuckets(context.TODO(), &s3.ListBucketsInput{})
	var buckets []s3types.Bucket
	if err == nil {
		buckets = result.Buckets
	}
	return buckets, err
}

func (as *awsService) BucketExists(bucketName string) bool {
	buckets, _ := as.ListBuckets()
	for _, v := range buckets {
		if *v.Name == bucketName {
			return true
		}
	}
	return false
}

func (as *awsService) DeleteObjects(bucketName string, objectKeys []string) error {
	client := s3.NewFromConfig(as.cfg)
	var objectIds []s3types.ObjectIdentifier
	for _, key := range objectKeys {
		objectIds = append(objectIds, s3types.ObjectIdentifier{Key: aws.String(key)})
	}
	_, err := client.DeleteObjects(context.TODO(), &s3.DeleteObjectsInput{
		Bucket: aws.String(bucketName),
		Delete: &s3types.Delete{Objects: objectIds},
	})
	return err
}

func (as *awsService) GlacierVaultList() *glacier.ListVaultsOutput {
	glacierCls := glacier.NewFromConfig(as.cfg)
	result, _ := glacierCls.ListVaults(context.Background(), &glacier.ListVaultsInput{})

	/*for _, vault := range result.VaultList {
		fmt.Println(*vault.VaultName)
	}*/

	return result
}

func (as *awsService) GlacierUploadArchive(vaultName string, fileBuffer []byte) (*glacier.UploadArchiveOutput, error) {
	glacierCls := glacier.NewFromConfig(as.cfg)
	return glacierCls.UploadArchive(context.Background(), &glacier.UploadArchiveInput{
		VaultName: &vaultName,
		Body:      bytes.NewReader(fileBuffer),
	})
}

func (a *awsService) IsConnected() bool {
	client := s3.NewFromConfig(a.cfg)
	_, err := client.ListBuckets(context.TODO(), &s3.ListBucketsInput{})
	return err == nil
}

func (as *awsService) GlacierInitiateRetrieval(vaultName, archiveId string, retrievalType string) (*glacier.InitiateJobOutput, error) {
	glacierCls := glacier.NewFromConfig(as.cfg)

	// Default to standard retrieval if not specified
	if retrievalType == "" {
		retrievalType = "Standard"
	}

	jobParams := &glacier.InitiateJobInput{
		VaultName: &vaultName,
		JobParameters: &types.JobParameters{
			Type:        aws.String("archive-retrieval"),
			ArchiveId:   &archiveId,
			Tier:        &retrievalType, // Standard, Bulk, or Expedited
			Description: aws.String(fmt.Sprintf("Archive retrieval for %s", archiveId)),
		},
	}

	return glacierCls.InitiateJob(context.Background(), jobParams)
}

func (as *awsService) GlacierListJobs(vaultName string) (*glacier.ListJobsOutput, error) {
	glacierCls := glacier.NewFromConfig(as.cfg)
	return glacierCls.ListJobs(context.Background(), &glacier.ListJobsInput{
		VaultName: &vaultName,
	})
}

func (as *awsService) GlacierGetJobOutput(vaultName, jobId string) (*glacier.GetJobOutputOutput, error) {
	glacierCls := glacier.NewFromConfig(as.cfg)
	return glacierCls.GetJobOutput(context.Background(), &glacier.GetJobOutputInput{
		VaultName: &vaultName,
		JobId:     &jobId,
	})
}

func (as *awsService) GlacierDescribeJob(vaultName, jobId string) (*glacier.DescribeJobOutput, error) {
	glacierCls := glacier.NewFromConfig(as.cfg)
	return glacierCls.DescribeJob(context.Background(), &glacier.DescribeJobInput{
		VaultName: &vaultName,
		JobId:     &jobId,
	})
}

func (as *awsService) GlacierInventoryRetrieval(vaultName string) (*glacier.InitiateJobOutput, error) {
	glacierCls := glacier.NewFromConfig(as.cfg)

	jobParams := &glacier.InitiateJobInput{
		VaultName: &vaultName,
		JobParameters: &types.JobParameters{
			Type:        aws.String("inventory-retrieval"),
			Description: aws.String(fmt.Sprintf("Inventory retrieval for vault %s", vaultName)),
			Format:      aws.String("JSON"),
		},
	}

	return glacierCls.InitiateJob(context.Background(), jobParams)
}

// GlacierDownloadToMinio downloads a completed Glacier job directly to MinIO
func (as *awsService) GlacierDownloadToMinio(vaultName, jobId, targetBucket, targetPath string) error {
	glacierCls := glacier.NewFromConfig(as.cfg)

	// Get the archive data
	result, err := glacierCls.GetJobOutput(context.Background(), &glacier.GetJobOutputInput{
		VaultName: &vaultName,
		JobId:     &jobId,
	})
	if err != nil {
		return fmt.Errorf("failed to get job output: %w", err)
	}
	defer result.Body.Close()

	// Upload to MinIO via S3 API
	s3Client := s3.NewFromConfig(as.cfg)
	uploader := manager.NewUploader(s3Client)

	_, err = uploader.Upload(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(targetBucket),
		Key:    aws.String(targetPath),
		Body:   result.Body,
	})

	return err
}

// GlacierDownloadToLocal downloads a completed Glacier job to local file system
func (as *awsService) GlacierDownloadToLocal(vaultName, jobId, localPath string) error {
	glacierCls := glacier.NewFromConfig(as.cfg)

	// Get the archive data
	result, err := glacierCls.GetJobOutput(context.Background(), &glacier.GetJobOutputInput{
		VaultName: &vaultName,
		JobId:     &jobId,
	})
	if err != nil {
		return fmt.Errorf("failed to get job output: %w", err)
	}
	defer result.Body.Close()

	// Create local file
	file, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %w", err)
	}
	defer file.Close()

	// Copy data to local file
	_, err = io.Copy(file, result.Body)
	if err != nil {
		return fmt.Errorf("failed to write to local file: %w", err)
	}

	return nil
}
