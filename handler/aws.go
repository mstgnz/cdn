package handler

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/mstgnz/cdn/service"
)

type AwsHandler interface {
	GlacierVaultList(c *fiber.Ctx) error
	BucketList(c *fiber.Ctx) error
	BucketExists(c *fiber.Ctx) error
}

type awsHandler struct {
	awsService service.AwsService
}

func NewAwsHandler(awsService service.AwsService) AwsHandler {
	return &awsHandler{awsService: awsService}
}

func (a awsHandler) BucketExists(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")
	exists := a.awsService.BucketExists(bucketName)
	if !exists {
		return service.Response(c, fiber.StatusNotFound, false, "not found", strconv.FormatBool(exists))
	}
	return service.Response(c, fiber.StatusFound, true, "found", strconv.FormatBool(exists))
}

func (a awsHandler) BucketList(c *fiber.Ctx) error {
	buckets, err := a.awsService.ListBuckets()
	if err != nil {
		return service.Response(c, fiber.StatusOK, false, err.Error(), buckets)
	}
	return service.Response(c, fiber.StatusOK, true, "buckets", buckets)
}

func (a awsHandler) GlacierVaultList(c *fiber.Ctx) error {
	return service.Response(c, fiber.StatusOK, true, "glacier vault list", a.awsService.GlacierVaultList())
}
