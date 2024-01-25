// Package handler /*
/*
## License
This project is licensed under the APACHE Licence. Refer to https://github.com/mstgnz/go-minio-cdn/blob/main/LICENSE for more information.
*/
package handler

import (
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/minio/minio-go/v7"
	"github.com/mstgnz/go-minio-cdn/service"
)

type Image interface {
	GetImage(c *fiber.Ctx) error
	UploadImage(c *fiber.Ctx) error
	UploadImageWithAws(c *fiber.Ctx) error
	DeleteImage(c *fiber.Ctx) error
	DeleteImageWithAws(c *fiber.Ctx) error
	ResizeImage(c *fiber.Ctx) error
	UploadImageWithUrl(c *fiber.Ctx) error
}

type image struct {
	minioService minio.Client
	awsService   service.AwsService
}

func NewImage(minioService *minio.Client, awsService service.AwsService) Image {
	return &image{
		minioService: *minioService,
		awsService:   awsService,
	}
}

func (i image) GetImage(c *fiber.Ctx) error {
	ctx := context.Background()

	width := 0
	height := 0
	resize := false
	bucket := c.Params("bucket")
	objectName := c.Params("*")

	obj := strings.Split(objectName, "/")

	if len(obj) >= 3 {
		getWith, wErr := strconv.Atoi(obj[0])
		getHeight, hErr := strconv.Atoi(obj[1])

		if wErr == nil && hErr == nil {
			resize = true
			width = getWith
			height = getHeight
			objectName = strings.Join(obj[2:], "/")
		}
	}

	// Bucket exists
	if found, err := i.minioService.BucketExists(ctx, bucket); !found || err != nil {
		return c.SendFile("./public/notfound.png")
	}

	// Get Object
	object, err := i.minioService.GetObject(ctx, bucket, objectName, minio.GetObjectOptions{})

	if err != nil {
		return c.SendFile("./public/notfound.png")
	}

	// Convert Byte
	getByte := service.StreamToByte(object)
	if len(getByte) == 0 {
		return c.SendFile("./public/notfound.png")
	}

	// Set Content Type
	c.Set("Content-Type", http.DetectContentType(getByte))

	// Send Resized Image
	if resize {
		return c.Send(service.ImagickResize(getByte, uint(width), uint(height)))
	}

	// Send Original Image
	return c.Send(getByte)
}

func (i image) UploadImage(c *fiber.Ctx) error {
	ctx := context.Background()

	if err := service.CheckToken(c); err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, "Invalid Token")
	}

	path := c.FormValue("path")
	bucket := c.FormValue("bucket")
	file, err := c.FormFile("file")

	if file == nil || err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, "File Not Found!")
	}

	return i.commonUpload(c, ctx, path, bucket, file, false)
}

func (i image) UploadImageWithAws(c *fiber.Ctx) error {
	ctx := context.Background()

	if err := service.CheckToken(c); err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, "Invalid Token")
	}

	path := c.FormValue("path")
	bucket := c.FormValue("bucket")
	file, err := c.FormFile("file")

	if file == nil || err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, "File Not Found!")
	}

	return i.commonUpload(c, ctx, path, bucket, file, true)
}

func (i image) DeleteImage(c *fiber.Ctx) error {
	ctx := context.Background()
	bucket := c.FormValue("bucket")
	object := c.FormValue("object")

	if len(bucket) == 0 || len(object) == 0 {
		return service.Response(c, fiber.StatusBadRequest, false, "invalid path or bucket or file.")
	}

	return i.deleteObject(c, ctx, bucket, object, false)
}

func (i image) DeleteImageWithAws(c *fiber.Ctx) error {
	ctx := context.Background()
	bucket := c.FormValue("bucket")
	object := c.FormValue("object")

	if len(bucket) == 0 || len(object) == 0 {
		return service.Response(c, fiber.StatusBadRequest, false, "invalid path or bucket or file.")
	}

	return i.deleteObject(c, ctx, bucket, object, true)
}

func (i image) ResizeImage(c *fiber.Ctx) error {

	if err := service.CheckToken(c); err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, "Invalid Token")
	}

	width := c.FormValue("width")
	height := c.FormValue("height")
	file, err := c.FormFile("file")

	if file == nil || err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, "File Not Found!")
	}

	width, height = service.SetWidthToHeight(width, height)
	hWidth, wErr := strconv.ParseUint(width, 10, 16)

	hHeight, hErr := strconv.ParseUint(height, 10, 16)

	if wErr != nil || hErr != nil {
		return service.Response(c, fiber.StatusBadRequest, false, "width or height invalid!")
	}

	fileBuffer, err := file.Open()
	defer func(fileBuffer multipart.File) {
		_ = fileBuffer.Close()
	}(fileBuffer)

	if err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, err.Error())
	}

	c.Set("Content-Type", http.DetectContentType(service.StreamToByte(fileBuffer)))
	return c.Send(service.ImagickResize(service.StreamToByte(fileBuffer), uint(hWidth), uint(hHeight)))
}

func (i image) UploadImageWithUrl(c *fiber.Ctx) error {
	ctx := context.Background()

	if err := service.CheckToken(c); err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, "Invalid Token")
	}

	path := c.FormValue("path")
	bucket := c.FormValue("bucket")
	url := c.FormValue("url")
	extension := c.FormValue("extension")

	if len(path) == 0 || len(bucket) == 0 || len(url) == 0 || len(extension) == 0 {
		return service.Response(c, fiber.StatusBadRequest, false, "invalid path or bucket or url or extension.")
	}

	// Check to see if already exist bucket
	exists, err := i.minioService.BucketExists(ctx, bucket)
	if err != nil && !exists {
		// Bucket not found so Make a new bucket
		err = i.minioService.MakeBucket(ctx, bucket, minio.MakeBucketOptions{})
		if err != nil {
			return service.Response(c, fiber.StatusBadRequest, false, "Bucket Not Found And Not Created!")
		}
	}

	res, err := http.Get(url)
	if err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, err.Error())
	}

	fileSize, _ := strconv.Atoi(res.Header.Get("Content-Length"))
	contentType := res.Header.Get("Content-Type")
	randomName := service.RandomName(10)
	objectName := path + "/" + randomName + "." + extension

	// Upload with PutObject
	minioResult, err := i.minioService.PutObject(ctx, bucket, objectName, res.Body, int64(fileSize), minio.PutObjectOptions{ContentType: contentType})

	link := "https://cdn.destechhasar.com/" + bucket + "/" + objectName

	// S3 upload with glacier storage class
	awsResult, err := i.awsService.S3PutObject(bucket, objectName, res.Body)

	awsErr := fmt.Sprintf("S3 Successfully Uploaded")

	if err != nil {
		awsErr = fmt.Sprintf("S3 Failed Uploaded %s", err.Error())
	}

	return c.JSON(fiber.Map{
		"error":       false,
		"minioUpload": fmt.Sprintf("Minio Successfully Uploaded size %d", minioResult.Size),
		"minioResult": minioResult,
		"awsUpload":   awsErr,
		"awsResult":   awsResult,
		"imageName":   randomName,
		"objectName":  objectName,
		"link":        link,
	})
}

// Minio And Aws Upload
func (i image) commonUpload(c *fiber.Ctx, ctx context.Context, path, bucket string, file *multipart.FileHeader, awsUpload bool) error {
	// Check to see if the bucket already exists
	exists, err := i.minioService.BucketExists(ctx, bucket)
	if err != nil && !exists {
		// Bucket not found, so create a new one
		err = i.minioService.MakeBucket(ctx, bucket, minio.MakeBucketOptions{})
		if err != nil {
			return service.Response(c, fiber.StatusBadRequest, false, "Bucket Not Found And Not Created!")
		}
	}

	// Check if the AWS bucket exists if required
	if awsUpload && !i.awsService.BucketExists(bucket) {
		return service.Response(c, fiber.StatusBadRequest, false, "Bucket Not Found On Aws S3!")
	}

	// Get the file buffer
	fileBuffer, err := file.Open()
	defer func(fileBuffer multipart.File) {
		_ = fileBuffer.Close()
	}(fileBuffer)

	if err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, err.Error())
	}

	// Parse the file name and extension
	parseFileName := strings.Split(file.Filename, ".")
	if len(parseFileName) < 2 {
		return service.Response(c, fiber.StatusBadRequest, false, "File extension not found!")
	}

	// Generate random name and construct object name
	randomName := service.RandomName(10)
	imageName := randomName + "." + parseFileName[1]
	objectName := path + "/" + imageName
	contentType := file.Header["Content-Type"][0]
	fileSize := file.Size

	// Minio Upload
	_, err = i.minioService.PutObject(ctx, bucket, objectName, fileBuffer, fileSize, minio.PutObjectOptions{ContentType: contentType})
	minioResult := "Minio Successfully Uploaded"

	if err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, err.Error())
	}

	url := service.GetEnv("PROJECT_ENDPOINT")
	url = strings.TrimSuffix(url, "/")
	link := url + "/" + bucket + "/" + objectName

	// S3 Upload
	if awsUpload {

		awsResult := "S3 Successfully Uploaded"

		if _, err = i.awsService.S3PutObject(bucket, objectName, fileBuffer); err != nil {
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

	// Only Minio upload
	return c.JSON(fiber.Map{
		"status":      true,
		"minioResult": minioResult,
		"imageName":   imageName,
		"objectName":  objectName,
		"link":        link,
	})
}

// Minio And Aws Delete
func (i image) deleteObject(c *fiber.Ctx, ctx context.Context, bucket, object string, awsDelete bool) error {
	// Check token
	if err := service.CheckToken(c); err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, "Invalid Token")
	}

	// Check if the bucket exists on Minio
	if found, _ := i.minioService.BucketExists(ctx, bucket); !found {
		return service.Response(c, fiber.StatusBadRequest, false, "Bucket Not Found On Minio!")
	}

	// Check if the bucket exists on AWS S3 if required
	if awsDelete && !i.awsService.BucketExists(bucket) {
		return service.Response(c, fiber.StatusBadRequest, false, "Bucket Not Found On Aws S3!")
	}

	// Remove object from Minio
	if err := i.minioService.RemoveObject(ctx, bucket, object, minio.RemoveObjectOptions{}); err != nil {
		return service.Response(c, fiber.StatusInternalServerError, false, err.Error())
	}

	// Remove object from AWS S3 if required
	if awsDelete {
		if err := i.awsService.DeleteObjects(bucket, []string{object}); err != nil {
			return service.Response(c, fiber.StatusInternalServerError, false, err.Error())
		}
	}

	return service.Response(c, fiber.StatusOK, true, "File Successfully Deleted")
}
