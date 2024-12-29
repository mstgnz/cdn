package service

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/glacier"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/mstgnz/cdn/pkg/circuitbreaker"
	cnf "github.com/mstgnz/cdn/pkg/config"
)

// https://docs.aws.amazon.com/amazonglacier/latest/dev/introduction.html
// https://docs.aws.amazon.com/sdk-for-go/v1/developer-guide/using-glacier-with-go-sdk.html
// https://docs.aws.amazon.com/cli/latest/reference/glacier/index.html

type AwsService interface {
	GlacierVaultList() *glacier.ListVaultsOutput
	GlacierUploadArchive(vaultName string, fileBuffer []byte) (*glacier.UploadArchiveOutput, error)
	S3PutObject(bucketName string, objectName string, fileBuffer io.Reader) (*manager.UploadOutput, error)
	ListBuckets() ([]types.Bucket, error)
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
		StorageClass: types.StorageClassGlacier,
	})
}

func (as *awsService) ListBuckets() ([]types.Bucket, error) {
	client := s3.NewFromConfig(as.cfg)
	result, err := client.ListBuckets(context.TODO(), &s3.ListBucketsInput{})
	var buckets []types.Bucket
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
	var objectIds []types.ObjectIdentifier
	for _, key := range objectKeys {
		objectIds = append(objectIds, types.ObjectIdentifier{Key: aws.String(key)})
	}
	_, err := client.DeleteObjects(context.TODO(), &s3.DeleteObjectsInput{
		Bucket: aws.String(bucketName),
		Delete: &types.Delete{Objects: objectIds},
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
