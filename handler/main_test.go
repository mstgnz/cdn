package handler

import (
	"os"
	"testing"

	"gopkg.in/gographics/imagick.v3/imagick"
)

// TestMain initializes the ImageMagick environment once for the whole handler
// test run (the resize path exercises ImageMagick). Mirrors how cmd/main.go
// drives the process.
func TestMain(m *testing.M) {
	imagick.Initialize()
	code := m.Run()
	imagick.Terminate()
	os.Exit(code)
}
