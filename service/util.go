package service

import (
	"bufio"
	"bytes"
	"crypto/subtle"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/joho/godotenv"
	"github.com/mstgnz/cdn/pkg/config"
)

const (
	ParamsType  = "params"
	FormsType   = "forms"
	HeadersType = "headers"
	QueryType   = "query"
)

func ReadEnvAndSet() error {
	envs, err := godotenv.Read(".env")
	if err != nil {
		return err
	}
	for k, v := range envs {
		err := os.Setenv(k, v)
		if err != nil {
			return err
		}
	}
	return nil
}

func RandomName(length int) string {
	return fmt.Sprintf("%d_%d", time.Now().UnixMicro(), rand.Intn(1000000))
}

func StreamToByte(stream io.Reader) []byte {
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(stream)
	return buf.Bytes()
}

func ByteToStream(data []byte) io.Reader {
	return bytes.NewReader(data)
}

func ImageToByte(img string) []byte {
	file, err := os.Open(img)
	if err != nil {
		log.Println(err)
	}
	defer func(file *os.File) {
		_ = file.Close()
	}(file)
	buffer := bufio.NewReader(file)
	return StreamToByte(buffer)
}

func SetWidthToHeight(width, height string) (string, string) {
	if len(width) > 0 && len(height) == 0 {
		height = width
	}
	if len(height) > 0 && len(width) == 0 {
		width = height
	}
	return width, height
}

func IsInt(one, two string) bool {
	_, oneErr := strconv.Atoi(one)
	_, twoErr := strconv.Atoi(two)
	return !(oneErr != nil && twoErr != nil)
}

func DownloadFile(filepath string, url string) error {

	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer func(out *os.File) {
		_ = out.Close()
	}(out)

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	return err
}

// CheckToken authenticates a request against the general TOKEN only. A
// bucket-scoped token is rejected here, which is what gates the operator routes
// (/aws, /minio, /monitor, /metrics): those act on any bucket or expose
// service-wide data, so a credential limited to a single bucket must not reach
// them. Bucket-aware routes use ResolvePrincipal instead.
func CheckToken(c *fiber.Ctx) error {
	raw, err := BearerToken(c)
	if err != nil {
		return err
	}

	// Never echo the provided or server token back to the caller.
	if !TokenValid(raw) {
		return ErrInvalidToken
	}
	return nil
}

// TokenValid reports whether clientToken matches the configured server TOKEN
// using a constant-time comparison. An empty token on either side is never
// valid (an empty server token must not authenticate an empty client token).
// Use this where the token arrives outside the Authorization header, e.g. a
// WebSocket query parameter that browsers cannot set as a header.
func TokenValid(clientToken string) bool {
	clientToken = strings.TrimSpace(clientToken)
	serverToken := strings.TrimSpace(config.GetEnvOrDefault("TOKEN", ""))
	if clientToken == "" || serverToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(clientToken), []byte(serverToken)) == 1
}

func Response(c *fiber.Ctx, code int, success bool, message string, data any) error {
	return c.Status(code).JSON(fiber.Map{
		"success": success,
		"message": message,
		"data":    data,
	})
}

// HasUnsafeObjectKey reports whether an object key is empty or contains a ".."
// path segment. Object stores treat keys opaquely, but rejecting traversal-like
// segments keeps read/delete keys aligned with the sanitized keys upload writes
// and avoids surprises if a key is ever mapped onto a filesystem path.
func HasUnsafeObjectKey(key string) bool {
	if strings.TrimSpace(key) == "" {
		return true
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

func IsImageFile(filename string) bool {
	ext := filepath.Ext(filename)

	ext = strings.ToLower(ext)

	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".tiff", ".tif", ".webp", ".svg", ".ico", ".raw":
		return true
	}

	return false
}

func GetWidthAndHeight(c *fiber.Ctx, requestType string) (bool, uint, uint) {
	width, height := 0, 0
	resize := false

	switch requestType {
	case ParamsType:
		if getWidth, err := strconv.Atoi(c.Params("width")); err == nil {
			width = getWidth
		}
		if getHeight, err := strconv.Atoi(c.Params("height")); err == nil {
			height = getHeight
		}
	case FormsType:
		if getWidth, err := strconv.Atoi(c.FormValue("width")); err == nil {
			width = getWidth
		}
		if getHeight, err := strconv.Atoi(c.FormValue("height")); err == nil {
			height = getHeight
		}
	case HeadersType:
		if getWidth, err := strconv.Atoi(c.Get("width")); err == nil {
			width = getWidth
		}
		if getHeight, err := strconv.Atoi(c.Get("height")); err == nil {
			height = getHeight
		}
	case QueryType:
		if getWidth, err := strconv.Atoi(c.Query("width")); err == nil {
			width = getWidth
		}
		if getHeight, err := strconv.Atoi(c.Query("height")); err == nil {
			height = getHeight
		}
	}

	// Clamp requested dimensions. Negative values are floored to 0 (a negative
	// int cast to uint would otherwise wrap to a huge value and allocate a giant
	// canvas), and both axes are capped so the unauthenticated GET resize path
	// cannot be driven to exhaust memory with e.g. w:99999/h:99999.
	maxDim := config.GetEnvAsIntOrDefault("MAX_RESIZE_DIMENSION", 4096)
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	if maxDim > 0 {
		if width > maxDim {
			width = maxDim
		}
		if height > maxDim {
			height = maxDim
		}
	}

	if width > 0 || height > 0 {
		resize = true
	}

	return resize, uint(width), uint(height)
}

func CreateFile(file []byte) (*os.File, error) {
	tempFile, err := os.CreateTemp("", "create_image_*.png")
	if err != nil {
		return tempFile, err
	}

	// Write the resized content to the temporary file
	_, err = tempFile.Write(file)
	if err != nil {
		return tempFile, err
	}

	// Seek back to the beginning of the file
	_, err = tempFile.Seek(0, 0)
	if err != nil {
		return tempFile, err
	}
	return tempFile, nil
}

func RatioWidthHeight(width, height, targetWidth, targetHeight uint) (uint, uint) {
	whRatio := float64(width) / float64(height)
	hwRatio := float64(height) / float64(width)

	if targetWidth == 0 {
		targetWidth = uint(float64(targetHeight) * whRatio)
	}

	if targetHeight == 0 {
		targetHeight = uint(float64(targetWidth) * hwRatio)
	}

	return targetWidth, targetHeight
}

// SanitizeObjectName removes or replaces unsupported characters for MinIO object names
func SanitizeObjectName(objectName string) string {
	// Replace unsupported characters with safe alternatives
	replacements := map[rune]string{
		'^':  "",
		'*':  "",
		'|':  "",
		'\\': "",
		'&':  "",
		'"':  "",
		';':  "",
		// Unicode characters that might cause issues
		'ç': "c",
		'ğ': "g",
		'ı': "i",
		'ö': "o",
		'ş': "s",
		'ü': "u",
		'Ç': "C",
		'Ğ': "G",
		'İ': "I",
		'Ö': "O",
		'Ş': "S",
		'Ü': "U",
		'ë': "e",
		'ñ': "n",
		'é': "e",
		'è': "e",
		'ê': "e",
		'à': "a",
		'á': "a",
		'â': "a",
		'ô': "o",
		'ú': "u",
		'ù': "u",
		'î': "i",
		'ï': "i",
		// Space and other whitespace characters
		' ':  "_",
		'\t': "_",
		'\n': "_",
		'\r': "_",
	}

	var result strings.Builder
	for _, char := range objectName {
		if replacement, exists := replacements[char]; exists {
			result.WriteString(replacement)
		} else if char < 32 || char > 126 { // Non-printable ASCII characters
			// Skip non-printable characters
			continue
		} else {
			result.WriteRune(char)
		}
	}

	sanitized := result.String()

	// Remove multiple consecutive underscores
	for strings.Contains(sanitized, "__") {
		sanitized = strings.ReplaceAll(sanitized, "__", "_")
	}

	// Remove leading/trailing underscores and dots
	sanitized = strings.Trim(sanitized, "_.")

	// Ensure the name is not empty
	if sanitized == "" {
		sanitized = "file"
	}

	return sanitized
}
