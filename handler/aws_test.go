package handler

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/mstgnz/go-minio-cdn/service"
)

func TestNewAwsHandler(t *testing.T) {
	type args struct {
		awsService service.AwsService
	}
	tests := []struct {
		args args
		want AwsHandler
	}{
		// TODO: Add test cases.
		{},
	}
	for i, tt := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if got := NewAwsHandler(tt.args.awsService); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewAwsHandler() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_awsHandler_BucketList(t *testing.T) {
	type fields struct {
		awsService service.AwsService
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
			ac := awsHandler{
				awsService: tt.fields.awsService,
			}
			if err := ac.BucketList(tt.args.c); (err != nil) != tt.wantErr {
				t.Errorf("BucketList() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_awsHandler_GlacierVaultList(t *testing.T) {
	type fields struct {
		awsService service.AwsService
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
			ac := awsHandler{
				awsService: tt.fields.awsService,
			}
			if err := ac.GlacierVaultList(tt.args.c); (err != nil) != tt.wantErr {
				t.Errorf("GlacierVaultList() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
