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
		".heic": true, // iOS HEIC format
		".heif": true, // HEIF format
		".avif": true, // AVIF format
		// PDF files
		".pdf": true,
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
		// Video files (mobile videos)
		".mp4": true,
		".mov": true, // QuickTime from iOS
		".3gp": true, // 3GP from mobile
		".avi": true, // AVI files
	}

	// Allowed MIME types
	AllowedMimeTypes = map[string]bool{
		"image/jpeg":               true,
		"image/jpg":                true, // Alternative JPEG MIME type
		"image/pjpeg":              true, // Progressive JPEG (IE and some mobile apps)
		"image/png":                true,
		"image/x-png":              true, // Alternative PNG MIME type
		"image/gif":                true,
		"image/webp":               true,
		"image/bmp":                true,
		"image/x-ms-bmp":           true, // Microsoft BMP variant
		"image/tiff":               true,
		"image/svg+xml":            true,
		"image/heic":               true, // iOS HEIC format
		"image/heif":               true, // HEIF format
		"image/avif":               true, // AVIF format (modern browsers/mobile)
		"application/octet-stream": true, // Generic binary (some mobile apps use this)
		"application/pdf":          true,
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
		// Video MIME types (mobile videos)
		"video/mp4":       true,
		"video/quicktime": true, // MOV files from iOS
		"video/3gpp":      true, // 3GP files from mobile
		"video/x-msvideo": true, // AVI files
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
	// Check if file validation is enabled
	if !config.GetEnvAsBoolOrDefault("VALIDATE_FILE", true) {
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
	// Check if file validation is enabled
	if !config.GetEnvAsBoolOrDefault("VALIDATE_FILE", true) {
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
		// HEIC/HEIF files (check for 'ftyp' at offset 4)
		"heic": {0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70}, // ftyp header
		// AVIF files
		"avif": {0x00, 0x00, 0x00, 0x1C, 0x66, 0x74, 0x79, 0x70, 0x61, 0x76, 0x69, 0x66}, // ftypavif
		// Office documents (ZIP-based)
		"office": {0x50, 0x4B, 0x03, 0x04}, // ZIP signature for modern Office files
		// Legacy Office files
		"ole": {0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, // OLE compound document
		// Audio files
		"wav":     {0x52, 0x49, 0x46, 0x46}, // RIFF header for WAV
		"mp3":     {0xFF, 0xFB},             // MP3 frame header (most common)
		"mp3_id3": {0x49, 0x44, 0x33},       // ID3v2 header
		// Video files
		"mp4": {0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70},                   // ftyp (same as HEIC but different content)
		"3gp": {0x00, 0x00, 0x00, 0x14, 0x66, 0x74, 0x79, 0x70, 0x33, 0x67, 0x70}, // ftyp3gp
		"avi": {0x52, 0x49, 0x46, 0x46},                                           // RIFF header for AVI
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
