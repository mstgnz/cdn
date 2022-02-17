package service

import (
	"gopkg.in/gographics/imagick.v3/imagick"
)

func ImagickResize(image []byte, hWidth, hHeight uint) []byte{
	imagick.Initialize()
	// Schedule cleanup
	defer imagick.Terminate()
	var err error

	mw := imagick.NewMagickWand()

	err = mw.ReadImageBlob(image)
	if err != nil {
		panic(err)
	}

	// Get original logo size
	//width := mw.GetImageWidth()
	//height := mw.GetImageHeight()

	// Calculate half the size
	//hWidth := uint(width / 2)
	//hHeight := uint(height / 2)

	// Resize the image using the Lanczos filter
	// The blur factor is a float, where > 1 is blurry, < 1 is sharp
	err = mw.ResizeImage(hWidth, hHeight, imagick.FILTER_LANCZOS)
	if err != nil {
		panic(err)
	}

	// Set the compression quality to 95 (high quality = low compression)
	err = mw.SetImageCompressionQuality(95)
	if err != nil {
		panic(err)
	}

	//mw.WriteImage("./restore.jpg")
	return mw.GetImageBlob()

}