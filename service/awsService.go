package service

import (
	"bytes"
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/glacier"
)

type IAwsService interface {
	GlacierVaultList() *glacier.ListVaultsOutput
	UploadArchive(vaultName string, fileBuffer []byte) (*glacier.UploadArchiveOutput, error)
}

type myAwsService struct {
	cfg     aws.Config
	glacier glacier.Client
}

func MyAwsService() IAwsService {
	cfg, _ := config.LoadDefaultConfig(context.TODO(), config.WithCredentialsProvider(
		credentials.NewStaticCredentialsProvider(GetEnv("AWS_ACCESS_KEY_ID"), GetEnv("AWS_SECRET_ACCESS_KEY"), "")))
	glacier := glacier.NewFromConfig(cfg)
	return &myAwsService{cfg: cfg, glacier: *glacier}
}

func (as myAwsService) GlacierVaultList() *glacier.ListVaultsOutput {

	result, _ := as.glacier.ListVaults(context.Background(), &glacier.ListVaultsInput{})

	/*for _, vault := range result.VaultList {
		fmt.Println(*vault.VaultName)
	}*/

	return result
}

func (as myAwsService) UploadArchive(vaultName string, fileBuffer []byte) (*glacier.UploadArchiveOutput, error) {
	return as.glacier.UploadArchive(context.Background(), &glacier.UploadArchiveInput{
		VaultName: &vaultName,
		Body:      bytes.NewReader(fileBuffer),
	})
}
