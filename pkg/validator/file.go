package validator

import (
	"fmt"
	"mime/multipart"
	"path/filepath"
	"strings"

	"github.com/mstgnz/cdn/pkg/config"
)

var (
	// Default maximum file size (100MB)
	DefaultMaxFileSize int64 = 100 * 1024 * 1024

	// Allowed file formats
	AllowedImageFormats = map[string]bool{
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".gif":  true,
		".webp": true,
		".bmp":  true,
		".tiff": true,
		".svg":  true,
	}

	// Allowed MIME types
	AllowedMimeTypes = map[string]bool{
		"image/jpeg":      true,
		"image/png":       true,
		"image/gif":       true,
		"image/webp":      true,
		"image/bmp":       true,
		"image/tiff":      true,
		"image/svg+xml":   true,
		"application/pdf": true,
	}
)

// FileValidationError custom error type
type FileValidationError struct {
	Code    string
	Message string
}

func (e *FileValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// ValidateFile performs file validation
func ValidateFile(file *multipart.FileHeader) error {
	// File size check
	maxSize := config.GetEnvAsIntOrDefault("MAX_FILE_SIZE", int(DefaultMaxFileSize))
	if file.Size > int64(maxSize) {
		return &FileValidationError{
			Code:    "FILE_TOO_LARGE",
			Message: fmt.Sprintf("File size is too large. Maximum: %d bytes", maxSize),
		}
	}

	// File extension check
	ext := strings.ToLower(filepath.Ext(file.Filename))
	if !AllowedImageFormats[ext] {
		return &FileValidationError{
			Code:    "INVALID_FILE_FORMAT",
			Message: fmt.Sprintf("Invalid file format. Allowed formats: %v", getAllowedFormats()),
		}
	}

	// MIME type check
	if !AllowedMimeTypes[file.Header.Get("Content-Type")] {
		return &FileValidationError{
			Code:    "INVALID_MIME_TYPE",
			Message: fmt.Sprintf("Invalid MIME type. Allowed types: %v", getAllowedMimeTypes()),
		}
	}

	return nil
}

// ValidateFileContent validates file content
func ValidateFileContent(content []byte) error {
	// File size check
	maxSize := config.GetEnvAsIntOrDefault("MAX_FILE_SIZE", int(DefaultMaxFileSize))
	if len(content) > maxSize {
		return &FileValidationError{
			Code:    "FILE_TOO_LARGE",
			Message: fmt.Sprintf("File size is too large. Maximum: %d bytes", maxSize),
		}
	}

	// Magic number check
	if !isValidImageContent(content) {
		return &FileValidationError{
			Code:    "INVALID_FILE_CONTENT",
			Message: "Invalid file content",
		}
	}

	return nil
}

// isValidImageContent checks magic numbers
func isValidImageContent(content []byte) bool {
	if len(content) < 4 {
		return false
	}

	// Magic number checks
	magicNumbers := map[string][]byte{
		"jpeg": {0xFF, 0xD8, 0xFF},
		"png":  {0x89, 0x50, 0x4E, 0x47},
		"gif":  {0x47, 0x49, 0x46, 0x38},
		"webp": {0x52, 0x49, 0x46, 0x46},
		"bmp":  {0x42, 0x4D},
	}

	for _, magic := range magicNumbers {
		if len(content) >= len(magic) && compareBytes(content[:len(magic)], magic) {
			return true
		}
	}

	return false
}

// compareBytes compares byte arrays
func compareBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// getAllowedFormats returns allowed formats as a string
func getAllowedFormats() string {
	formats := make([]string, 0, len(AllowedImageFormats))
	for format := range AllowedImageFormats {
		formats = append(formats, format)
	}
	return strings.Join(formats, ", ")
}

// getAllowedMimeTypes returns allowed MIME types as a string
func getAllowedMimeTypes() string {
	mimeTypes := make([]string, 0, len(AllowedMimeTypes))
	for mimeType := range AllowedMimeTypes {
		mimeTypes = append(mimeTypes, mimeType)
	}
	return strings.Join(mimeTypes, ", ")
}
