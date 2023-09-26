package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/favicon"
	"github.com/mstgnz/go-minio-cdn/handler"
	"github.com/mstgnz/go-minio-cdn/service"
)

var (
	awsService   = service.NewAwsService()
	minioClient  = service.MinioClient()
	imageHandler = handler.NewImage(minioClient, awsService)
	awsHandler   = handler.NewAwsHandler(awsService)
)

func main() {

	app := fiber.New()

	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "*",
	}))

	app.Static("/", "./public")

	app.Use(favicon.New(favicon.Config{
		File: "./public/favicon.png",
	}))

	// Aws
	app.Get("/aws/bucket-list", awsHandler.BucketList)
	app.Get("/aws/get-vault-list", awsHandler.GlacierVaultList)

	// Minio
	app.Get("/:bucket/*", imageHandler.GetImage)

	app.Delete("delete", imageHandler.DeleteImage)
	app.Delete("delete-with-aws", imageHandler.DeleteImageWithAws)

	app.Post("/upload", imageHandler.UploadImage)
	app.Post("/upload-with-aws", imageHandler.UploadImageWithAws)
	app.Post("/upload-url", imageHandler.UploadImageWithUrl)

	app.Post("/resize", imageHandler.ResizeImage)

	// Index
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendFile("index.html")
	})

	log.Fatal(app.Listen(":9090"))

}
