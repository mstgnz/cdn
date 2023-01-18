package controller

import (
	"GominioCdn/service"
	"github.com/gofiber/fiber/v2"
)

type IAwsController interface {
	GlacierVaultList(c *fiber.Ctx) error
	BucketList(c *fiber.Ctx) error
}

type myAwsController struct {
	awsService service.IAwsService
}

func MyAwsController(awsService service.IAwsService) IAwsController {
	return &myAwsController{awsService: awsService}
}

func (ac myAwsController) BucketList(c *fiber.Ctx) error {
	buckets, _ := ac.awsService.ListBuckets()
	return c.JSON(fiber.Map{
		"status": true,
		"result": buckets,
	})
}

func (ac myAwsController) GlacierVaultList(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"status": true,
		"result": ac.awsService.GlacierVaultList(),
	})
}
