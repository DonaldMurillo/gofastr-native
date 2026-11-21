// Package decode reads one scanner/v1 symbology from a bounded still image.
package decode

import (
	"errors"
	"image"
	"strings"

	"github.com/DonaldMurillo/gofastr-native/device/scanner"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/datamatrix"
	"github.com/makiuchi-d/gozxing/oned"
	"github.com/makiuchi-d/gozxing/qrcode"
)

// MaxDimension bounds each side of the decoded image in pixels.
const MaxDimension = 2048

// ErrNotFound means the image held no code in a requested, supported symbology.
var ErrNotFound = errors.New("barcode: no supported code found")

// readers lists the symbologies gozxing can decode, two-dimensional codes
// first. PDF417 and Aztec are absent: Android reports them unsupported.
var readers = []struct {
	symbologies []scanner.Symbology
	reader      func(hints map[gozxing.DecodeHintType]interface{}) gozxing.Reader
}{
	{[]scanner.Symbology{scanner.QR}, func(map[gozxing.DecodeHintType]interface{}) gozxing.Reader { return qrcode.NewQRCodeReader() }},
	{[]scanner.Symbology{scanner.DataMatrix}, func(map[gozxing.DecodeHintType]interface{}) gozxing.Reader { return datamatrix.NewDataMatrixReader() }},
	// UPC-A is left out of the possible formats, so it reads as EAN-13 with a
	// leading zero, the value iOS reports for the same code.
	{[]scanner.Symbology{scanner.EAN13, scanner.EAN8, scanner.UPCE}, oned.NewMultiFormatUPCEANReader},
	{[]scanner.Symbology{scanner.Code128}, func(map[gozxing.DecodeHintType]interface{}) gozxing.Reader { return oned.NewCode128Reader() }},
	{[]scanner.Symbology{scanner.Code93}, func(map[gozxing.DecodeHintType]interface{}) gozxing.Reader { return oned.NewCode93Reader() }},
	{[]scanner.Symbology{scanner.Code39}, func(map[gozxing.DecodeHintType]interface{}) gozxing.Reader { return oned.NewCode39Reader() }},
	{[]scanner.Symbology{scanner.ITF14}, func(map[gozxing.DecodeHintType]interface{}) gozxing.Reader { return oned.NewITFReader() }},
}

var formats = map[gozxing.BarcodeFormat]scanner.Symbology{
	gozxing.BarcodeFormat_QR_CODE: scanner.QR, gozxing.BarcodeFormat_DATA_MATRIX: scanner.DataMatrix,
	gozxing.BarcodeFormat_EAN_13: scanner.EAN13, gozxing.BarcodeFormat_EAN_8: scanner.EAN8, gozxing.BarcodeFormat_UPC_E: scanner.UPCE,
	gozxing.BarcodeFormat_CODE_128: scanner.Code128, gozxing.BarcodeFormat_CODE_93: scanner.Code93,
	gozxing.BarcodeFormat_CODE_39: scanner.Code39, gozxing.BarcodeFormat_ITF: scanner.ITF14,
}

var upcean = map[scanner.Symbology]gozxing.BarcodeFormat{
	scanner.EAN13: gozxing.BarcodeFormat_EAN_13, scanner.EAN8: gozxing.BarcodeFormat_EAN_8, scanner.UPCE: gozxing.BarcodeFormat_UPC_E,
}

// Supported is the subset of requested symbologies this decoder reads. An
// empty request means every supported symbology.
func Supported(request scanner.Request) []scanner.Symbology {
	var result []scanner.Symbology
	for _, entry := range readers {
		for _, symbology := range entry.symbologies {
			if len(request.Symbologies) == 0 || contains(request.Symbologies, symbology) {
				result = append(result, symbology)
			}
		}
	}
	return result
}

// Luminance decodes one row-major luminance byte per pixel.
func Luminance(request scanner.Request, pixels []byte, width, height int) (scanner.Result, error) {
	if !validDimensions(width, height) || len(pixels) != width*height {
		return scanner.Result{}, errors.New("barcode: invalid luminance dimensions")
	}
	return Image(request, &image.Gray{Pix: pixels, Stride: width, Rect: image.Rect(0, 0, width, height)})
}

// Image searches a bounded image for one code the request accepts. It does
// not execute or open anything encoded in the image.
func Image(request scanner.Request, img image.Image) (scanner.Result, error) {
	if err := request.Validate(); err != nil {
		return scanner.Result{}, err
	}
	if img == nil || !validDimensions(img.Bounds().Dx(), img.Bounds().Dy()) {
		return scanner.Result{}, errors.New("barcode: invalid image dimensions")
	}
	wanted := Supported(request)
	if len(wanted) == 0 {
		return scanner.Result{}, ErrNotFound
	}
	bitmap, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return scanner.Result{}, err
	}
	var possible []gozxing.BarcodeFormat
	for _, symbology := range wanted {
		if format, ok := upcean[symbology]; ok {
			possible = append(possible, format)
		}
	}
	hints := map[gozxing.DecodeHintType]interface{}{
		gozxing.DecodeHintType_TRY_HARDER:       true,
		gozxing.DecodeHintType_POSSIBLE_FORMATS: possible,
		gozxing.DecodeHintType_ALLOWED_LENGTHS:  []int{14},
	}
	for _, entry := range readers {
		if !overlaps(entry.symbologies, wanted) {
			continue
		}
		reader := entry.reader(hints)
		decoded, err := reader.Decode(bitmap, hints)
		if err != nil {
			continue
		}
		symbology, ok := formats[decoded.GetBarcodeFormat()]
		if !ok || !contains(wanted, symbology) {
			continue
		}
		text := decoded.GetText()
		if symbology == scanner.Code128 {
			if text, err = gs1(reader, bitmap, decoded); err != nil {
				return scanner.Result{}, err
			}
		}
		result := scanner.Result{Value: text, Symbology: symbology}
		return result, request.Accept(result)
	}
	return scanner.Result{}, ErrNotFound
}

// gs1 keeps GS1-128 structure: a leading FNC1 becomes the "]C1" symbology
// identifier and later FNC1 codewords become GS (0x1d) field separators.
func gs1(reader gozxing.Reader, bitmap *gozxing.BinaryBitmap, decoded *gozxing.Result) (string, error) {
	const fnc1 = 102
	// Codewords include start, data, checksum, and stop.
	raw := decoded.GetRawBytes()
	if len(raw) < 5 || raw[1] != fnc1 {
		return decoded.GetText(), nil
	}
	again, err := reader.Decode(bitmap, map[gozxing.DecodeHintType]interface{}{
		gozxing.DecodeHintType_TRY_HARDER: true,
		gozxing.DecodeHintType_ASSUME_GS1: true,
	})
	if err != nil {
		return "", err
	}
	text := again.GetText()
	if raw[len(raw)-2] == fnc1 {
		// gozxing decodes a checksum codeword equal to FNC1 as a separator.
		text = strings.TrimSuffix(text, "\x1d")
	}
	return text, nil
}

func validDimensions(width, height int) bool {
	return width > 0 && height > 0 && width <= MaxDimension && height <= MaxDimension
}

func contains(values []scanner.Symbology, value scanner.Symbology) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func overlaps(first, second []scanner.Symbology) bool {
	for _, value := range first {
		if contains(second, value) {
			return true
		}
	}
	return false
}
