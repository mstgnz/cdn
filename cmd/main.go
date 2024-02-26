package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/favicon"
	"github.com/joho/godotenv"
	"github.com/minio/minio-go/v7"
	"github.com/mstgnz/cdn/handler"
	"github.com/mstgnz/cdn/service"
)

var (
	awsService   service.AwsService
	minioClient  *minio.Client
	imageHandler handler.Image
	awsHandler   handler.AwsHandler
	minioHandler handler.MinioHandler
)

func main() {

	err := godotenv.Load(".env")
	if err != nil {
		log.Fatal("Error loading .env file, must be at project root")
	}

	awsService = service.NewAwsService()
	minioClient = service.MinioClient()
	imageHandler = handler.NewImage(minioClient, awsService)
	awsHandler = handler.NewAwsHandler(awsService)
	minioHandler = handler.NewMinioHandler(minioClient)

	app := fiber.New(fiber.Config{
		BodyLimit: 25 * 1024 * 2014,
	})

	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "*",
	}))

	app.Use(favicon.New(favicon.Config{
		File: "./public/favicon.png",
	}))

	disableDelete := service.GetBool("DISABLE_DELETE")
	disableUpload := service.GetBool("DISABLE_UPLOAD")
	disableGet := service.GetBool("DISABLE_GET")

	// Swagger
	app.Get("/swagger", func(c *fiber.Ctx) error {
		return c.SendFile("./public/swagger.html")
	})
	app.Get("/swagger.yaml", func(c *fiber.Ctx) error {
		return c.SendFile("./public/swagger.yaml")
	})

	// Aws
	aws := app.Group("/aws", AuthMiddleware)
	aws.Get("/bucket-list", awsHandler.BucketList)
	aws.Get("/:bucket/exists", awsHandler.BucketExists)
	aws.Get("/vault-list", awsHandler.GlacierVaultList)

	// Minio
	io := app.Group("/minio", AuthMiddleware)
	io.Get("/bucket-list", minioHandler.BucketList)
	io.Get("/:bucket/exists", minioHandler.BucketExists)
	io.Get("/:bucket/create", minioHandler.CreateBucket)
	io.Get("/:bucket/delete", minioHandler.RemoveBucket)

	// resize
	app.Post("/resize", imageHandler.ResizeImage)

	// Minio
	if !disableGet {
		app.Get("/:bucket/w::width/h::height/*", imageHandler.GetImage)
		app.Get("/:bucket/w::width/*", imageHandler.GetImage)
		app.Get("/:bucket/h::height/*", imageHandler.GetImage)
		app.Get("/:bucket/*", imageHandler.GetImage)
	}

	if !disableDelete {
		app.Delete("/with-aws/:bucket/*", AuthMiddleware, imageHandler.DeleteImageWithAws)
		app.Delete("/:bucket/*", AuthMiddleware, imageHandler.DeleteImage)
	}

	if !disableUpload {
		app.Post("/upload", AuthMiddleware, imageHandler.UploadImage)
		app.Post("/upload-with-aws", AuthMiddleware, imageHandler.UploadImageWithAws)
		app.Post("/upload-url", AuthMiddleware, imageHandler.UploadImageWithUrl)
	}

	// Index
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendFile("./public/index.html")
	})

	log.Fatal(app.Listen(":9090"))

}

func AuthMiddleware(c *fiber.Ctx) error {
	if err := service.CheckToken(c); err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, "Invalid Token", nil)
	}
	return c.Next()
}
