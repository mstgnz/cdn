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
	app.Get("/aws/bucket-list", awsController.BucketList)
	app.Get("/aws/get-vault-list", awsController.GlacierVaultList)

	// Minio
	app.Get("/:bucket/*", minioController.GetImage)

	app.Delete("delete", minioController.DeleteImage)
	app.Delete("delete-with-aws", minioController.DeleteImageWithAws)

	app.Post("/upload", minioController.UploadImage)
	app.Post("/upload-with-aws", minioController.UploadImageWithAws)
	app.Post("/upload-url", minioController.UploadImageWithUrl)

	app.Post("/resize", minioController.ResizeImage)

	// Index
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendFile("./index.html")
	})

	log.Fatal(app.Listen(":9090"))

}
