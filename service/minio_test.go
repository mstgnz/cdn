package service

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/minio/minio-go/v7"
)

func TestMinioClient(t *testing.T) {
	tests := []struct {
		want *minio.Client
	}{
		// TODO: Add test cases.
		{},
	}
	for i, tt := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if got := MinioClient(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MinioClient() = %v, want %v", got, tt.want)
			}
		})
	}
}
