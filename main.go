package main

import (
	"log"

	"GominioCdn/controller"
	"GominioCdn/service"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/favicon"
)

var (
	awsService      = service.MyAwsService()
	minioClient     = service.MinioClient()
	minioController = controller.Image(minioClient, awsService)
	awsController   = controller.MyAwsController(awsService)
)

func main() {

	app := fiber.New()

	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "*",
	}))

	app.Use(favicon.New(favicon.Config{
		File: "./favicon.png",
	}))

	// Aws
	app.Get("/aws/get-vault-list", awsController.GetVaultList)

	// Minio
	app.Get("/:bucket/w::width/h::height/*", minioController.GetImageWidthHeight)
	app.Get("/:bucket/w::width/*", minioController.GetImageWidth)
	app.Get("/:bucket/h::height/*", minioController.GetImageHeight)
	app.Get("/:bucket/*", minioController.GetImage)

	app.Delete("/:bucket/*", minioController.DeleteImage)

	app.Post("/upload", minioController.UploadImage)

	// Index
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendFile("./index.html")
	})

	log.Fatal(app.Listen(":9090"))

}
