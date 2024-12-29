// Package handler /*
/*
## License
This project is licensed under the APACHE Licence. Refer to https://github.com/mstgnz/go-minio-cdn/blob/main/LICENSE for more information.
*/
package handler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/mstgnz/cdn/pkg/batch"
	"github.com/mstgnz/cdn/pkg/config"
	"github.com/mstgnz/cdn/pkg/filetype"
	"github.com/mstgnz/cdn/pkg/validator"
	"github.com/mstgnz/cdn/pkg/worker"
	"github.com/mstgnz/cdn/service"
)

type Image interface {
	GetImage(c *fiber.Ctx) error
	UploadImage(c *fiber.Ctx) error
	DeleteImage(c *fiber.Ctx) error
	ResizeImage(c *fiber.Ctx) error
	UploadWithUrl(c *fiber.Ctx) error
}

type image struct {
	minioClient  *minio.Client
	awsService   service.AwsService
	imageService *service.ImageService
	workerPool   *worker.Pool
	batchProc    *batch.BatchProcessor
	cache        service.CacheService
}

// ImageProcessRequest represents an image processing request
type ImageProcessRequest struct {
	File        []byte
	Width       uint
	Height      uint
	ContentType string
	Filename    string
}

// UploadUrlRequest represents the request body for URL-based uploads
type UploadUrlRequest struct {
	Path      string `json:"path"`
	Bucket    string `json:"bucket" validate:"required"`
	URL       string `json:"url" validate:"required,url"`
	AWSUpload bool   `json:"aws_upload"`
}

func NewImage(minioClient *minio.Client, awsService service.AwsService, imageService *service.ImageService) Image {
	// Initialize worker pool with 5 workers
	workerConfig := worker.DefaultConfig()
	workerConfig.Workers = 5
	wp := worker.NewPool(workerConfig)
	wp.Start()

	img := &image{
		minioClient:  minioClient,
		awsService:   awsService,
		imageService: imageService,
		workerPool:   wp,
		cache:        nil,
	}

	// Initialize batch processor with default config
	batchConfig := batch.DefaultConfig()
	batchConfig.BatchSize = 10
	batchConfig.FlushTimeout = 5 * time.Second
	bp := batch.NewBatchProcessor(batchConfig, img.processBatch)
	bp.Start()

	// Initialize cache service
	cacheService, err := service.NewCacheService("redis://localhost:6379")
	if err != nil {
		log.Printf("Failed to initialize cache service: %v", err)
	}
	img.cache = cacheService
	img.batchProc = bp

	return img
}

func (i image) GetImage(c *fiber.Ctx) error {
	ctx := context.Background()
	bucket := c.Params("bucket")
	objectName := c.Params("*")

	var width uint
	var height uint
	var resize bool

	if service.IsImageFile(objectName) {
		resize, width, height = service.GetWidthAndHeight(c, service.ParamsType)
	}

	// Try to get from cache if resize is requested
	if resize {
		if cachedImage, err := i.cache.GetResizedImage(bucket, objectName, width, height); err == nil {
			c.Set("Content-Type", http.DetectContentType(cachedImage))
			return c.Send(cachedImage)
		}
	}

	// Continue with normal flow if not in cache
	if found, err := i.minioClient.BucketExists(ctx, bucket); !found || err != nil {
		return c.SendFile("./public/notfound.png")
	}

	object, err := i.minioClient.GetObject(ctx, bucket, objectName, minio.GetObjectOptions{})
	if err != nil {
		return c.SendFile("./public/notfound.png")
	}

	getByte := service.StreamToByte(object)
	if len(getByte) == 0 {
		return c.SendFile("./public/notfound.png")
	}

	if err, orjWidth, orjHeight := i.imageService.ImagickGetWidthHeight(getByte); err == nil {
		c.Set("Width", strconv.Itoa(int(orjWidth)))
		c.Set("Height", strconv.Itoa(int(orjHeight)))
	}

	c.Set("Content-Type", http.DetectContentType(getByte))

	if resize {
		resizedImage := i.imageService.ImagickResize(getByte, width, height)
		// Cache the resized image
		if err := i.cache.SetResizedImage(bucket, objectName, width, height, resizedImage); err != nil {
			log.Printf("Failed to cache resized image: %v", err)
		}
		return c.Send(resizedImage)
	}

	c.Status(http.StatusFound)
	return c.Send(getByte)
}

func (i image) UploadImage(c *fiber.Ctx) error {
	ctx := context.Background()

	path := c.FormValue("path")
	bucket := c.FormValue("bucket")
	file, err := c.FormFile("file")
	awsUpload := c.FormValue("aws_upload") == "true"

	if file == nil || err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, "File Not Found!", nil)
	}

	// Check to see if the bucket already exists
	exists, err := i.minioClient.BucketExists(ctx, bucket)
	if err != nil && !exists {
		// Bucket not found, so create a new one
		err = i.minioClient.MakeBucket(ctx, bucket, minio.MakeBucketOptions{})
		if err != nil {
			return service.Response(c, fiber.StatusBadRequest, false, "Bucket Not Found And Not Created!", nil)
		}
	}

	// Validate file
	if err := validator.ValidateFile(file); err != nil {
		if valErr, ok := err.(*validator.FileValidationError); ok {
			return service.Response(c, fiber.StatusBadRequest, false, valErr.Message, map[string]string{
				"code": valErr.Code,
			})
		}
		return service.Response(c, fiber.StatusBadRequest, false, err.Error(), nil)
	}

	// Check if the AWS bucket exists if required
	if awsUpload && !i.awsService.BucketExists(bucket) {
		return service.Response(c, fiber.StatusBadRequest, false, "Bucket Not Found On Aws S3!", nil)
	}

	// Get the file buffer
	fileBuffer, err := file.Open()
	defer func(fileBuffer multipart.File) {
		_ = fileBuffer.Close()
	}(fileBuffer)

	if err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, err.Error(), nil)
	}

	// Parse the file name and extension
	parseFileName := strings.Split(file.Filename, ".")
	if len(parseFileName) < 2 {
		return service.Response(c, fiber.StatusBadRequest, false, "File extension not found!", nil)
	}

	// Generate random name and construct object name
	randomName := service.RandomName(10)
	imageName := randomName + "." + parseFileName[1]
	objectName := imageName
	if path != "" {
		objectName = path + "/" + imageName
	}
	contentType := file.Header["Content-Type"][0]
	fileSize := file.Size

	// size
	if fileContent, err := io.ReadAll(fileBuffer); err == nil {
		// Validate file content
		if err := validator.ValidateFileContent(fileContent); err != nil {
			if valErr, ok := err.(*validator.FileValidationError); ok {
				return service.Response(c, fiber.StatusBadRequest, false, valErr.Message, map[string]string{
					"code": valErr.Code,
				})
			}
			return service.Response(c, fiber.StatusBadRequest, false, err.Error(), nil)
		}

		_, _ = fileBuffer.Seek(0, 0)
		fileSize = int64(len(fileContent))
		contentType = http.DetectContentType(fileContent)

		// set size
		var (
			orjWidth  uint
			orjHeight uint
		)
		if err, orjWidth, orjHeight = i.imageService.ImagickGetWidthHeight(fileContent); err == nil {
			c.Set("Width", strconv.Itoa(int(orjWidth)))
			c.Set("Height", strconv.Itoa(int(orjHeight)))
		}

		// resize
		resize, width, height := service.GetWidthAndHeight(c, service.FormsType)
		if resize && orjWidth > 0 && orjHeight > 0 {
			width, height = service.RatioWidthHeight(orjWidth, orjHeight, width, height)
			fileContent = i.imageService.ImagickResize(fileContent, width, height)
			if tempFile, err := service.CreateFile(fileContent); err == nil {
				defer func() {
					_ = tempFile.Close()
				}()
				fileSize = int64(len(fileContent))
				c.Set("Width", strconv.Itoa(int(width)))
				c.Set("Height", strconv.Itoa(int(height)))
				c.Set("Content-Length", strconv.Itoa(len(fileContent)))
				fileBuffer = tempFile
			}
		}
	}

	// Minio Upload
	_, err = i.minioClient.PutObject(ctx, bucket, objectName, fileBuffer, fileSize, minio.PutObjectOptions{ContentType: contentType})
	minioResult := "Minio Successfully Uploaded"

	if err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, err.Error(), nil)
	}

	url := config.GetEnvOrDefault("APP_URL", "http://localhost:9090")
	url = strings.TrimSuffix(url, "/")
	link := url + "/" + bucket + "/" + objectName

	// S3 Upload
	if awsUpload {
		awsResult := "S3 Successfully Uploaded"
		if _, err = i.awsService.S3PutObject(bucket, objectName, fileBuffer); err != nil {
			awsResult = fmt.Sprintf("S3 Failed Uploaded %s", err.Error())
		}
		return service.Response(c, fiber.StatusCreated, true, "success", map[string]any{
			"minioUpload": fmt.Sprintf("Minio Successfully Uploaded size %d", fileSize),
			"minioResult": minioResult,
			"awsUpload":   awsResult,
			"awsResult":   awsResult,
			"imageName":   imageName,
			"objectName":  objectName,
			"link":        link,
		})
	}

	// Only Minio upload
	return service.Response(c, fiber.StatusCreated, true, "success", map[string]any{
		"minioUpload": fmt.Sprintf("Minio Successfully Uploaded size %d", fileSize),
		"minioResult": minioResult,
		"awsUpload":   "",
		"awsResult":   "",
		"imageName":   imageName,
		"objectName":  objectName,
		"link":        link,
	})
}

func (i image) UploadWithUrl(c *fiber.Ctx) error {
	ctx := context.Background()

	// Parse request body
	var req UploadUrlRequest
	if err := c.BodyParser(&req); err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, "Invalid request body", nil)
	}

	// Validate request
	if err := validator.ValidateStruct(req); err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, err.Error(), nil)
	}

	// Check to see if already exist bucket
	exists, err := i.minioClient.BucketExists(ctx, req.Bucket)
	if err != nil && !exists {
		// Bucket not found so Make a new bucket
		err = i.minioClient.MakeBucket(ctx, req.Bucket, minio.MakeBucketOptions{})
		if err != nil {
			return service.Response(c, fiber.StatusBadRequest, false, "Bucket Not Found And Not Created!", nil)
		}
	}

	// Check if the AWS bucket exists if required
	if req.AWSUpload && !i.awsService.BucketExists(req.Bucket) {
		return service.Response(c, fiber.StatusBadRequest, false, "Bucket Not Found On Aws S3!", nil)
	}

	res, err := http.Get(req.URL)
	if err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, err.Error(), nil)
	}
	defer res.Body.Close()

	// Read content from URL
	content, err := io.ReadAll(res.Body)
	if err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, "Failed to read content from URL", nil)
	}

	// Automatically detect content type
	contentType := http.DetectContentType(content)

	// Determine file extension from content type
	extension := filetype.GetExtensionFromContentType(contentType)
	if extension == "" {
		// Try to extract extension from URL if content type is not recognized
		extension = filetype.GetExtensionFromURL(req.URL)
		if !filetype.IsValidExtension(extension) {
			return service.Response(c, fiber.StatusBadRequest, false, "Unsupported or unrecognized file type", nil)
		}
	}

	randomName := service.RandomName(10)
	objectName := randomName + "." + extension
	if req.Path != "" {
		objectName = req.Path + "/" + randomName + "." + extension
	}

	// Prepare content as a new reader
	contentReader := bytes.NewReader(content)

	// Upload with PutObject
	minioResult, err := i.minioClient.PutObject(ctx, req.Bucket, objectName, contentReader, int64(len(content)), minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, err.Error(), nil)
	}

	url := config.GetEnvOrDefault("APP_URL", "http://localhost:9090")
	url = strings.TrimSuffix(url, "/")
	link := url + "/" + req.Bucket + "/" + objectName

	// S3 upload with glacier storage class
	awsResult := "S3 Successfully Uploaded"
	if req.AWSUpload {
		contentReader.Seek(0, 0) // Reset reader to beginning
		_, err := i.awsService.S3PutObject(req.Bucket, objectName, contentReader)
		if err != nil {
			awsResult = fmt.Sprintf("S3 Failed Uploaded %s", err.Error())
		}
		return service.Response(c, fiber.StatusCreated, true, "success", map[string]any{
			"minioUpload": fmt.Sprintf("Minio Successfully Uploaded size %d", minioResult.Size),
			"minioResult": minioResult,
			"awsUpload":   awsResult,
			"awsResult":   awsResult,
			"imageName":   randomName + "." + extension,
			"objectName":  objectName,
			"link":        link,
		})
	}

	return service.Response(c, fiber.StatusCreated, true, "success", map[string]any{
		"minioUpload": fmt.Sprintf("Minio Successfully Uploaded size %d", minioResult.Size),
		"minioResult": minioResult,
		"awsUpload":   "",
		"awsResult":   "",
		"imageName":   randomName + "." + extension,
		"objectName":  objectName,
		"link":        link,
	})
}

// DeleteImage handles image deletion
func (i image) DeleteImage(c *fiber.Ctx) error {
	ctx := context.Background()
	bucket := c.Params("bucket")
	awsDelete := c.Params("aws_delete") == "true"
	object := c.Params("*")

	if len(bucket) == 0 || len(object) == 0 {
		return service.Response(c, fiber.StatusBadRequest, false, "invalid path or bucket or file.", nil)
	}

	// Check if the bucket exists on Minio
	if found, _ := i.minioClient.BucketExists(ctx, bucket); !found {
		return service.Response(c, fiber.StatusBadRequest, false, "Bucket Not Found On Minio!", "")
	}

	// Check if the bucket exists on AWS S3 if required
	if awsDelete && !i.awsService.BucketExists(bucket) {
		return service.Response(c, fiber.StatusBadRequest, false, "Bucket Not Found On Aws S3!", "")
	}

	// Remove object from Minio
	if err := i.minioClient.RemoveObject(ctx, bucket, object, minio.RemoveObjectOptions{}); err != nil {
		return service.Response(c, fiber.StatusInternalServerError, false, err.Error(), "")
	}

	// Remove object from AWS S3 if required
	if awsDelete {
		if err := i.awsService.DeleteObjects(bucket, []string{object}); err != nil {
			return service.Response(c, fiber.StatusInternalServerError, false, err.Error(), "")
		}
	}

	return service.Response(c, fiber.StatusOK, true, "File Successfully Deleted", "")
}

// ResizeImage handles image resizing using worker pool
func (i *image) ResizeImage(c *fiber.Ctx) error {
	resize, width, height := service.GetWidthAndHeight(c, service.FormsType)
	file, err := c.FormFile("file")

	if file == nil || err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, "File Not Found!", nil)
	}

	fileBuffer, err := file.Open()
	if err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, err.Error(), nil)
	}
	defer fileBuffer.Close()

	fileContent, err := io.ReadAll(fileBuffer)
	if err != nil {
		return service.Response(c, fiber.StatusInternalServerError, false, "Error reading file content", nil)
	}

	if !resize || !service.IsImageFile(file.Filename) {
		c.Set("Content-Length", strconv.Itoa(len(fileContent)))
		c.Set("Content-Type", http.DetectContentType(fileContent))
		return c.Send(fileContent)
	}

	// Create response channel
	respChan := make(chan error)

	// Create and submit job
	job := worker.Job{
		ID: uuid.New().String(),
		Task: func() error {
			req := &ImageProcessRequest{
				File:        fileContent,
				Width:       uint(width),
				Height:      uint(height),
				ContentType: file.Header.Get("Content-Type"),
				Filename:    file.Filename,
			}
			return processImage(req, i)
		},
		Response: respChan,
	}

	i.workerPool.Submit(job)

	// Wait for response
	if err := <-respChan; err != nil {
		return service.Response(c, fiber.StatusInternalServerError, false, "Image processing failed", nil)
	}

	return service.Response(c, fiber.StatusOK, true, "Image processed successfully", nil)
}

// processBatch handles batch processing of items
func (i *image) processBatch(items []batch.BatchItem) []batch.BatchItem {
	// Process items in parallel using goroutines
	var wg sync.WaitGroup
	for idx := range items {
		wg.Add(1)
		go func(item *batch.BatchItem) {
			defer wg.Done()

			// Process the item based on its type
			switch data := item.Data.(type) {
			case *ImageProcessRequest:
				// Process image
				err := processImage(data, i)
				item.Error = err
				item.Success = err == nil
			}
		}(&items[idx])
	}
	wg.Wait()
	return items
}

// processImage handles the actual image processing
func processImage(req *ImageProcessRequest, i *image) error {
	if service.IsImageFile(req.Filename) {
		resized := i.imageService.ImagickResize(req.File, req.Width, req.Height)
		if resized == nil {
			return fmt.Errorf("image processing failed")
		}
		return nil
	}
	return nil
}
