package handler

import (
	"context"
	"net/http"

	"github.com/minio/minio-go/v7"
	"github.com/mstgnz/cdn/pkg/httpx"
	"github.com/mstgnz/cdn/service"
)

type MinioHandler interface {
	BucketList(w http.ResponseWriter, r *http.Request) error
	BucketExists(w http.ResponseWriter, r *http.Request) error
	CreateBucket(w http.ResponseWriter, r *http.Request) error
	RemoveBucket(w http.ResponseWriter, r *http.Request) error
}

type minioHandler struct {
	minioClient *minio.Client
}

func NewMinioHandler(minioClient *minio.Client) MinioHandler {
	return &minioHandler{minioClient: minioClient}
}

func (m minioHandler) BucketList(w http.ResponseWriter, r *http.Request) error {
	buckets, err := m.minioClient.ListBuckets(context.Background())
	if err != nil {
		return service.Response(w, http.StatusOK, false, err.Error(), buckets)
	}
	return service.Response(w, http.StatusOK, true, "buckets listed", buckets)
}

func (m minioHandler) BucketExists(w http.ResponseWriter, r *http.Request) error {
	bucketName := httpx.Param(r, "bucket")
	exists, err := m.minioClient.BucketExists(context.Background(), bucketName)
	if err != nil {
		return service.Response(w, http.StatusNotFound, false, err.Error(), nil)
	}
	if !exists {
		return service.Response(w, http.StatusNotFound, false, "bucket not found", nil)
	}
	return service.Response(w, http.StatusOK, true, "bucket exists", nil)
}

func (m minioHandler) CreateBucket(w http.ResponseWriter, r *http.Request) error {
	bucketName := httpx.Param(r, "bucket")
	err := m.minioClient.MakeBucket(context.Background(), bucketName, minio.MakeBucketOptions{})
	if err != nil {
		return service.Response(w, http.StatusOK, false, err.Error(), bucketName)
	}
	return service.Response(w, http.StatusCreated, true, "bucket created", bucketName)
}

func (m minioHandler) RemoveBucket(w http.ResponseWriter, r *http.Request) error {
	bucketName := httpx.Param(r, "bucket")
	err := m.minioClient.RemoveBucket(context.Background(), bucketName)
	if err != nil {
		return service.Response(w, http.StatusOK, false, err.Error(), bucketName)
	}
	return service.Response(w, http.StatusOK, true, "bucket deleted", bucketName)
}
