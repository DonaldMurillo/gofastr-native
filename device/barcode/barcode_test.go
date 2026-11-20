package barcode

import (
	"errors"
	"image"
	"image/color"
	"image/draw"
	"testing"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/oned"
	"github.com/makiuchi-d/gozxing/qrcode"
)

func TestDecodeLuminance(t *testing.T) {
	for _, tc := range []struct {
		format gozxing.BarcodeFormat
		encode gozxing.Writer
		text   string
	}{
		{gozxing.BarcodeFormat_QR_CODE, qrcode.NewQRCodeWriter(), "https://example.org/scan"},
		{gozxing.BarcodeFormat_CODE_128, oned.NewCode128Writer(), "BOX-8135"},
		{gozxing.BarcodeFormat_EAN_13, oned.NewEAN13Writer(), "5901234123457"},
		{gozxing.BarcodeFormat_EAN_8, oned.NewEAN8Writer(), "96385074"},
		{gozxing.BarcodeFormat_UPC_A, oned.NewUPCAWriter(), "036000291452"},
		{gozxing.BarcodeFormat_UPC_E, oned.NewUPCEWriter(), "04252614"},
	} {
		t.Run(tc.format.String(), func(t *testing.T) {
			code, err := tc.encode.Encode(tc.text, tc.format, 400, 128, nil)
			if err != nil {
				t.Fatal(err)
			}
			img := image.NewGray(image.Rect(0, 0, code.GetWidth(), code.GetHeight()))
			draw.Draw(img, img.Bounds(), image.White, image.Point{}, draw.Src)
			for y := 0; y < img.Bounds().Dy(); y++ {
				for x := 0; x < img.Bounds().Dx(); x++ {
					if code.Get(x, y) {
						img.SetGray(x, y, color.Gray{})
					}
				}
			}
			got, err := DecodeLuminance(img.Pix, img.Rect.Dx(), img.Rect.Dy())
			if want := (Result{Format: Format(tc.format.String()), Text: tc.text}); err != nil || got != want {
				t.Fatalf("got %+v, %v; want %+v", got, err, want)
			}
		})
	}
}

func TestInvalidImageAndMissingCode(t *testing.T) {
	blank := image.NewGray(image.Rect(0, 0, 64, 64))
	if _, err := Decode(blank); !errors.Is(err, ErrNotFound) {
		t.Fatalf("blank image: %v", err)
	}
	if _, err := DecodeLuminance([]byte{1}, 2, 2); err == nil {
		t.Fatal("accepted truncated pixels")
	}
	if _, err := DecodeLuminance(nil, MaxDimension+1, 1); err == nil {
		t.Fatal("accepted oversized image")
	}
}
