package controller

import (
	"GominioCdn/service"
	"github.com/gofiber/fiber/v2"
)

type IAwsController interface {
	GetVaultList(c *fiber.Ctx) error
}

type myAwsController struct {
	awsService service.IAwsService
}

func MyAwsController(awsService service.IAwsService) IAwsController {
	return &myAwsController{awsService: awsService}
}

func (ac myAwsController) GetVaultList(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"error":  false,
		"result": ac.awsService.GlacierVaultList(),
	})
}
