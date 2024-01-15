package service

import (
	"io"
	"reflect"
	"strconv"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/glacier"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func TestNewAwsService(t *testing.T) {
	tests := []struct {
		want AwsService
	}{
		// TODO: Add test cases.
		{},
	}
	for i, tt := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if got := NewAwsService(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewAwsService() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_awsService_BucketExists(t *testing.T) {
	type fields struct {
		cfg aws.Config
	}
	type args struct {
		bucketName string
	}
	tests := []struct {
		fields fields
		args   args
		want   bool
	}{
		// TODO: Add test cases.
		{},
	}
	for i, tt := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			as := &awsService{
				cfg: tt.fields.cfg,
			}
			if got := as.BucketExists(tt.args.bucketName); got != tt.want {
				t.Errorf("BucketExists() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_awsService_DeleteObjects(t *testing.T) {
	type fields struct {
		cfg aws.Config
	}
	type args struct {
		bucketName string
		objectKeys []string
	}
	tests := []struct {
		fields  fields
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
		{},
	}
	for i, tt := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			as := &awsService{
				cfg: tt.fields.cfg,
			}
			if err := as.DeleteObjects(tt.args.bucketName, tt.args.objectKeys); (err != nil) != tt.wantErr {
				t.Errorf("DeleteObjects() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_awsService_GlacierUploadArchive(t *testing.T) {
	type fields struct {
		cfg aws.Config
	}
	type args struct {
		vaultName  string
		fileBuffer []byte
	}
	tests := []struct {
		fields  fields
		args    args
		want    *glacier.UploadArchiveOutput
		wantErr bool
	}{
		// TODO: Add test cases.
		{},
	}
	for i, tt := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			as := &awsService{
				cfg: tt.fields.cfg,
			}
			got, err := as.GlacierUploadArchive(tt.args.vaultName, tt.args.fileBuffer)
			if (err != nil) != tt.wantErr {
				t.Errorf("GlacierUploadArchive() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GlacierUploadArchive() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_awsService_GlacierVaultList(t *testing.T) {
	type fields struct {
		cfg aws.Config
	}
	tests := []struct {
		fields fields
		want   *glacier.ListVaultsOutput
	}{
		// TODO: Add test cases.
		{},
	}
	for i, tt := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			as := &awsService{
				cfg: tt.fields.cfg,
			}
			if got := as.GlacierVaultList(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GlacierVaultList() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_awsService_ListBuckets(t *testing.T) {
	type fields struct {
		cfg aws.Config
	}
	tests := []struct {
		fields  fields
		want    []types.Bucket
		wantErr bool
	}{
		// TODO: Add test cases.
		{},
	}
	for i, tt := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			as := &awsService{
				cfg: tt.fields.cfg,
			}
			got, err := as.ListBuckets()
			if (err != nil) != tt.wantErr {
				t.Errorf("ListBuckets() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ListBuckets() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_awsService_S3PutObject(t *testing.T) {
	type fields struct {
		cfg aws.Config
	}
	type args struct {
		bucketName string
		objectName string
		fileBuffer io.Reader
	}
	tests := []struct {
		fields  fields
		args    args
		want    *manager.UploadOutput
		wantErr bool
	}{
		// TODO: Add test cases.
		{},
	}
	for i, tt := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			as := &awsService{
				cfg: tt.fields.cfg,
			}
			got, err := as.S3PutObject(tt.args.bucketName, tt.args.objectName, tt.args.fileBuffer)
			if (err != nil) != tt.wantErr {
				t.Errorf("S3PutObject() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("S3PutObject() got = %v, want %v", got, tt.want)
			}
		})
	}
}
