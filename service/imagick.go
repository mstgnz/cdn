package service

import (
	"log"

	"gopkg.in/gographics/imagick.v3/imagick"
)

// https://github.com/gographics/imagick/tree/master/examples
// https://pkg.go.dev/gopkg.in/gographics/imagick.v3/imagick

func ImagickResize(image []byte, hWidth, hHeight uint) []byte {
	imagick.Initialize()
	// Schedule cleanup
	defer imagick.Terminate()
	var err error

	mw := imagick.NewMagickWand()

	err = mw.ReadImageBlob(image)
	if err != nil {
		log.Println(err)
		return ImagickResize(ImageToByte("./notfound.png"), hWidth, hHeight)
	}

	// Resize the image using the Lanczos filter
	// The blur factor is a float, where > 1 is blurry, < 1 is sharp
	err = mw.ResizeImage(hWidth, hHeight, imagick.FILTER_LANCZOS)
	if err != nil {
		log.Println(err)
		return ImagickResize(ImageToByte("./notfound.png"), hWidth, hHeight)
	}

	// Set the compression quality to 95 (high quality = low compression)
	err = mw.SetImageCompressionQuality(95)
	if err != nil {
		log.Println(err)
		return ImagickResize(ImageToByte("./notfound.png"), hWidth, hHeight)
	}

	// Return byte image
	return mw.GetImageBlob()

}
