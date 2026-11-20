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

func TestDecodeGS1128(t *testing.T) {
	for _, tc := range []struct{ name, encoded, want string }{
		{"GS1", "\u00f110LOT1\u00f117251231", "]C110LOT1\x1d17251231"},
		{"non-GS1 FNC1", "LOT1\u00f117251231", "LOT117251231"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, err := oned.NewCode128Writer().Encode(tc.encoded, gozxing.BarcodeFormat_CODE_128, 400, 80, nil)
			if err != nil {
				t.Fatal(err)
			}
			img := image.NewGray(image.Rect(0, 0, code.GetWidth(), code.GetHeight()))
			for y := 0; y < img.Rect.Dy(); y++ {
				for x := 0; x < img.Rect.Dx(); x++ {
					if !code.Get(x, y) {
						img.SetGray(x, y, color.Gray{Y: 255})
					}
				}
			}
			got, err := Decode(img)
			if err != nil || got != (Result{Format: Code128, Text: tc.want}) {
				t.Fatalf("got %+v, error %v; want Code 128 text %q", got, err, tc.want)
			}
		})
	}
}

func TestGS1128ChecksumIsNotData(t *testing.T) {
	for _, tc := range []struct{ encoded, want string }{
		{"\u00f11027", "]C11027"},
		{"\u00f11097\u00f1", "]C11097\x1d"},
	} {
		t.Run(tc.encoded, func(t *testing.T) {
			hints := map[gozxing.EncodeHintType]interface{}{gozxing.EncodeHintType_FORCE_CODE_SET: "C"}
			code, err := oned.NewCode128Writer().Encode(tc.encoded, gozxing.BarcodeFormat_CODE_128, 400, 80, hints)
			if err != nil {
				t.Fatal(err)
			}
			img := image.NewGray(image.Rect(0, 0, code.GetWidth(), code.GetHeight()))
			for y := 0; y < img.Rect.Dy(); y++ {
				for x := 0; x < img.Rect.Dx(); x++ {
					if !code.Get(x, y) {
						img.SetGray(x, y, color.Gray{Y: 255})
					}
				}
			}
			bitmap, err := gozxing.NewBinaryBitmapFromImage(img)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := oned.NewCode128Reader().DecodeWithoutHints(bitmap)
			if err != nil {
				t.Fatal(err)
			}
			if raw := decoded.GetRawBytes(); len(raw) < 4 || raw[len(raw)-2] != 102 {
				t.Fatalf("expected FNC1-valued checksum before stop; codewords: %v", raw)
			}
			got, err := Decode(img)
			if err != nil || got != (Result{Format: Code128, Text: tc.want}) {
				t.Fatalf("got %q, error %v; want %q", got.Text, err, tc.want)
			}
		})
	}
}
