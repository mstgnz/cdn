package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/mstgnz/go-minio-cdn/service"
)

type AwsHandler interface {
	GlacierVaultList(c *fiber.Ctx) error
	BucketList(c *fiber.Ctx) error
}

type awsHandler struct {
	awsService service.AwsService
}

func NewAwsHandler(awsService service.AwsService) AwsHandler {
	return &awsHandler{awsService: awsService}
}

func (ac awsHandler) BucketList(c *fiber.Ctx) error {
	buckets, _ := ac.awsService.ListBuckets()
	return c.JSON(fiber.Map{
		"status": true,
		"result": buckets,
	})
}

func (ac awsHandler) GlacierVaultList(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"status": true,
		"result": ac.awsService.GlacierVaultList(),
	})
}
