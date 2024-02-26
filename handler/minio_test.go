package handler

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/minio/minio-go/v7"
)

func TestNewMinioHandler(t *testing.T) {
	type args struct {
		minioClient *minio.Client
	}
	tests := []struct {
		args args
		want MinioHandler
	}{
		// TODO: Add test cases.
		{},
	}
	for i, tt := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if got := NewMinioHandler(tt.args.minioClient); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewMinioHandler() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_minioHandler_BucketExists(t *testing.T) {
	type fields struct {
		minioClient *minio.Client
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
			m := minioHandler{
				minioClient: tt.fields.minioClient,
			}
			if err := m.BucketExists(tt.args.c); (err != nil) != tt.wantErr {
				t.Errorf("BucketExists() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_minioHandler_BucketList(t *testing.T) {
	type fields struct {
		minioClient *minio.Client
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
			m := minioHandler{
				minioClient: tt.fields.minioClient,
			}
			if err := m.BucketList(tt.args.c); (err != nil) != tt.wantErr {
				t.Errorf("BucketList() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_minioHandler_CreateBucket(t *testing.T) {
	type fields struct {
		minioClient *minio.Client
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
			m := minioHandler{
				minioClient: tt.fields.minioClient,
			}
			if err := m.CreateBucket(tt.args.c); (err != nil) != tt.wantErr {
				t.Errorf("CreateBucket() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_minioHandler_RemoveBucket(t *testing.T) {
	type fields struct {
		minioClient *minio.Client
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
			m := minioHandler{
				minioClient: tt.fields.minioClient,
			}
			if err := m.RemoveBucket(tt.args.c); (err != nil) != tt.wantErr {
				t.Errorf("RemoveBucket() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
