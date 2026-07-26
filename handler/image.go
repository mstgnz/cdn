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
	bucketname "github.com/mstgnz/cdn/pkg/bucket"
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
	BatchUpload(c *fiber.Ctx) error
	BatchDelete(c *fiber.Ctx) error
}

type image struct {
	minioClient  *minio.Client
	awsService   service.AwsService
	imageService *service.ImageService
	workerPool   *worker.Pool
	batchProc    *batch.BatchProcessor
}

// ImageProcessRequest represents an image processing request
type ImageProcessRequest struct {
	File        []byte
	Width       uint
	Height      uint
	ContentType string
	Filename    string
}

// UploadUrlRequest represents the request body for URL-based uploads.
//
// Bucket carries no "required" tag: a bucket-scoped token already names its
// bucket, so the field is optional for those callers. Emptiness is enforced
// after the token and the request have been reconciled by resolveBucket.
type UploadUrlRequest struct {
	Path      string `json:"path"`
	Bucket    string `json:"bucket"`
	URL       string `json:"url" validate:"required,url"`
	AWSUpload bool   `json:"aws_upload"`
	Optimize  bool   `json:"optimize"`
}

// optimizeSem bounds the number of concurrent ImageMagick optimizations
// independently of the per-endpoint upload concurrency, since a re-encode is
// CPU/memory heavy. Initialized lazily so OPTIMIZE_MAX_CONCURRENT is read after
// .env is loaded.
var (
	optimizeSemOnce sync.Once
	optimizeSem     chan struct{}
)

func acquireOptimizeSlot() func() {
	optimizeSemOnce.Do(func() {
		n := config.GetEnvAsIntOrDefault("OPTIMIZE_MAX_CONCURRENT", 4)
		if n < 1 {
			n = 1
		}
		optimizeSem = make(chan struct{}, n)
	})
	optimizeSem <- struct{}{}
	return func() { <-optimizeSem }
}

// validateImageContent enforces that a file whose extension marks it an image
// actually is a valid image. Raster formats must decode via ImageMagick; SVG is
// validated structurally (it is XML/text, not a raster the decoder reliably
// reads). Non-image extensions return (0,0,nil) and upload unchanged. Honors the
// VALIDATE_FILE toggle: when validation is disabled it never rejects. On success
// it returns the decoded dimensions (0,0 for SVG).
func (i image) validateImageContent(filename string, content []byte) (uint, uint, error) {
	if !service.IsImageFile(filename) {
		return 0, 0, nil
	}
	validate := config.GetEnvAsBoolOrDefault("VALIDATE_FILE", true)

	if strings.HasSuffix(strings.ToLower(filename), ".svg") {
		if !isValidSVG(content) && validate {
			return 0, 0, fmt.Errorf("invalid image content")
		}
		return 0, 0, nil
	}

	w, h, _, err := i.imageService.GetImageInfo(content)
	if err != nil {
		if validate {
			return 0, 0, fmt.Errorf("invalid image content")
		}
		return 0, 0, nil
	}
	return w, h, nil
}

// isValidSVG does a lightweight structural check: a valid SVG must contain an
// <svg root element near the top. Rejects e.g. a PDF or script renamed to .svg.
func isValidSVG(content []byte) bool {
	head := content
	if len(head) > 2048 {
		head = head[:2048]
	}
	return bytes.Contains(bytes.ToLower(head), []byte("<svg"))
}

// maybeOptimize re-encodes content per opts, bounding ImageMagick concurrency.
// It never fails the upload: on any optimization error it logs and returns the
// original bytes with zero dimensions (caller keeps whatever dims it had).
func (i image) maybeOptimize(content []byte, opts service.OptimizeOptions) ([]byte, uint, uint) {
	release := acquireOptimizeSlot()
	defer release()

	out, w, h, err := i.imageService.OptimizeImage(content, opts)
	if err != nil {
		log.Printf("Warning: image optimization failed, storing original: %v", err)
		return content, 0, 0
	}
	return out, w, h
}

// BatchUploadRequest represents the request body for batch uploads
type BatchUploadRequest struct {
	Bucket    string   `json:"bucket" validate:"required"`
	Path      string   `json:"path"`
	Files     []string `json:"files" validate:"required,min=1"`
	AWSUpload bool     `json:"aws_upload"`
}

// BatchDeleteRequest represents the request body for batch deletions. Bucket is
// optional for the same reason as UploadUrlRequest.Bucket.
type BatchDeleteRequest struct {
	Bucket    string   `json:"bucket"`
	Files     []string `json:"files" validate:"required,min=1"`
	AWSDelete bool     `json:"aws_delete"`
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
	}

	// Initialize batch processor with default config
	batchConfig := batch.DefaultConfig()
	batchConfig.BatchSize = 10
	batchConfig.FlushTimeout = 5 * time.Second
	bp := batch.NewBatchProcessor(batchConfig, img.processBatch)
	bp.Start()

	img.batchProc = bp

	return img
}

func (i image) GetImage(c *fiber.Ctx) error {
	ctx := context.Background()
	bucket := c.Params("bucket")
	objectName := c.Params("*")

	// Reject traversal-like keys instead of forwarding them verbatim to MinIO.
	if service.HasUnsafeObjectKey(objectName) {
		return c.SendFile("./public/notfound.png")
	}

	var width uint
	var height uint
	var resize bool

	if service.IsImageFile(objectName) {
		// Both forms are documented and routed, so both have to be read here: the
		// path form (/:bucket/w:100/h:100/*, and the width- or height-only
		// variants) and the query form (?width=100&height=100).
		//
		// Reading only the query form silently served the original for every path
		// request, because those routes still matched and this branch then found
		// no dimensions. The path form is checked first since it is the more
		// specific route; a request that carries neither falls through unresized.
		resize, width, height = service.GetWidthAndHeight(c, service.ParamsType)
		if !resize {
			resize, width, height = service.GetWidthAndHeight(c, service.QueryType)
		}
	}

	if found, err := i.minioClient.BucketExists(ctx, bucket); !found || err != nil {
		return c.SendFile("./public/notfound.png")
	}

	object, err := i.minioClient.GetObject(ctx, bucket, objectName, minio.GetObjectOptions{})
	if err != nil {
		return c.SendFile("./public/notfound.png")
	}

	// SVG needs its type declared rather than sniffed. http.DetectContentType
	// cannot recognise SVG and answers text/plain, which combined with the global
	// nosniff header meant browsers refused to render it at all: <img src=x.svg>
	// showed nothing. Declaring image/svg+xml restores that.
	//
	// The safety of doing so rests on the CSP below, not on the type being wrong.
	// SVG can carry inline <script>, and `sandbox` with no allow- flags blocks
	// script execution on direct navigation, while `default-src 'none'` stops the
	// document reaching anything external. In an <img> context scripts never run
	// regardless. nosniff stays on and now means "this really is SVG", which is
	// the guarantee it is meant to give.
	isSVG := strings.HasSuffix(strings.ToLower(objectName), ".svg")
	if isSVG {
		c.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	}

	// contentTypeFor keeps the sniffed type for everything else. That is what
	// makes a valid image carrying an appended payload serve as image/*, so it
	// cannot be reinterpreted as script.
	contentTypeFor := func(head []byte) string {
		if isSVG {
			return "image/svg+xml"
		}
		return http.DetectContentType(head)
	}

	// Resize path: ImageMagick must decode the whole image, so the object is
	// fully buffered here. Width/Height headers come from the decode we are
	// already doing for the resize.
	if resize {
		defer object.Close()

		getByte := service.StreamToByte(object)
		if len(getByte) == 0 {
			return c.SendFile("./public/notfound.png")
		}

		if err, orjWidth, orjHeight := i.imageService.ImagickGetWidthHeight(getByte); err == nil {
			c.Set("Width", strconv.Itoa(int(orjWidth)))
			c.Set("Height", strconv.Itoa(int(orjHeight)))
		}

		c.Set("Content-Type", contentTypeFor(getByte))
		c.Status(http.StatusOK)
		return c.Send(i.imageService.ImagickResize(getByte, width, height))
	}

	// Direct (non-resize) path: stream the object straight to the client with
	// constant memory, whatever its type or size (original images served
	// as-is, PDFs, videos, large files). This avoids buffering whole objects
	// into RAM and the per-request ImageMagick decode. fasthttp closes the
	// stream (and therefore the MinIO object) once the response is written.
	stat, err := object.Stat()
	if err != nil || stat.Size == 0 {
		_ = object.Close()
		return c.SendFile("./public/notfound.png")
	}

	// Sniff the content type from the first bytes, then replay them in front of
	// the remaining stream so nothing is lost.
	head := make([]byte, 512)
	n, readErr := io.ReadFull(object, head)
	if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
		_ = object.Close()
		return c.SendFile("./public/notfound.png")
	}
	head = head[:n]

	c.Set("Content-Type", contentTypeFor(head))
	c.Status(http.StatusOK)
	return c.SendStream(streamCloser{
		Reader: io.MultiReader(bytes.NewReader(head), object),
		closer: object,
	}, int(stat.Size))
}

// streamCloser couples the reader handed to fasthttp with the underlying
// closer, so closing the response stream also closes the MinIO object.
// io.MultiReader is not an io.Closer, so without this the object would leak.
type streamCloser struct {
	io.Reader
	closer io.Closer
}

func (s streamCloser) Close() error { return s.closer.Close() }

func (i image) UploadImage(c *fiber.Ctx) error {
	ctx := context.Background()

	path := c.FormValue("path")
	file, err := c.FormFile("file")
	awsUpload := c.FormValue("aws_upload") == "true"

	if file == nil || err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, "File Not Found!", nil)
	}

	// The bucket a scoped token owns wins over whatever the form says; a scoped
	// token naming someone else's bucket is refused outright. Deliberately after
	// the file check so a non-multipart body still reports "File Not Found!"
	// rather than a bucket complaint.
	bucket, err := resolveBucket(c, c.FormValue("bucket"))
	if err != nil {
		return bucketForbidden(c)
	}
	if bucket == "" {
		return service.Response(c, fiber.StatusBadRequest, false, "Bucket is required", nil)
	}

	// Check to see if the bucket already exists. BucketExists returns
	// (false, nil) for a genuinely missing bucket, so create when !exists (the
	// previous `err != nil && !exists` never created a missing bucket and the
	// later PutObject failed with "bucket does not exist").
	exists, err := i.minioClient.BucketExists(ctx, bucket)
	if err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, "bucket check failed: "+err.Error(), nil)
	}
	if !exists {
		// Only a genuinely new bucket is name-checked. Buckets that already exist
		// predate this rule and must keep working whatever they are called, so the
		// check sits here rather than on the upload path as a whole.
		if err := bucketname.Validate(bucket); err != nil {
			return service.Response(c, fiber.StatusBadRequest, false, err.Error(), nil)
		}
		if err := i.minioClient.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
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
	randomName := uuid.New().String()
	// Sanitize file extension
	fileExtension := service.SanitizeObjectName(parseFileName[len(parseFileName)-1])
	imageName := randomName + "." + fileExtension
	objectName := imageName
	if path != "" {
		// Sanitize path as well
		path = strings.Trim(path, "/")
		sanitizedPath := service.SanitizeObjectName(path)
		objectName = sanitizedPath + "/" + imageName
	}
	// Use Header.Get: a multipart part may omit Content-Type, and indexing the
	// raw header slice ([0]) would panic on a nil/empty slice.
	contentType := file.Header.Get("Content-Type")
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

		// A file with an image extension must actually be a valid image; this
		// also yields the original dimensions in a single decode. Non-image
		// files pass through untouched.
		orjWidth, orjHeight, verr := i.validateImageContent(file.Filename, fileContent)
		if verr != nil {
			return service.Response(c, fiber.StatusBadRequest, false, "invalid image content", map[string]string{
				"code": "INVALID_IMAGE_CONTENT",
			})
		}
		if orjWidth > 0 && orjHeight > 0 {
			c.Set("Width", strconv.Itoa(int(orjWidth)))
			c.Set("Height", strconv.Itoa(int(orjHeight)))
		}

		// resize / optimize
		resize, width, height := service.GetWidthAndHeight(c, service.FormsType)
		optimize := c.FormValue("optimize") == "true"

		switch {
		case optimize && service.IsImageFile(file.Filename):
			// Opt-in visually-lossless optimization. Explicit width/height win
			// over the max-dimension cap, and quality/strip apply in one pass.
			opts := service.DefaultOptimizeOptions()
			if resize {
				opts.TargetWidth = width
				opts.TargetHeight = height
			}
			optimized, ow, oh := i.maybeOptimize(fileContent, opts)
			fileContent = optimized
			if tempFile, err := service.CreateFile(fileContent); err == nil {
				defer func() {
					_ = tempFile.Close()
				}()
				fileSize = int64(len(fileContent))
				if ow > 0 && oh > 0 {
					c.Set("Width", strconv.Itoa(int(ow)))
					c.Set("Height", strconv.Itoa(int(oh)))
				}
				c.Set("Content-Length", strconv.Itoa(len(fileContent)))
				fileBuffer = tempFile
			}
		case resize && orjWidth > 0 && orjHeight > 0:
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

	// Reconcile the body with the token before anything acts on req.Bucket.
	bucketName, err := resolveBucket(c, req.Bucket)
	if err != nil {
		return bucketForbidden(c)
	}
	if bucketName == "" {
		return service.Response(c, fiber.StatusBadRequest, false, "Bucket is required", nil)
	}
	req.Bucket = bucketName

	// SSRF guard: reject non-http(s) schemes and literal private/loopback/
	// metadata targets before any request is made. Ordered before the MinIO
	// call so it is exercised without a live storage backend.
	if err := validator.ValidateUploadURL(req.URL); err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, err.Error(), nil)
	}

	// Check to see if the bucket already exists (create when genuinely missing;
	// BucketExists returns (false, nil) in that case).
	exists, err := i.minioClient.BucketExists(ctx, req.Bucket)
	if err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, "bucket check failed: "+err.Error(), nil)
	}
	if !exists {
		// See UploadImage: the name rule applies to bucket creation only.
		if err := bucketname.Validate(req.Bucket); err != nil {
			return service.Response(c, fiber.StatusBadRequest, false, err.Error(), nil)
		}
		if err := i.minioClient.MakeBucket(ctx, req.Bucket, minio.MakeBucketOptions{}); err != nil {
			return service.Response(c, fiber.StatusBadRequest, false, "Bucket Not Found And Not Created!", nil)
		}
	}

	// Check if the AWS bucket exists if required
	if req.AWSUpload && !i.awsService.BucketExists(req.Bucket) {
		return service.Response(c, fiber.StatusBadRequest, false, "Bucket Not Found On Aws S3!", nil)
	}

	httpClient := validator.NewSafeHTTPClient(30 * time.Second)
	res, err := httpClient.Get(req.URL)
	if err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, err.Error(), nil)
	}
	defer res.Body.Close()

	// Read content from URL, capped at the configured max file size
	maxSize := config.GetEnvAsIntOrDefault("MAX_FILE_SIZE", int(validator.DefaultMaxFileSize))
	content, err := io.ReadAll(io.LimitReader(res.Body, int64(maxSize)))
	if err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, "Failed to read content from URL", nil)
	}

	// Automatically detect content type
	contentType := http.DetectContentType(content)

	// Opt-in optimization. Non-image / non-resizable content passes through
	// unchanged, so this runs safely before the file-type checks below. Format
	// is preserved, so the content type is re-detected to keep the invariant.
	if req.Optimize {
		optimized, ow, oh := i.maybeOptimize(content, service.DefaultOptimizeOptions())
		content = optimized
		contentType = http.DetectContentType(content)
		if ow > 0 && oh > 0 {
			c.Set("Width", strconv.Itoa(int(ow)))
			c.Set("Height", strconv.Itoa(int(oh)))
		}
	}

	// Determine file extension from content type
	extension := filetype.GetExtensionFromContentType(contentType)
	if extension == "" {
		// Try to extract extension from URL if content type is not recognized
		extension = filetype.GetExtensionFromURL(req.URL)
		if !filetype.IsValidExtension(extension) {
			return service.Response(c, fiber.StatusBadRequest, false, "Unsupported or unrecognized file type", nil)
		}
	}

	// If the resolved type is an image, the downloaded bytes must be a valid
	// image; non-image content uploads unchanged.
	if _, _, verr := i.validateImageContent("f."+extension, content); verr != nil {
		return service.Response(c, fiber.StatusBadRequest, false, "invalid image content", map[string]string{
			"code": "INVALID_IMAGE_CONTENT",
		})
	}

	randomName := uuid.New().String()
	// Sanitize extension
	sanitizedExtension := service.SanitizeObjectName(extension)
	objectName := randomName + "." + sanitizedExtension
	if req.Path != "" {
		// Sanitize path as well
		req.Path = strings.Trim(req.Path, "/")
		sanitizedPath := service.SanitizeObjectName(req.Path)
		objectName = sanitizedPath + "/" + randomName + "." + sanitizedExtension
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

	// This is the one write route with the bucket in the URL path, so a scoped
	// token deleting from another bucket is refused here.
	bucket, err := resolveBucket(c, c.Params("bucket"))
	if err != nil {
		return bucketForbidden(c)
	}
	awsDelete := c.Params("aws_delete") == "true"
	object := c.Params("*")

	if len(bucket) == 0 || len(object) == 0 {
		return service.Response(c, fiber.StatusBadRequest, false, "invalid path or bucket or file.", nil)
	}

	// Reject traversal-like keys instead of forwarding them verbatim to MinIO.
	if service.HasUnsafeObjectKey(object) {
		return service.Response(c, fiber.StatusBadRequest, false, "invalid object key", nil)
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

	// Magic-number gate before the bytes reach ImageMagick, matching the upload
	// endpoints. Without it, /resize would decode arbitrary content (a crafted
	// file with a dangerous ImageMagick coder/delegate) with no content check.
	if err := validator.ValidateFileContent(fileContent); err != nil {
		if valErr, ok := err.(*validator.FileValidationError); ok {
			return service.Response(c, fiber.StatusBadRequest, false, valErr.Message, map[string]string{
				"code": valErr.Code,
			})
		}
		return service.Response(c, fiber.StatusBadRequest, false, err.Error(), nil)
	}

	if !resize || !service.IsImageFile(file.Filename) {
		c.Set("Content-Length", strconv.Itoa(len(fileContent)))
		c.Set("Content-Type", http.DetectContentType(fileContent))
		return c.Send(fileContent)
	}

	// Create response channel
	respChan := make(chan error, 1)

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

	if err := i.workerPool.Submit(job); err != nil {
		return service.Response(c, fiber.StatusServiceUnavailable, false, "Image processing queue is full", nil)
	}

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

// BatchUpload handles multiple file uploads
func (i *image) BatchUpload(c *fiber.Ctx) error {
	form, err := c.MultipartForm()
	if err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, "Invalid form data", nil)
	}

	requestedBucket := ""
	if v := form.Value["bucket"]; len(v) > 0 {
		requestedBucket = v[0]
	}
	bucketName, err := resolveBucket(c, requestedBucket)
	if err != nil {
		return bucketForbidden(c)
	}
	if bucketName == "" {
		return service.Response(c, fiber.StatusBadRequest, false, "Bucket is required", nil)
	}

	path := form.Value["path"]
	pathPrefix := ""
	if len(path) > 0 {
		pathPrefix = path[0]
	}

	awsUpload := form.Value["aws_upload"] != nil && form.Value["aws_upload"][0] == "true"
	optimize := form.Value["optimize"] != nil && form.Value["optimize"][0] == "true"

	// Check bucket existence
	exists, err := i.minioClient.BucketExists(context.Background(), bucketName)
	if err != nil || !exists {
		return service.Response(c, fiber.StatusBadRequest, false, "Bucket not found", nil)
	}

	// Check AWS bucket if needed
	if awsUpload && !i.awsService.BucketExists(bucketName) {
		return service.Response(c, fiber.StatusBadRequest, false, "AWS bucket not found", nil)
	}

	files := form.File["files"]
	if len(files) == 0 {
		return service.Response(c, fiber.StatusBadRequest, false, "No files provided", nil)
	}

	// Cap the batch size so a huge multipart request cannot pre-allocate an
	// unbounded result channel / slice (memory DoS).
	maxBatch := config.GetEnvAsIntOrDefault("MAX_BATCH_FILES", 100)
	if maxBatch > 0 && len(files) > maxBatch {
		return service.Response(c, fiber.StatusBadRequest, false, fmt.Sprintf("Too many files in one batch (max %d)", maxBatch), nil)
	}

	results := make([]map[string]any, 0)
	var wg sync.WaitGroup
	resultChan := make(chan map[string]any, len(files))
	sem := make(chan struct{}, 10)

	for _, file := range files {
		wg.Add(1)
		sem <- struct{}{}
		go func(file *multipart.FileHeader) {
			defer func() { <-sem; wg.Done() }()

			result := make(map[string]any)
			result["filename"] = file.Filename

			// Validate file
			if err := validator.ValidateFile(file); err != nil {
				result["success"] = false
				result["error"] = err.Error()
				resultChan <- result
				return
			}

			// Process and upload file
			fileContent, err := file.Open()
			if err != nil {
				result["success"] = false
				result["error"] = err.Error()
				resultChan <- result
				return
			}
			defer fileContent.Close()

			// Generate object name
			randomName := uuid.New().String()
			// Sanitize filename
			sanitizedFilename := service.SanitizeObjectName(file.Filename)
			objectName := randomName + "_" + sanitizedFilename
			if pathPrefix != "" {
				// Sanitize path prefix as well
				sanitizedPath := service.SanitizeObjectName(pathPrefix)
				objectName = sanitizedPath + "/" + objectName
			}

			// Determine the bytes to store. Image-extension files are buffered so
			// their content can be validated as a real image (and optionally
			// optimized); non-images stream straight through, byte-identical to
			// the pre-feature behavior.
			contentType := file.Header.Get("Content-Type")
			uploadSize := file.Size
			var payload []byte
			optimized := false
			if service.IsImageFile(file.Filename) {
				raw, readErr := io.ReadAll(fileContent)
				if readErr != nil {
					result["success"] = false
					result["error"] = readErr.Error()
					resultChan <- result
					return
				}
				if _, _, verr := i.validateImageContent(file.Filename, raw); verr != nil {
					result["success"] = false
					result["error"] = "invalid image content"
					resultChan <- result
					return
				}
				if optimize {
					raw, _, _ = i.maybeOptimize(raw, service.DefaultOptimizeOptions())
					optimized = true
				}
				payload = raw
				uploadSize = int64(len(payload))
				contentType = http.DetectContentType(payload)
			}

			var minioReader io.Reader = fileContent
			if payload != nil {
				minioReader = bytes.NewReader(payload)
			}

			// Upload to MinIO
			_, err = i.minioClient.PutObject(
				context.Background(),
				bucketName,
				objectName,
				minioReader,
				uploadSize,
				minio.PutObjectOptions{ContentType: contentType},
			)

			if err != nil {
				result["success"] = false
				result["error"] = err.Error()
				resultChan <- result
				return
			}

			// Upload to AWS if requested
			if awsUpload {
				var awsReader io.Reader
				if payload != nil {
					awsReader = bytes.NewReader(payload)
				} else {
					_, _ = fileContent.Seek(0, 0)
					awsReader = fileContent
				}
				_, err = i.awsService.S3PutObject(bucketName, objectName, awsReader)
				if err != nil {
					result["aws_error"] = err.Error()
				}
			}

			result["success"] = true
			result["object_name"] = objectName
			if optimized {
				result["size"] = uploadSize
			}
			resultChan <- result
		}(file)
	}

	// Wait for all uploads to complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	for result := range resultChan {
		results = append(results, result)
	}

	return service.Response(c, fiber.StatusOK, true, "Batch upload completed", results)
}

// BatchDelete handles multiple file deletions
func (i *image) BatchDelete(c *fiber.Ctx) error {
	var req BatchDeleteRequest
	if err := c.BodyParser(&req); err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, "Invalid request body", nil)
	}

	if err := validator.ValidateStruct(req); err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, err.Error(), nil)
	}

	// Reconcile the body with the token before anything acts on req.Bucket.
	bucketName, err := resolveBucket(c, req.Bucket)
	if err != nil {
		return bucketForbidden(c)
	}
	if bucketName == "" {
		return service.Response(c, fiber.StatusBadRequest, false, "Bucket is required", nil)
	}
	req.Bucket = bucketName

	// Cap the batch size so a huge JSON array cannot pre-allocate an unbounded
	// result channel / slice (memory DoS).
	maxBatch := config.GetEnvAsIntOrDefault("MAX_BATCH_FILES", 100)
	if maxBatch > 0 && len(req.Files) > maxBatch {
		return service.Response(c, fiber.StatusBadRequest, false, fmt.Sprintf("Too many files in one batch (max %d)", maxBatch), nil)
	}

	// Check bucket existence
	exists, err := i.minioClient.BucketExists(context.Background(), req.Bucket)
	if err != nil || !exists {
		return service.Response(c, fiber.StatusBadRequest, false, "Bucket not found", nil)
	}

	// Check AWS bucket if needed
	if req.AWSDelete && !i.awsService.BucketExists(req.Bucket) {
		return service.Response(c, fiber.StatusBadRequest, false, "AWS bucket not found", nil)
	}

	results := make([]map[string]any, 0)
	var wg sync.WaitGroup
	resultChan := make(chan map[string]any, len(req.Files))
	sem := make(chan struct{}, 10)

	for _, file := range req.Files {
		wg.Add(1)
		sem <- struct{}{}
		go func(filename string) {
			defer func() { <-sem; wg.Done() }()

			result := make(map[string]any)
			result["filename"] = filename

			// Delete from MinIO
			err := i.minioClient.RemoveObject(context.Background(), req.Bucket, filename, minio.RemoveObjectOptions{})
			if err != nil {
				result["success"] = false
				result["error"] = err.Error()
				resultChan <- result
				return
			}

			// Delete from AWS if requested
			if req.AWSDelete {
				if err := i.awsService.DeleteObjects(req.Bucket, []string{filename}); err != nil {
					result["aws_error"] = err.Error()
				}
			}

			result["success"] = true
			resultChan <- result
		}(file)
	}

	// Wait for all deletions to complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	for result := range resultChan {
		results = append(results, result)
	}

	return service.Response(c, fiber.StatusOK, true, "Batch delete completed", results)
}
