package filetype

import "strings"

// GetExtensionFromContentType returns file extension based on content type
func GetExtensionFromContentType(contentType string) string {
	switch contentType {
	// Images
	case "image/jpeg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	case "image/svg+xml":
		return "svg"
	// Documents
	case "application/pdf":
		return "pdf"
	case "application/msword":
		return "doc"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return "docx"
	case "application/vnd.ms-excel":
		return "xls"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return "xlsx"
	case "application/vnd.ms-powerpoint":
		return "ppt"
	case "application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return "pptx"
	// CAD Files
	case "application/x-autocad", "application/acad", "image/vnd.dwg", "image/x-dwg":
		return "dwg"
	case "application/dxf":
		return "dxf"
	// Archives
	case "application/zip":
		return "zip"
	case "application/x-rar-compressed":
		return "rar"
	case "application/x-7z-compressed":
		return "7z"
	// Text files
	case "text/plain":
		return "txt"
	case "text/csv":
		return "csv"
	default:
		return ""
	}
}

// GetExtensionFromURL extracts file extension from URL
func GetExtensionFromURL(url string) string {
	parts := strings.Split(url, ".")
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}
	return ""
}

// IsValidExtension checks if the given extension is supported
func IsValidExtension(extension string) bool {
	validExtensions := map[string]bool{
		"jpg":  true,
		"jpeg": true,
		"png":  true,
		"gif":  true,
		"webp": true,
		"svg":  true,
		"pdf":  true,
		"doc":  true,
		"docx": true,
		"xls":  true,
		"xlsx": true,
		"ppt":  true,
		"pptx": true,
		"dwg":  true,
		"dxf":  true,
		"zip":  true,
		"rar":  true,
		"7z":   true,
		"txt":  true,
		"csv":  true,
	}

	return validExtensions[strings.ToLower(extension)]
}
