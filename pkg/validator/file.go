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
	AllowedFileFormats = map[string]bool{
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".gif":  true,
		".webp": true,
		".bmp":  true,
		".tiff": true,
		".svg":  true,
		// Excel files
		".xls":  true,
		".xlsx": true,
		// PowerPoint files
		".ppt":  true,
		".pptx": true,
		// SQL files
		".sql": true,
		// Audio files
		".wav": true,
		".mp3": true,
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
		// Excel MIME types
		"application/vnd.ms-excel": true,
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": true,
		// PowerPoint MIME types
		"application/vnd.ms-powerpoint":                                             true,
		"application/vnd.openxmlformats-officedocument.presentationml.presentation": true,
		// SQL MIME types
		"text/plain":      true,
		"application/sql": true,
		// Audio MIME types
		"audio/wav":  true,
		"audio/wave": true,
		"audio/mpeg": true,
		"audio/mp3":  true,
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
	if config.GetEnvAsBoolOrDefault("DISABLE_VALIDATION", false) {
		return nil
	}

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
	if !AllowedFileFormats[ext] {
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
	if config.GetEnvAsBoolOrDefault("DISABLE_VALIDATION", false) {
		return nil
	}

	// File size check
	maxSize := config.GetEnvAsIntOrDefault("MAX_FILE_SIZE", int(DefaultMaxFileSize))
	if len(content) > maxSize {
		return &FileValidationError{
			Code:    "FILE_TOO_LARGE",
			Message: fmt.Sprintf("File size is too large. Maximum: %d bytes", maxSize),
		}
	}

	// Magic number check
	if !isValidFileContent(content) {
		return &FileValidationError{
			Code:    "INVALID_FILE_CONTENT",
			Message: "Invalid file content",
		}
	}

	return nil
}

// isValidFileContent checks magic numbers
func isValidFileContent(content []byte) bool {
	if len(content) < 4 {
		// For very small files, check if it's valid UTF-8 text (for SQL files)
		return isValidUTF8Text(content)
	}

	// Magic number checks
	magicNumbers := map[string][]byte{
		"jpeg": {0xFF, 0xD8, 0xFF},
		"png":  {0x89, 0x50, 0x4E, 0x47},
		"gif":  {0x47, 0x49, 0x46, 0x38},
		"webp": {0x52, 0x49, 0x46, 0x46},
		"bmp":  {0x42, 0x4D},
		// Office documents (ZIP-based)
		"office": {0x50, 0x4B, 0x03, 0x04}, // ZIP signature for modern Office files
		// Legacy Office files
		"ole": {0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, // OLE compound document
		// Audio files
		"wav":     {0x52, 0x49, 0x46, 0x46}, // RIFF header for WAV
		"mp3":     {0xFF, 0xFB},             // MP3 frame header (most common)
		"mp3_id3": {0x49, 0x44, 0x33},       // ID3v2 header
		// PDF files
		"pdf": {0x25, 0x50, 0x44, 0x46}, // %PDF
	}

	for _, magic := range magicNumbers {
		if len(content) >= len(magic) && compareBytes(content[:len(magic)], magic) {
			return true
		}
	}

	// If no magic number matches, check if it's valid UTF-8 text (for SQL files)
	return isValidUTF8Text(content)
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
	formats := make([]string, 0, len(AllowedFileFormats))
	for format := range AllowedFileFormats {
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

// isValidUTF8Text checks if the content is valid UTF-8 text
func isValidUTF8Text(content []byte) bool {
	// Check if content is valid UTF-8
	if !isValidUTF8(content) {
		return false
	}

	// Additional checks for text files
	if len(content) == 0 {
		return true // Empty files are valid
	}

	// Check for null bytes (binary indicator)
	for _, b := range content {
		if b == 0 {
			return false
		}
	}

	return true
}

// isValidUTF8 checks if bytes are valid UTF-8
func isValidUTF8(content []byte) bool {
	for i := 0; i < len(content); {
		if content[i] < 0x80 {
			i++
			continue
		}

		var size int
		if content[i]&0xE0 == 0xC0 {
			size = 2
		} else if content[i]&0xF0 == 0xE0 {
			size = 3
		} else if content[i]&0xF8 == 0xF0 {
			size = 4
		} else {
			return false
		}

		if i+size > len(content) {
			return false
		}

		for j := 1; j < size; j++ {
			if content[i+j]&0xC0 != 0x80 {
				return false
			}
		}

		i += size
	}

	return true
}
