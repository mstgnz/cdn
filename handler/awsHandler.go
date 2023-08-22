package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/mstgnz/go-minio-cdn/service"
)

type IAwsHandler interface {
	GlacierVaultList(c *fiber.Ctx) error
	BucketList(c *fiber.Ctx) error
}

type myAwsHandler struct {
	awsService service.IAwsService
}

func MyAwsHandler(awsService service.IAwsService) IAwsHandler {
	return &myAwsHandler{awsService: awsService}
}

func (ac myAwsHandler) BucketList(c *fiber.Ctx) error {
	buckets, _ := ac.awsService.ListBuckets()
	return c.JSON(fiber.Map{
		"status": true,
		"result": buckets,
	})
}

func (ac myAwsHandler) GlacierVaultList(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"status": true,
		"result": ac.awsService.GlacierVaultList(),
	})
}
