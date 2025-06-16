package handler

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/minio/minio-go/v7"
	"github.com/mstgnz/cdn/service"
)

type MinioHandler interface {
	BucketList(c *fiber.Ctx) error
	BucketExists(c *fiber.Ctx) error
	CreateBucket(c *fiber.Ctx) error
	RemoveBucket(c *fiber.Ctx) error
}

type minioHandler struct {
	minioClient *minio.Client
}

func NewMinioHandler(minioClient *minio.Client) MinioHandler {
	return &minioHandler{minioClient: minioClient}
}

func (m minioHandler) BucketList(c *fiber.Ctx) error {
	buckets, err := m.minioClient.ListBuckets(context.Background())
	if err != nil {
		return service.Response(c, fiber.StatusOK, false, err.Error(), buckets)
	}
	return service.Response(c, fiber.StatusOK, true, "buckets listed", buckets)
}

func (m minioHandler) BucketExists(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")
	exists, err := m.minioClient.BucketExists(context.Background(), bucketName)
	if err != nil {
		return service.Response(c, fiber.StatusNotFound, false, err.Error(), nil)
	}
	if !exists {
		return service.Response(c, fiber.StatusNotFound, false, "bucket not found", nil)
	}
	return service.Response(c, fiber.StatusOK, true, "bucket exists", nil)
}

func (m minioHandler) CreateBucket(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")
	err := m.minioClient.MakeBucket(context.Background(), bucketName, minio.MakeBucketOptions{})
	if err != nil {
		return service.Response(c, fiber.StatusOK, false, err.Error(), bucketName)
	}
	return service.Response(c, fiber.StatusCreated, true, "bucket created", bucketName)
}

func (m minioHandler) RemoveBucket(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")
	err := m.minioClient.RemoveBucket(context.Background(), bucketName)
	if err != nil {
		return service.Response(c, fiber.StatusOK, false, err.Error(), bucketName)
	}
	return service.Response(c, fiber.StatusOK, true, "bucket deleted", bucketName)
}
