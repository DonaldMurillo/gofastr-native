// Package barcode defines image-based QR and barcode scanning.
package barcode

import (
	"errors"
	"image"
	"unicode/utf8"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/oned"
	"github.com/makiuchi-d/gozxing/qrcode"
)

type Source string

const (
	Camera Source = "camera"
	Image  Source = "image"
)

type Format string

const (
	QRCode  Format = "QR_CODE"
	Code128 Format = "CODE_128"
	EAN13   Format = "EAN_13"
	EAN8    Format = "EAN_8"
	UPCA    Format = "UPC_A"
	UPCE    Format = "UPC_E"

	MaxDimension = 2048
	maxTextBytes = 16 << 10
)

// Request asks for one code from a camera photo or a selected image.
type Request struct {
	Source Source `json:"source"`
}

func (request Request) Validate() error {
	if request.Source != Camera && request.Source != Image {
		return errors.New("barcode: choose the camera or image source")
	}
	return nil
}

// Result holds decoded text, which must be treated as untrusted input.
type Result struct {
	Format Format `json:"format"`
	Text   string `json:"text"`
}

func (result Result) Validate() error {
	switch result.Format {
	case QRCode, Code128, EAN13, EAN8, UPCA, UPCE:
	default:
		return errors.New("barcode: unsupported format")
	}
	if result.Text == "" || len(result.Text) > maxTextBytes || !utf8.ValidString(result.Text) {
		return errors.New("barcode: invalid decoded text")
	}
	return nil
}

// ErrNotFound means no supported code was found in the image.
var ErrNotFound = errors.New("barcode: no supported code found")

// Decode searches a bounded image for one supported code. It does not execute
// or open anything encoded in the image.
func Decode(img image.Image) (Result, error) {
	if img == nil || !validDimensions(img.Bounds().Dx(), img.Bounds().Dy()) {
		return Result{}, errors.New("barcode: invalid image dimensions")
	}
	bitmap, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return Result{}, err
	}
	hints := map[gozxing.DecodeHintType]interface{}{
		gozxing.DecodeHintType_TRY_HARDER: true,
		gozxing.DecodeHintType_POSSIBLE_FORMATS: []gozxing.BarcodeFormat{
			gozxing.BarcodeFormat_EAN_13, gozxing.BarcodeFormat_EAN_8, gozxing.BarcodeFormat_UPC_A, gozxing.BarcodeFormat_UPC_E,
		},
	}
	for _, reader := range []gozxing.Reader{qrcode.NewQRCodeReader(), oned.NewMultiFormatUPCEANReader(hints), oned.NewCode128Reader()} {
		decoded, err := reader.Decode(bitmap, hints)
		if err != nil {
			continue
		}
		result := Result{Format: Format(decoded.GetBarcodeFormat().String()), Text: decoded.GetText()}
		return result, result.Validate()
	}
	return Result{}, ErrNotFound
}

// DecodeLuminance takes one row-major luminance byte per pixel.
func DecodeLuminance(pixels []byte, width, height int) (Result, error) {
	if !validDimensions(width, height) || len(pixels) != width*height {
		return Result{}, errors.New("barcode: invalid luminance dimensions")
	}
	return Decode(&image.Gray{Pix: pixels, Stride: width, Rect: image.Rect(0, 0, width, height)})
}

func validDimensions(width, height int) bool {
	return width > 0 && height > 0 && width <= MaxDimension && height <= MaxDimension
}
