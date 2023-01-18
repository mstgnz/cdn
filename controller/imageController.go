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
	UploadImage(c *fiber.Ctx) error
	UploadImageWithAws(c *fiber.Ctx) error
	ResizeImage(c *fiber.Ctx) error
	DeleteImage(c *fiber.Ctx) error
	DeleteImageWithAws(c *fiber.Ctx) error
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

	resize := false
	width := 0
	height := 0
	bucket := c.Params("bucket")
	objectName := c.Params("*")

	// if resize
	obj := strings.Split(objectName, "/")

	width, wErr := strconv.Atoi(obj[0])
	height, hErr := strconv.Atoi(obj[1])

	if wErr == nil && hErr == nil {
		resize = true
		objectName = strings.Join(obj[2:], "/")
	}

	// Bucket exists
	if found, _ := i.minioService.BucketExists(ctx, bucket); !found {
		return c.SendFile("./notfound.png")
	}

	object, err := i.minioService.GetObject(ctx, bucket, objectName, minio.GetObjectOptions{})

	if err != nil {
		return c.SendFile("./notfound.png")
	}

	getByte := service.StreamToByte(object)
	if len(getByte) == 0 {
		return c.SendFile("./notfound.png")
	}
	c.Set("Content-Type", http.DetectContentType(getByte))
	if resize {
		return c.Send(service.ImagickResize(getByte, uint(width), uint(height)))
	}
	return c.Send(getByte)
}

func (i image) DeleteImage(c *fiber.Ctx) error {

	ctx := context.Background()

	getToken := strings.Split(c.Get("Authorization"), " ")
	if len(getToken) != 2 || !strings.EqualFold(getToken[1], service.GetEnv("TOKEN")) {
		return c.JSON(fiber.Map{
			"status":  false,
			"message": "Invalid Token",
		})
	}

	bucket := c.FormValue("bucket")
	object := c.FormValue("object")

	if len(bucket) == 0 || len(object) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  false,
			"message": "invalid path or bucket or file.",
		})
	}

	// Minio Bucket Exists
	if found, _ := i.minioService.BucketExists(ctx, bucket); !found {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  false,
			"message": "Bucket Not Found On Minio!",
		})
	}

	err := i.minioService.RemoveObject(ctx, bucket, object, minio.RemoveObjectOptions{})
	if err != nil {
		return c.JSON(fiber.Map{
			"status":  false,
			"message": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"status":  true,
		"message": "File Successfully Deleted",
	})
}

func (i image) DeleteImageWithAws(c *fiber.Ctx) error {

	ctx := context.Background()

	getToken := strings.Split(c.Get("Authorization"), " ")
	if len(getToken) != 2 || !strings.EqualFold(getToken[1], service.GetEnv("TOKEN")) {
		return c.JSON(fiber.Map{
			"status":  false,
			"message": "Invalid Token",
		})
	}

	bucket := c.FormValue("bucket")
	object := c.FormValue("object")

	if len(bucket) == 0 || len(object) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  false,
			"message": "invalid path or bucket or file.",
		})
	}

	// Minio Bucket Exists
	if found, _ := i.minioService.BucketExists(ctx, bucket); !found {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  false,
			"message": "Bucket Not Found On Minio!",
		})
	}

	// Aws Bucket Exists
	if !i.awsService.BucketExists(bucket) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  false,
			"message": "Bucket Not Found On Aws S3!",
		})
	}

	err := i.minioService.RemoveObject(ctx, bucket, object, minio.RemoveObjectOptions{})
	if err != nil {
		return c.JSON(fiber.Map{
			"status":  false,
			"message": err.Error(),
		})
	}
	err = i.awsService.DeleteObjects(bucket, []string{object})
	if err != nil {
		return c.JSON(fiber.Map{
			"status":  false,
			"message": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"status":  true,
		"message": "File Successfully Deleted",
	})
}

func (i image) UploadImage(c *fiber.Ctx) error {
	ctx := context.Background()

	getToken := strings.Split(c.Get("Authorization"), " ")
	if len(getToken) != 2 || !strings.EqualFold(getToken[1], service.GetEnv("TOKEN")) {
		return c.JSON(fiber.Map{
			"status":  false,
			"message": "Invalid Token",
		})
	}

	path := c.FormValue("path")
	bucket := c.FormValue("bucket")
	file, err := c.FormFile("file")

	if file == nil || err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  false,
			"message": "File Not Found!",
		})
	}

	if len(path) == 0 || len(bucket) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  false,
			"message": "invalid path or bucket or file.",
		})
	}

	// Minio Bucket Exists
	if found, _ := i.minioService.BucketExists(ctx, bucket); !found {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  false,
			"message": "Bucket Not Found On Minio!",
		})
	}

	// Get Buffer from file
	fileBuffer, err := file.Open()
	defer fileBuffer.Close()

	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  false,
			"message": err.Error(),
		})
	}

	parseFileName := strings.Split(file.Filename, ".")

	if len(parseFileName) < 2 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  false,
			"message": "File extension not found!",
		})
	}

	randomName := service.RandomName(10)
	imageName := randomName + "." + parseFileName[1]
	objectName := path + "/" + imageName
	contentType := file.Header["Content-Type"][0]
	fileSize := file.Size

	// Minio Upload
	_, err = i.minioService.PutObject(ctx, bucket, objectName, fileBuffer, fileSize, minio.PutObjectOptions{ContentType: contentType})
	minioResult := "Minio Successfully Uploaded"

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"status":  false,
			"message": err.Error(),
		})
	}

	url := service.GetEnv("PROJECT_ENDPOINT")
	url = strings.TrimSuffix(url, "/")
	link := url + "/" + bucket + "/" + objectName

	return c.JSON(fiber.Map{
		"status":      true,
		"minioResult": minioResult,
		"imageName":   imageName,
		"objectName":  objectName,
		"link":        link,
	})
}

func (i image) UploadImageWithAws(c *fiber.Ctx) error {
	ctx := context.Background()

	getToken := strings.Split(c.Get("Authorization"), " ")
	if len(getToken) != 2 || !strings.EqualFold(getToken[1], service.GetEnv("TOKEN")) {
		return c.JSON(fiber.Map{
			"status":  false,
			"message": "Invalid Token",
		})
	}

	path := c.FormValue("path")
	bucket := c.FormValue("bucket")
	file, err := c.FormFile("file")

	if file == nil || err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  false,
			"message": "File Not Found!",
		})
	}

	if len(path) == 0 || len(bucket) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  false,
			"message": "invalid path or bucket or file.",
		})
	}

	// Minio Bucket Exists
	if found, _ := i.minioService.BucketExists(ctx, bucket); !found {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  false,
			"message": "Bucket Not Found On Minio!",
		})
	}

	// Aws Bucket Exists
	if !i.awsService.BucketExists(bucket) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  false,
			"message": "Bucket Not Found On Aws S3!",
		})
	}

	// Get Buffer from file
	fileBuffer, err := file.Open()
	defer fileBuffer.Close()

	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  false,
			"message": err.Error(),
		})
	}

	parseFileName := strings.Split(file.Filename, ".")

	if len(parseFileName) < 2 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  false,
			"message": "File extension not found!",
		})
	}

	randomName := service.RandomName(10)
	imageName := randomName + "." + parseFileName[1]
	objectName := path + "/" + imageName
	contentType := file.Header["Content-Type"][0]
	fileSize := file.Size

	// Minio Upload
	_, err = i.minioService.PutObject(ctx, bucket, objectName, fileBuffer, fileSize, minio.PutObjectOptions{ContentType: contentType})
	minioResult := "Minio Successfully Uploaded"

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"status":  false,
			"message": err.Error(),
		})
	}

	url := service.GetEnv("PROJECT_ENDPOINT")
	url = strings.TrimSuffix(url, "/")
	link := url + "/" + bucket + "/" + objectName

	// S3 Upload
	_, err = i.awsService.S3PutObject(bucket, objectName, fileBuffer)
	awsResult := "S3 Successfully Uploaded"

	if err != nil {
		awsResult = fmt.Sprintf("S3 Failed Uploaded %s", err.Error())
	}

	return c.JSON(fiber.Map{
		"status":      true,
		"minioResult": minioResult,
		"awsResult":   awsResult,
		"imageName":   imageName,
		"objectName":  objectName,
		"link":        link,
	})
}

func (i image) ResizeImage(c *fiber.Ctx) error {

	getToken := strings.Split(c.Get("Authorization"), " ")
	if len(getToken) != 2 || !strings.EqualFold(getToken[1], service.GetEnv("TOKEN")) {
		return c.JSON(fiber.Map{
			"status":  false,
			"message": "Invalid Token",
		})
	}

	width := c.FormValue("width")
	height := c.FormValue("height")
	file, err := c.FormFile("file")

	if file == nil || err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  false,
			"message": "File Not Found!",
		})
	}

	width, height = service.SetWidthToHeight(width, height)
	hWidth, wErr := strconv.ParseUint(width, 10, 16)

	hHeight, hErr := strconv.ParseUint(height, 10, 16)

	if wErr != nil || hErr != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  false,
			"message": "width or height invalid!",
		})
	}

	fileBuffer, err := file.Open()
	defer fileBuffer.Close()

	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  false,
			"message": err.Error(),
		})
	}

	c.Set("content-type", http.DetectContentType(service.StreamToByte(fileBuffer)))
	return c.Send(service.ImagickResize(service.StreamToByte(fileBuffer), uint(hWidth), uint(hHeight)))
}
