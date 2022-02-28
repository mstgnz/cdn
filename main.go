package main

import (
	"log"

	"MinioApi/controller"
	"MinioApi/service"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/favicon"
)

var (
	minioClient = service.MinioClient()
	handler     = controller.Image(minioClient)
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

	app.Get("/:bucket/w::width/h::height/*", handler.GetImageWidthHeight)
	app.Get("/:bucket/w::width/*", handler.GetImageWidth)
	app.Get("/:bucket/h::height/*", handler.GetImageHeight)
	app.Get("/:bucket/*", handler.GetImage)

	app.Delete("/:bucket/*", handler.DeleteImage)

	app.Post("/upload", handler.UploadImage)

	log.Fatal(app.Listen(":9090"))

}
