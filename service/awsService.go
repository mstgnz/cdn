package service

import (
	"bytes"
	"context"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/glacier"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// https://docs.aws.amazon.com/amazonglacier/latest/dev/introduction.html
// https://docs.aws.amazon.com/sdk-for-go/v1/developer-guide/using-glacier-with-go-sdk.html
// https://docs.aws.amazon.com/cli/latest/reference/glacier/index.html

type IAwsService interface {
	GlacierVaultList() *glacier.ListVaultsOutput
	GlacierUploadArchive(vaultName string, fileBuffer []byte) (*glacier.UploadArchiveOutput, error)
	S3PutObject(bucketName string, objectName string, fileBuffer io.Reader) (*manager.UploadOutput, error)
}

type myAwsService struct {
	cfg aws.Config
}

func MyAwsService() IAwsService {
	cfg, _ := config.LoadDefaultConfig(context.TODO(), config.WithCredentialsProvider(
		credentials.NewStaticCredentialsProvider(GetEnv("AWS_ACCESS_KEY_ID"), GetEnv("AWS_SECRET_ACCESS_KEY"), "")))
	return &myAwsService{cfg: cfg}
}

func (as myAwsService) S3PutObject(bucketName string, objectName string, fileBuffer io.Reader) (*manager.UploadOutput, error) {
	client := s3.NewFromConfig(as.cfg)
	uploader := manager.NewUploader(client)
	return uploader.Upload(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectName),
		Body:   fileBuffer,
		StorageClass: types.StorageClassGlacier,
	})
}

func (as myAwsService) GlacierVaultList() *glacier.ListVaultsOutput {
	glacierCls := glacier.NewFromConfig(as.cfg)
	result, _ := glacierCls.ListVaults(context.Background(), &glacier.ListVaultsInput{})

	/*for _, vault := range result.VaultList {
		fmt.Println(*vault.VaultName)
	}*/

	return result
}

func (as myAwsService) GlacierUploadArchive(vaultName string, fileBuffer []byte) (*glacier.UploadArchiveOutput, error) {
	glacierCls := glacier.NewFromConfig(as.cfg)
	return glacierCls.UploadArchive(context.Background(), &glacier.UploadArchiveInput{
		VaultName: &vaultName,
		Body:      bytes.NewReader(fileBuffer),
	})
}
