package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"MinioApi/service"
	"github.com/gofiber/fiber/v2"
	"github.com/minio/minio-go/v7"
	"gopkg.in/gographics/imagick.v3/imagick"
)

type IHandler interface{
	GetImage(c *fiber.Ctx) error
	GetImageWidth(c *fiber.Ctx) error
	GetImageHeight(c *fiber.Ctx) error
	GetImageWidthHeight(c *fiber.Ctx) error
	UploadImage(c *fiber.Ctx) error
}

type handler struct{
	minioClient minio.Client
}

func Handler(client *minio.Client) IHandler{
	return &handler{
		minioClient: *client,
	}
}

func (h handler) GetImage(c *fiber.Ctx) error{
	ctx := context.Background()

	imagick.Initialize()
	defer imagick.Terminate()
	var err error

	bucket := c.Params("bucket")
	objectName := c.Params("*")

	found, _ := h.minioClient.BucketExists(ctx, bucket)

	object, err := h.minioClient.GetObject(ctx, bucket, objectName, minio.GetObjectOptions{})

	if !found || err != nil {
		return c.SendFile("./notfound.png")
	}

	return c.SendStream(object)
}

func (h handler) GetImageWidthHeight(c *fiber.Ctx) error{
	ctx := context.Background()

	bucket := c.Params("bucket")
	width := c.Params("width")
	height := c.Params("height")
	objectName := c.Params("*")

	found, _ := h.minioClient.BucketExists(ctx, bucket)

	object, err := h.minioClient.GetObject(ctx, bucket, objectName, minio.GetObjectOptions{})

	if !found || err != nil {
		return c.SendFile("./notfound.png")
	}

	hWidth, err := strconv.ParseUint(width, 10, 16)
	if err != nil {
		fmt.Println(err)
	}

	hHeight, err := strconv.ParseUint(height, 10, 16)
	if err != nil {
		fmt.Println(err)
	}

	return c.Send(service.ImagickResize(service.StreamToByte(object), uint(hWidth), uint(hHeight)))
}

func (h handler) GetImageWidth(c *fiber.Ctx) error{
	bucket := c.Params("bucket")
	width := c.Params("width")
	objectName := c.Params("*")

	return c.JSON(fiber.Map{
		"bucket": bucket,
		"width": width,
		"objectName": objectName,
	})
}

func (h handler) GetImageHeight(c *fiber.Ctx) error{
	bucket := c.Params("bucket")
	height := c.Params("height")
	objectName := c.Params("*")

	return c.JSON(fiber.Map{
		"bucket": bucket,
		"height": height,
		"objectName": objectName,
	})
}

func (h handler) UploadImage(c *fiber.Ctx) error{
	ctx := context.Background()

	path := c.FormValue("path")
	bucket := c.FormValue("bucket")
	file, err := c.FormFile("file")

	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": true,
			"msg":   err.Error(),
		})
	}

	if len(path) == 0 || len(bucket) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": true,
			"msg":   "invalid path or bucket or file.",
		})
	}

	found, _ := h.minioClient.BucketExists(ctx, bucket)
	if !found {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": true,
			"msg":   "Bucket Not Found!",
		})
	}

	// Get Buffer from file
	buffer, err := file.Open()

	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": true,
			"msg":   err.Error(),
		})
	}
	defer buffer.Close()

	parseFileName := strings.Split(file.Filename, ".")

	if len(parseFileName) < 2 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": true,
			"msg":   "File extension not found!",
		})
	}

	randomName := service.RandomName(10)
	objectName := path + "/" + randomName + "." + parseFileName[1]
	fileBuffer := buffer
	contentType := file.Header["Content-Type"][0]
	fileSize := file.Size

	// Upload with PutObject
	info, err := h.minioClient.PutObject(ctx, bucket, objectName, fileBuffer, fileSize, minio.PutObjectOptions{ContentType: contentType})

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": true,
			"msg":   err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"error": false,
		"msg":   fmt.Sprintf("Successfully uploaded %s of size %d", objectName, info.Size),
		"info":  info,
	})
}