package service

import (
	"log"

	"gopkg.in/gographics/imagick.v3/imagick"
)

// https://github.com/gographics/imagick/tree/master/examples
// https://pkg.go.dev/gopkg.in/gographics/imagick.v3/imagick

func ImagickGetWidthHeight(image []byte) (error, uint, uint) {
	imagick.Initialize()
	defer imagick.Terminate()

	mw := imagick.NewMagickWand()
	defer mw.Destroy()

	if err := mw.ReadImageBlob(image); err != nil {
		return err, 0, 0
	}
	return nil, mw.GetImageWidth(), mw.GetImageHeight()
}

func ImagickResize(image []byte, targetWidth, targetHeight uint) []byte {
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
	ratio := width / height

	if targetWidth == 0 {
		targetWidth = width
	}

	if targetHeight == 0 {
		targetHeight = height
	}

	newWidth := targetHeight * ratio
	newHeight := targetWidth / ratio

	// Resize the image using the Lanczos filter
	// The blur factor is a float, where > 1 is blurry, < 1 is sharp
	err = mw.ResizeImage(newWidth, newHeight, imagick.FILTER_LANCZOS)
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
