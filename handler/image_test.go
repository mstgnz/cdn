package handler

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/minio/minio-go/v7"
	"github.com/mstgnz/cdn/service"
)

func TestNewImage(t *testing.T) {
	type args struct {
		minioService *minio.Client
		awsService   service.AwsService
	}
	tests := []struct {
		args args
		want Image
	}{
		// TODO: Add test cases.
		{},
	}
	for i, tt := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if got := NewImage(tt.args.minioService, tt.args.awsService); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewImage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_image_DeleteImage(t *testing.T) {
	type fields struct {
		minioClient *minio.Client
		awsService  service.AwsService
	}
	type args struct {
		c *fiber.Ctx
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
			i := image{
				minioClient: tt.fields.minioClient,
				awsService:  tt.fields.awsService,
			}
			if err := i.DeleteImage(tt.args.c); (err != nil) != tt.wantErr {
				t.Errorf("DeleteImage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_image_GetImage(t *testing.T) {
	type fields struct {
		minioClient *minio.Client
		awsService  service.AwsService
	}
	type args struct {
		c *fiber.Ctx
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
			i := image{
				minioClient: tt.fields.minioClient,
				awsService:  tt.fields.awsService,
			}
			if err := i.GetImage(tt.args.c); (err != nil) != tt.wantErr {
				t.Errorf("GetImage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_image_ResizeImage(t *testing.T) {
	type fields struct {
		minioClient *minio.Client
		awsService  service.AwsService
	}
	type args struct {
		c *fiber.Ctx
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
			i := image{
				minioClient: tt.fields.minioClient,
				awsService:  tt.fields.awsService,
			}
			if err := i.ResizeImage(tt.args.c); (err != nil) != tt.wantErr {
				t.Errorf("ResizeImage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_image_UploadImage(t *testing.T) {
	type fields struct {
		minioClient *minio.Client
		awsService  service.AwsService
	}
	type args struct {
		c *fiber.Ctx
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
			i := image{
				minioClient: tt.fields.minioClient,
				awsService:  tt.fields.awsService,
			}
			if err := i.UploadImage(tt.args.c); (err != nil) != tt.wantErr {
				t.Errorf("UploadImage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_image_UploadImageWithUrl(t *testing.T) {
	type fields struct {
		minioClient *minio.Client
		awsService  service.AwsService
	}
	type args struct {
		c *fiber.Ctx
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
			i := image{
				minioClient: tt.fields.minioClient,
				awsService:  tt.fields.awsService,
			}
			if err := i.UploadWithUrl(tt.args.c); (err != nil) != tt.wantErr {
				t.Errorf("UploadImageWithUrl() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
