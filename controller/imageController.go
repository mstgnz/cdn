package controller

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"GominioCdn/service"
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
	minioService minio.Client
	awsService   service.IAwsService
}

func Image(minioService *minio.Client, awsService service.IAwsService) IImage {
	return &image{
		minioService: *minioService,
		awsService:   awsService,
	}
}

func (i image) GetImage(c *fiber.Ctx) error {
	ctx := context.Background()

	bucket := c.Params("bucket")
	objectName := c.Params("*")

	found, _ := i.minioService.BucketExists(ctx, bucket)

	object, err := i.minioService.GetObject(ctx, bucket, objectName, minio.GetObjectOptions{})

	if !found || err != nil {
		return c.SendFile("./notfound.png")
	}

	getByte := service.StreamToByte(object)

	c.Set("Content-Type", http.DetectContentType(getByte))

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

	found, _ := i.minioService.BucketExists(ctx, bucket)

	object, err := i.minioService.GetObject(ctx, bucket, objectName, minio.GetObjectOptions{})

	hWidth, wErr := strconv.ParseUint(width, 10, 16)

	hHeight, hErr := strconv.ParseUint(height, 10, 16)

	if wErr != nil || hErr != nil {
		return c.SendFile("./notfound.png")
	}

	if !found || err != nil {
		//return c.SendFile("./notfound.png")
		return c.Send(service.ImagickResize(service.ImageToByte("./notfound.png"), uint(hWidth), uint(hHeight)))
	}

	getByte := service.StreamToByte(object)
	c.Set("content-type", http.DetectContentType(getByte))
	return c.Send(service.ImagickResize(getByte, uint(hWidth), uint(hHeight)))
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

	if len(getToken) != 2 || !strings.EqualFold(getToken[1], service.GetEnv("TOKEN")) {
		return c.JSON(fiber.Map{
			"error": true,
			"msg":   "Invalid Token",
		})
	}

	bucket := c.Params("bucket")
	objectName := c.Params("*")

	found, _ := i.minioService.BucketExists(ctx, bucket)

	err := i.minioService.RemoveObject(ctx, bucket, objectName, minio.RemoveObjectOptions{})

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

	getToken := strings.Split(c.Get("Authorization"), " ")
	if len(getToken) != 2 || !strings.EqualFold(getToken[1], service.GetEnv("TOKEN")) {
		return c.JSON(fiber.Map{
			"error": true,
			"msg":   "Invalid Token",
		})
	}

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

	found, _ := i.minioService.BucketExists(ctx, bucket)
	if !found {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": true,
			"msg":   "Bucket Not Found!",
		})
	}

	// Get Buffer from file
	fileBuffer, err := file.Open()

	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": true,
			"msg":   err.Error(),
		})
	}
	defer fileBuffer.Close()

	parseFileName := strings.Split(file.Filename, ".")

	if len(parseFileName) < 2 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": true,
			"msg":   "File extension not found!",
		})
	}

	randomName := service.RandomName(10)
	objectName := path + "/" + randomName + "." + parseFileName[1]
	contentType := file.Header["Content-Type"][0]
	fileSize := file.Size

	// Upload with PutObject
	minioResult, err := i.minioService.PutObject(ctx, bucket, objectName, fileBuffer, fileSize, minio.PutObjectOptions{ContentType: contentType})

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": true,
			"msg":   err.Error(),
		})
	}

	link := "localhost:9090/" + bucket + "/" + objectName

	// S3 upload with glacier storage class
	awsResult, err := i.awsService.S3PutObject(bucket, objectName, fileBuffer)

	awsErr := fmt.Sprintf("S3 Successfully Uploaded")

	if err != nil {
		awsErr = fmt.Sprintf("S3 Failed Uploaded %s", err.Error())
	}

	return c.JSON(fiber.Map{
		"error":       false,
		"minioUpload": fmt.Sprintf("Minio Successfully Uploaded %s of size %d", link, minioResult.Size),
		"minioResult": minioResult,
		"awsUpload":   awsErr,
		"awsResult":   awsResult,
	})
}
