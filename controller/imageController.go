package controller

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"MinioApi/service"
	"github.com/gofiber/fiber/v2"
	"github.com/minio/minio-go/v7"
)

type IImage interface {
	GetImage(c *fiber.Ctx) error
	GetImageWidth(c *fiber.Ctx) error
	GetImageHeight(c *fiber.Ctx) error
	GetImageWidthHeight(c *fiber.Ctx) error
	UploadImage(c *fiber.Ctx) error
	DeleteImage(c *fiber.Ctx) error
}

type image struct {
	minioClient minio.Client
}

func Image(client *minio.Client) IImage {
	return &image{
		minioClient: *client,
	}
}

func (i image) GetImage(c *fiber.Ctx) error {
	ctx := context.Background()

	bucket := c.Params("bucket")
	objectName := c.Params("*")

	found, _ := i.minioClient.BucketExists(ctx, bucket)

	object, err := i.minioClient.GetObject(ctx, bucket, objectName, minio.GetObjectOptions{})

	if !found || err != nil {
		return c.SendFile("./notfound.png")
	}

	getByte := service.StreamToByte(object)
	if len(getByte) == 0 {
		return c.Send(service.ImageToByte("./notfound.png"))
	}
	return c.Send(getByte)
}

func (i image) GetImageWidthHeight(c *fiber.Ctx) error {
	ctx := context.Background()

	bucket := c.Params("bucket")
	width := c.Params("width")
	height := c.Params("height")
	objectName := c.Params("*")

	width, height = service.SetWidthToHeight(width, height)

	found, _ := i.minioClient.BucketExists(ctx, bucket)

	object, err := i.minioClient.GetObject(ctx, bucket, objectName, minio.GetObjectOptions{})

	hWidth, wErr := strconv.ParseUint(width, 10, 16)

	hHeight, hErr := strconv.ParseUint(height, 10, 16)

	if wErr != nil || hErr != nil {
		return c.SendFile("./notfound.png")
	}

	if !found || err != nil {
		//return c.SendFile("./notfound.png")
		return c.Send(service.ImagickResize(service.ImageToByte("./notfound.png"), uint(hWidth), uint(hHeight)))
	}

	return c.Send(service.ImagickResize(service.StreamToByte(object), uint(hWidth), uint(hHeight)))
}

func (i image) GetImageWidth(c *fiber.Ctx) error {
	return i.GetImageWidthHeight(c)
}

func (i image) GetImageHeight(c *fiber.Ctx) error {
	return i.GetImageWidthHeight(c)
}

func (i image) DeleteImage(c *fiber.Ctx) error {

	ctx := context.Background()

	getToken := strings.Split(c.Get("Authorization"), " ")

	if !strings.EqualFold(getToken[1], os.Getenv("TOKEN")) {
		return c.JSON(fiber.Map{
			"error": true,
			"msg":   "Invalid Token",
		})
	}

	bucket := c.Params("bucket")
	objectName := c.Params("*")

	found, _ := i.minioClient.BucketExists(ctx, bucket)

	err := i.minioClient.RemoveObject(ctx, bucket, objectName, minio.RemoveObjectOptions{})

	if !found || err != nil {
		return c.JSON(fiber.Map{
			"error": true,
			"msg":   "File Not Found",
		})
	}

	return c.JSON(fiber.Map{
		"error": false,
		"msg":   "File Successfully Deleted",
	})
}

func (i image) UploadImage(c *fiber.Ctx) error {
	ctx := context.Background()

	path := c.FormValue("path")
	bucket := c.FormValue("bucket")
	file, err := c.FormFile("file")

	if file == nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": true,
			"msg":   "File Not Found!",
		})
	}

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

	found, _ := i.minioClient.BucketExists(ctx, bucket)
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
	info, err := i.minioClient.PutObject(ctx, bucket, objectName, fileBuffer, fileSize, minio.PutObjectOptions{ContentType: contentType})

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
