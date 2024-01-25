package service

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

func GetEnv(key string) string {
	return os.Getenv(key)
}

// GetBool fetches an env var meant to be a bool and follows this logic to
// determine the value of that bool:
// if "", return false
// strconv.ParseBool() otherwise:
// if that errors, exit;
// otherwise return the value
func GetBool(key string) bool {
	val := os.Getenv(key)
	v, err := strconv.ParseBool(val)
	if err != nil {
		log.Fatalf("invalid boolean environment variable '%s': %v", val, err)
	}
	return v
}

func RandomName(length int) string {
	return strconv.FormatInt(time.Now().UnixMicro(), 10)
}

func StreamToByte(stream io.Reader) []byte {
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(stream)
	return buf.Bytes()
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
	getToken := strings.Split(c.Get("Authorization"), " ")
	if len(getToken) != 2 || !strings.EqualFold(getToken[1], GetEnv("TOKEN")) {
		return errors.New("invalid token")
	}
	return nil
}

func Response(c *fiber.Ctx, code int, status bool, message string) error {
	return c.Status(code).JSON(fiber.Map{
		"status":  status,
		"message": message,
	})
}
