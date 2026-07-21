package handler

import (
	"testing"

	"github.com/mstgnz/cdn/service"
)

// TestValidateImageContent covers the rule that a file whose extension marks it
// an image must actually be a valid image, while non-image files pass through.
func TestValidateImageContent(t *testing.T) {
	img := image{imageService: &service.ImageService{}}
	valid := makePNG(t, 32, 24)

	t.Run("valid png returns dims", func(t *testing.T) {
		w, h, err := img.validateImageContent("photo.png", valid)
		if err != nil {
			t.Fatalf("valid png rejected: %v", err)
		}
		if w != 32 || h != 24 {
			t.Fatalf("dims = %dx%d, want 32x24", w, h)
		}
	})

	t.Run("image extension with non-image content rejected", func(t *testing.T) {
		if _, _, err := img.validateImageContent("fake.png", []byte("this is not an image")); err == nil {
			t.Fatal("expected rejection for non-image content with image extension")
		}
	})

	t.Run("non-image extension passes through", func(t *testing.T) {
		if _, _, err := img.validateImageContent("notes.pdf", []byte("%PDF-1.4 not really")); err != nil {
			t.Fatalf("non-image passthrough rejected: %v", err)
		}
	})

	t.Run("valid svg accepted", func(t *testing.T) {
		svg := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`)
		if _, _, err := img.validateImageContent("icon.svg", svg); err != nil {
			t.Fatalf("valid svg rejected: %v", err)
		}
	})

	t.Run("non-svg content with svg extension rejected", func(t *testing.T) {
		if _, _, err := img.validateImageContent("icon.svg", []byte("just text, no root element")); err == nil {
			t.Fatal("expected rejection for non-svg content with .svg extension")
		}
	})
}

// TestValidateImageContent_DisabledSkips ensures the VALIDATE_FILE toggle turns
// the check off (never rejects) when validation is globally disabled.
func TestValidateImageContent_DisabledSkips(t *testing.T) {
	t.Setenv("VALIDATE_FILE", "false")
	img := image{imageService: &service.ImageService{}}
	if _, _, err := img.validateImageContent("fake.png", []byte("not an image")); err != nil {
		t.Fatalf("with VALIDATE_FILE=false, must not reject: %v", err)
	}
}
