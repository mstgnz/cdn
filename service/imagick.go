package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"

	"github.com/minio/minio-go/v7"
	"gopkg.in/gographics/imagick.v3/imagick"

	"github.com/mstgnz/cdn/pkg/config"
)

// https://github.com/gographics/imagick/tree/master/examples
// https://pkg.go.dev/gopkg.in/gographics/imagick.v3/imagick
//
// NOTE: imagick.Initialize() / imagick.Terminate() are process-wide
// (MagickWandGenesis / MagickWandTerminus) and are called exactly once in
// cmd/main.go (and once per test run via TestMain). They must NOT be called
// per operation: doing so re-initializes the global environment on every
// request and is not reentrant, which corrupts state and crashes under
// concurrent load. Each operation only creates and destroys its own wand.

type ImageService struct {
	MinioClient *minio.Client
}

// ApplyImagickResourceLimits sets process-wide ImageMagick resource limits so a
// decompression bomb or an oversized resize target cannot exhaust host memory,
// disk or CPU. MagickSetResourceLimit is global despite hanging off a wand.
// Must be called once after imagick.Initialize(). Defaults are generous enough
// that no legitimate stored image starts failing; when a limit is exceeded the
// resize paths already fall back to serving the original bytes.
//
// Unit note: the C API takes bytes for MEMORY/MAP/DISK, pixels for AREA and
// seconds for TIME. We log the effective values so they can be eyeballed on the
// first boot.
func ApplyImagickResourceLimits() {
	mw := imagick.NewMagickWand()
	defer mw.Destroy()

	limits := []struct {
		name  string
		rtype imagick.ResourceType
		value int64
	}{
		{"memory", imagick.RESOURCE_MEMORY, int64(config.GetEnvAsIntOrDefault("IMAGICK_MEMORY_LIMIT_MB", 512)) * 1024 * 1024},
		{"map", imagick.RESOURCE_MAP, int64(config.GetEnvAsIntOrDefault("IMAGICK_MAP_LIMIT_MB", 1024)) * 1024 * 1024},
		{"area", imagick.RESOURCE_AREA, int64(config.GetEnvAsIntOrDefault("IMAGICK_AREA_LIMIT_MP", 256)) * 1000 * 1000},
		{"disk", imagick.RESOURCE_DISK, int64(config.GetEnvAsIntOrDefault("IMAGICK_DISK_LIMIT_MB", 2048)) * 1024 * 1024},
		{"time", imagick.RESOURCE_TIME, int64(config.GetEnvAsIntOrDefault("IMAGICK_TIME_LIMIT_SEC", 60))},
	}

	for _, l := range limits {
		if err := mw.SetResourceLimit(l.rtype, l.value); err != nil {
			log.Printf("Warning: failed to set ImageMagick %s limit: %v", l.name, err)
			continue
		}
		log.Printf("ImageMagick %s limit set: requested=%d effective=%d", l.name, l.value, imagick.GetResourceLimit(l.rtype))
	}
}

func (s *ImageService) ImagickGetWidthHeight(image []byte) (error, uint, uint) {
	mw := imagick.NewMagickWand()
	defer mw.Destroy()

	if err := mw.ReadImageBlob(image); err != nil {
		return err, 0, 0
	}
	return nil, mw.GetImageWidth(), mw.GetImageHeight()
}

func (s *ImageService) ImagickFormat(image []byte) (error, string) {
	mw := imagick.NewMagickWand()
	defer mw.Destroy()

	if err := mw.ReadImageBlob(image); err != nil {
		return err, ""
	}

	return nil, mw.GetImageFormat()
}

func (s *ImageService) ImagickResize(image []byte, targetWidth, targetHeight uint) []byte {
	mw := imagick.NewMagickWand()
	defer mw.Destroy()

	var err error

	err = mw.ReadImageBlob(image)
	if err != nil {
		log.Println("Error reading image:", err)
		return image
	}

	width := mw.GetImageWidth()
	height := mw.GetImageHeight()

	targetWidth, targetHeight = RatioWidthHeight(width, height, targetWidth, targetHeight)

	// Resize the image using the Lanczos filter
	// The blur factor is a float, where > 1 is blurry, < 1 is sharp
	err = mw.ResizeImage(targetWidth, targetHeight, imagick.FILTER_LANCZOS)
	if err != nil {
		log.Println("Error resizing image:", err)
		return image
	}

	// Set the compression quality to 95 (high quality = low compression)
	err = mw.SetImageCompressionQuality(95)
	if err != nil {
		log.Println("Error setting compression quality:", err)
		return image
	}

	// Return byte image
	return mw.GetImageBlob()

}

// IsImageFile checks if the file is a valid image by examining its content (magic bytes)
func (s *ImageService) IsImageFile(data []byte) bool {
	if len(data) < 4 {
		return false
	}

	// Check magic bytes for common image formats
	switch {
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}): // JPEG
		return true
	case bytes.HasPrefix(data, []byte{0x89, 0x50, 0x4E, 0x47}): // PNG
		return true
	case bytes.HasPrefix(data, []byte{0x47, 0x49, 0x46}): // GIF
		return true
	case bytes.HasPrefix(data, []byte{0x42, 0x4D}): // BMP
		return true
	case bytes.HasPrefix(data, []byte{0x00, 0x00, 0x01, 0x00}): // ICO
		return true
	case bytes.HasPrefix(data, []byte{0x52, 0x49, 0x46, 0x46}): // WEBP
		return true
	default:
		return false
	}
}

// GetImageInfo returns width, height and format of an image
func (s *ImageService) GetImageInfo(data []byte) (width uint, height uint, format string, err error) {
	mw := imagick.NewMagickWand()
	defer mw.Destroy()

	if err := mw.ReadImageBlob(data); err != nil {
		return 0, 0, "", fmt.Errorf("failed to read image: %w", err)
	}

	return mw.GetImageWidth(), mw.GetImageHeight(), mw.GetImageFormat(), nil
}

// IsResizable checks if the file is a valid image that can be resized
func (s *ImageService) IsResizable(data []byte) bool {
	// First check if it's a valid image
	if !s.IsImageFile(data) {
		return false
	}

	// Get image info
	_, _, format, err := s.GetImageInfo(data)
	if err != nil {
		log.Printf("Warning: Failed to get image info: %v", err)
		return false
	}

	// Check if format supports resizing
	switch format {
	case "JPEG", "PNG", "GIF", "WEBP":
		return true
	default:
		return false
	}
}

// OptimizeOptions controls a single-pass re-encode used to shrink an uploaded
// image without visible quality loss.
type OptimizeOptions struct {
	MaxDimension uint // cap on the longest side; 0 = no cap
	TargetWidth  uint // explicit dimensions; when either is set they override MaxDimension
	TargetHeight uint
	JPEGQuality  uint
	PNGQuality   uint // lossless: ImageMagick maps PNG "quality" to zlib level/filter
	WebPQuality  uint
	Strip        bool // remove metadata (EXIF/ICC/etc.)
}

// DefaultOptimizeOptions builds the opt-in upload optimization options from the
// OPTIMIZE_* environment variables, falling back to visually-lossless defaults.
func DefaultOptimizeOptions() OptimizeOptions {
	return OptimizeOptions{
		MaxDimension: uint(config.GetEnvAsIntOrDefault("OPTIMIZE_MAX_DIMENSION", 2560)),
		JPEGQuality:  uint(config.GetEnvAsIntOrDefault("OPTIMIZE_JPEG_QUALITY", 85)),
		PNGQuality:   uint(config.GetEnvAsIntOrDefault("OPTIMIZE_PNG_QUALITY", 95)),
		WebPQuality:  uint(config.GetEnvAsIntOrDefault("OPTIMIZE_WEBP_QUALITY", 85)),
		Strip:        true,
	}
}

// OptimizeImage re-encodes a resizable raster image in a single decode/encode
// pass: optional downscale (explicit target dims win over MaxDimension),
// metadata strip, and per-format quality. It returns the encoded bytes and the
// final dimensions.
//
// Inputs that are not resizable rasters (per IsResizable) and animated-capable
// GIFs are returned unchanged with a nil error, so callers can treat any
// success as "safe to store". GIF is passed through because a single-frame
// resize would flatten animations.
func (s *ImageService) OptimizeImage(data []byte, opts OptimizeOptions) ([]byte, uint, uint, error) {
	if !s.IsResizable(data) {
		return data, 0, 0, nil
	}

	mw := imagick.NewMagickWand()
	defer mw.Destroy()

	if err := mw.ReadImageBlob(data); err != nil {
		return nil, 0, 0, fmt.Errorf("failed to read image data: %w", err)
	}

	// Preserve animated GIFs untouched.
	if mw.GetImageFormat() == "GIF" {
		return data, mw.GetImageWidth(), mw.GetImageHeight(), nil
	}

	origWidth := mw.GetImageWidth()
	origHeight := mw.GetImageHeight()
	newWidth, newHeight := origWidth, origHeight

	switch {
	case opts.TargetWidth > 0 || opts.TargetHeight > 0:
		newWidth, newHeight = s.calculateDimensions(origWidth, origHeight, opts.TargetWidth, opts.TargetHeight)
	case opts.MaxDimension > 0 && (origWidth > opts.MaxDimension || origHeight > opts.MaxDimension):
		aspect := float64(origWidth) / float64(origHeight)
		if origWidth >= origHeight {
			newWidth = opts.MaxDimension
			newHeight = uint(float64(opts.MaxDimension) / aspect)
		} else {
			newHeight = opts.MaxDimension
			newWidth = uint(float64(opts.MaxDimension) * aspect)
		}
	}

	if newWidth != origWidth || newHeight != origHeight {
		if err := mw.ResizeImage(newWidth, newHeight, imagick.FILTER_LANCZOS); err != nil {
			return nil, 0, 0, fmt.Errorf("failed to resize image: %w", err)
		}
	}

	if opts.Strip {
		if err := mw.StripImage(); err != nil {
			log.Printf("Warning: Failed to strip image metadata: %v", err)
		}
	}

	quality := uint(0)
	switch mw.GetImageFormat() {
	case "JPEG":
		quality = opts.JPEGQuality
	case "PNG":
		quality = opts.PNGQuality
	case "WEBP":
		quality = opts.WebPQuality
	}
	if quality > 0 {
		if err := mw.SetImageCompressionQuality(quality); err != nil {
			log.Printf("Warning: Failed to set compression quality: %v", err)
		}
	}

	processed := mw.GetImageBlob()
	if len(processed) == 0 {
		return nil, 0, 0, fmt.Errorf("failed to get processed image data")
	}

	return processed, mw.GetImageWidth(), mw.GetImageHeight(), nil
}

// ProcessImage processes an image (resize, optimize, etc.). Retained for
// backward compatibility; it is a thin wrapper over OptimizeImage with the
// original caps and per-format quality values.
func (s *ImageService) ProcessImage(data []byte) ([]byte, error) {
	out, _, _, err := s.OptimizeImage(data, OptimizeOptions{
		MaxDimension: 2000,
		JPEGQuality:  85,
		PNGQuality:   95,
		WebPQuality:  80,
		Strip:        true,
	})
	return out, err
}

// ResizeImage resizes an image to the specified dimensions
func (s *ImageService) ResizeImage(data []byte, width, height int) ([]byte, error) {
	// Verify that the input is a valid, resizable image
	if !s.IsResizable(data) {
		return nil, fmt.Errorf("file is not a resizable image")
	}

	mw := imagick.NewMagickWand()
	defer mw.Destroy()

	// Read the image data
	if err := mw.ReadImageBlob(data); err != nil {
		return nil, fmt.Errorf("failed to read image data: %w", err)
	}

	// Get original dimensions
	origWidth := mw.GetImageWidth()
	origHeight := mw.GetImageHeight()

	// Calculate target dimensions maintaining aspect ratio
	targetWidth, targetHeight := s.calculateDimensions(origWidth, origHeight, uint(width), uint(height))

	// Resize the image
	if err := mw.ResizeImage(targetWidth, targetHeight, imagick.FILTER_LANCZOS); err != nil {
		return nil, fmt.Errorf("failed to resize image: %w", err)
	}

	// Get the resized image data
	resized := mw.GetImageBlob()
	if len(resized) == 0 {
		return nil, fmt.Errorf("failed to get resized image data")
	}

	return resized, nil
}

// calculateDimensions calculates target dimensions while maintaining aspect ratio
func (s *ImageService) calculateDimensions(origWidth, origHeight, targetWidth, targetHeight uint) (uint, uint) {
	if targetWidth == 0 && targetHeight == 0 {
		return origWidth, origHeight
	}

	aspect := float64(origWidth) / float64(origHeight)

	if targetWidth == 0 {
		targetWidth = uint(float64(targetHeight) * aspect)
	} else if targetHeight == 0 {
		targetHeight = uint(float64(targetWidth) / aspect)
	}

	return targetWidth, targetHeight
}

// GetImage gets an image from storage and optionally resizes it
func (s *ImageService) GetImage(bucket, objectName string, width, height int) ([]byte, error) {
	// Get the image data
	data, err := s.MinioClient.GetObject(context.Background(), bucket, objectName, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get object: %w", err)
	}

	// Read the data into memory
	imageData, err := io.ReadAll(data)
	if err != nil {
		return nil, fmt.Errorf("failed to read object data: %w", err)
	}

	// Check if it's a valid image before processing
	if !s.IsImageFile(imageData) {
		return imageData, nil // Return original file if not an image
	}

	// If no resize is requested, return original
	if width == 0 && height == 0 {
		return imageData, nil
	}

	// Check if the image can be resized
	if !s.IsResizable(imageData) {
		return imageData, nil
	}

	// Resize the image
	resized, err := s.ResizeImage(imageData, width, height)
	if err != nil {
		return nil, fmt.Errorf("failed to resize image: %w", err)
	}

	return resized, nil
}
