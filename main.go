package main

import (
	"MinioApi/controller"
	"MinioApi/service"
	"github.com/gofiber/fiber/v2"
)

var (
	minioClient = service.MinioClient()
	handler = controller.Handler(minioClient)
)

func main(){

	app := fiber.New()

	app.Get("/:bucket/w::width/h::height/*", handler.GetImageWidthHeight)
	app.Get("/:bucket/w::width/*", handler.GetImageWidth)
	app.Get("/:bucket/h::height/*", handler.GetImageHeight)
	app.Get("/:bucket/*", handler.GetImage)

	app.Post("/upload", handler.UploadImage)

	_ = app.Listen(":9090")

}

