package service

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
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
	return strconv.FormatInt(time.Now().UnixMicro(), 10)
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

func CheckToken(c *fiber.Ctx) error {
	authHeader := c.Get("Authorization")
	if authHeader == "" {
		return errors.New("no token provided")
	}

	getToken := strings.Split(authHeader, " ")
	if len(getToken) != 2 {
		return errors.New("invalid authorization format")
	}

	// Temizle - satır sonu karakterlerini kaldır
	clientToken := strings.TrimSpace(getToken[1])
	serverToken := strings.TrimSpace(config.GetEnvOrDefault("TOKEN", ""))

	if clientToken != serverToken {
		return errors.New(fmt.Sprintf("invalid token: Authorization: %s TOKEN: %s", getToken[1], config.GetEnvOrDefault("TOKEN", "null")))
	}
	return nil
}

func Response(c *fiber.Ctx, code int, success bool, message string, data any) error {
	return c.Status(code).JSON(fiber.Map{
		"success": success,
		"message": message,
		"data":    data,
	})
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
