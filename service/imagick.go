package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"

	"github.com/minio/minio-go/v7"
	"gopkg.in/gographics/imagick.v3/imagick"
)

// https://github.com/gographics/imagick/tree/master/examples
// https://pkg.go.dev/gopkg.in/gographics/imagick.v3/imagick

type ImageService struct {
	MinioClient *minio.Client
}

func (s *ImageService) ImagickGetWidthHeight(image []byte) (error, uint, uint) {
	imagick.Initialize()
	defer imagick.Terminate()

	mw := imagick.NewMagickWand()
	defer mw.Destroy()

	if err := mw.ReadImageBlob(image); err != nil {
		return err, 0, 0
	}
	return nil, mw.GetImageWidth(), mw.GetImageHeight()
}

func (s *ImageService) ImagickFormat(image []byte) (error, string) {
	imagick.Initialize()
	defer imagick.Terminate()

	mw := imagick.NewMagickWand()
	defer mw.Destroy()

	if err := mw.ReadImageBlob(image); err != nil {
		return err, ""
	}

	return nil, mw.GetImageFormat()
}

func (s *ImageService) ImagickResize(image []byte, targetWidth, targetHeight uint) []byte {
	imagick.Initialize()
	defer imagick.Terminate()

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
	imagick.Initialize()
	defer imagick.Terminate()

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

// ProcessImage processes an image (resize, optimize, etc.)
func (s *ImageService) ProcessImage(data []byte) ([]byte, error) {
	imagick.Initialize()
	defer imagick.Terminate()

	mw := imagick.NewMagickWand()
	defer mw.Destroy()

	// Read the image data
	if err := mw.ReadImageBlob(data); err != nil {
		return nil, fmt.Errorf("failed to read image data: %w", err)
	}

	// Get original dimensions
	width := mw.GetImageWidth()
	height := mw.GetImageHeight()

	// Calculate new dimensions (max 2000x2000)
	maxDim := uint(2000)
	if width > maxDim || height > maxDim {
		aspect := float64(width) / float64(height)
		if width > height {
			width = maxDim
			height = uint(float64(maxDim) / aspect)
		} else {
			height = maxDim
			width = uint(float64(maxDim) * aspect)
		}

		// Resize the image
		if err := mw.ResizeImage(width, height, imagick.FILTER_LANCZOS); err != nil {
			return nil, fmt.Errorf("failed to resize image: %w", err)
		}
	}

	// Strip metadata
	if err := mw.StripImage(); err != nil {
		log.Printf("Warning: Failed to strip image metadata: %v", err)
	}

	// Optimize quality based on format
	format := mw.GetImageFormat()
	switch format {
	case "JPEG":
		if err := mw.SetImageCompressionQuality(85); err != nil {
			log.Printf("Warning: Failed to set JPEG quality: %v", err)
		}
	case "PNG":
		if err := mw.SetImageCompressionQuality(95); err != nil {
			log.Printf("Warning: Failed to set PNG quality: %v", err)
		}
	case "WEBP":
		if err := mw.SetImageCompressionQuality(80); err != nil {
			log.Printf("Warning: Failed to set WebP quality: %v", err)
		}
	}

	// Get the processed image data
	processed := mw.GetImageBlob()
	if len(processed) == 0 {
		return nil, fmt.Errorf("failed to get processed image data")
	}

	return processed, nil
}

// ResizeImage resizes an image to the specified dimensions
func (s *ImageService) ResizeImage(data []byte, width, height int) ([]byte, error) {
	imagick.Initialize()
	defer imagick.Terminate()

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
