package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/favicon"
	"github.com/joho/godotenv"
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

	err := godotenv.Load(".env")
	if err != nil {
		log.Fatal("Error loading .env file, must be at project root")
	}

	app := fiber.New(fiber.Config{
		BodyLimit: 25 * 1024 * 2014,
	})

	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "*",
	}))

	app.Static("/", "./public")

	app.Use(favicon.New(favicon.Config{
		File: "./public/favicon.png",
	}))

	disableDelete := service.GetBool("DISABLE_DELETE")
	disableUpload := service.GetBool("DISABLE_UPLOAD")
	disableGet := service.GetBool("DISABLE_GET")

	// Aws
	app.Get("/aws/bucket-list", awsHandler.BucketList)
	app.Get("/aws/get-vault-list", awsHandler.GlacierVaultList)

	// Minio
	if !disableGet {
		app.Get("/:bucket/*", imageHandler.GetImage)
	}

	if !disableDelete {
		app.Delete("delete", imageHandler.DeleteImage)
		app.Delete("delete-with-aws", imageHandler.DeleteImageWithAws)
	}

	if !disableUpload {
		app.Post("/upload", imageHandler.UploadImage)
		app.Post("/upload-with-aws", imageHandler.UploadImageWithAws)
		app.Post("/upload-url", imageHandler.UploadImageWithUrl)
	}

	app.Post("/resize", imageHandler.ResizeImage)

	// Index
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendFile("index.html")
	})

	log.Fatal(app.Listen(":9090"))

}
