package decode

import (
	"errors"
	"image"
	"image/color"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/scanner"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/datamatrix"
	"github.com/makiuchi-d/gozxing/oned"
	"github.com/makiuchi-d/gozxing/qrcode"
)

var all = scanner.Request{Title: "Scan"}

// render draws an encoded symbol, dark on white, with a quiet zone.
func render(t *testing.T, writer gozxing.Writer, text string, format gozxing.BarcodeFormat, width, height int, hints map[gozxing.EncodeHintType]interface{}) *image.Gray {
	t.Helper()
	code, err := writer.Encode(text, format, width, height, hints)
	if err != nil {
		t.Fatal(err)
	}
	const margin = 20
	img := image.NewGray(image.Rect(0, 0, code.GetWidth()+2*margin, code.GetHeight()+2*margin))
	for y := 0; y < img.Rect.Dy(); y++ {
		for x := 0; x < img.Rect.Dx(); x++ {
			cx, cy := x-margin, y-margin
			if cx < 0 || cy < 0 || cx >= code.GetWidth() || cy >= code.GetHeight() || !code.Get(cx, cy) {
				img.SetGray(x, y, color.Gray{Y: 255})
			}
		}
	}
	return img
}

func TestLuminanceReadsEverySupportedSymbology(t *testing.T) {
	for _, tc := range []struct {
		format gozxing.BarcodeFormat
		writer gozxing.Writer
		text   string
		want   scanner.Result
	}{
		{gozxing.BarcodeFormat_QR_CODE, qrcode.NewQRCodeWriter(), "https://example.org/scan", scanner.Result{Value: "https://example.org/scan", Symbology: scanner.QR}},
		{gozxing.BarcodeFormat_DATA_MATRIX, datamatrix.NewDataMatrixWriter(), "LOT-42", scanner.Result{Value: "LOT-42", Symbology: scanner.DataMatrix}},
		{gozxing.BarcodeFormat_CODE_128, oned.NewCode128Writer(), "BOX-8135", scanner.Result{Value: "BOX-8135", Symbology: scanner.Code128}},
		{gozxing.BarcodeFormat_CODE_93, oned.NewCode93Writer(), "SHELF 12", scanner.Result{Value: "SHELF 12", Symbology: scanner.Code93}},
		{gozxing.BarcodeFormat_CODE_39, oned.NewCode39Writer(), "PART-77", scanner.Result{Value: "PART-77", Symbology: scanner.Code39}},
		{gozxing.BarcodeFormat_ITF, oned.NewITFWriter(), "10012345678902", scanner.Result{Value: "10012345678902", Symbology: scanner.ITF14}},
		{gozxing.BarcodeFormat_EAN_13, oned.NewEAN13Writer(), "5901234123457", scanner.Result{Value: "5901234123457", Symbology: scanner.EAN13}},
		{gozxing.BarcodeFormat_EAN_8, oned.NewEAN8Writer(), "96385074", scanner.Result{Value: "96385074", Symbology: scanner.EAN8}},
		// UPC-A reads as EAN-13 with a leading zero, as on iOS.
		{gozxing.BarcodeFormat_UPC_A, oned.NewUPCAWriter(), "036000291452", scanner.Result{Value: "0036000291452", Symbology: scanner.EAN13}},
		{gozxing.BarcodeFormat_UPC_E, oned.NewUPCEWriter(), "04252614", scanner.Result{Value: "04252614", Symbology: scanner.UPCE}},
	} {
		t.Run(tc.format.String(), func(t *testing.T) {
			img := render(t, tc.writer, tc.text, tc.format, 400, 128, nil)
			got, err := Luminance(all, img.Pix, img.Rect.Dx(), img.Rect.Dy())
			if err != nil || got != tc.want {
				t.Fatalf("got %+v, %v; want %+v", got, err, tc.want)
			}
			only := scanner.Request{Title: "Scan", Symbologies: []scanner.Symbology{tc.want.Symbology}}
			if got, err := Image(only, img); err != nil || got != tc.want {
				t.Fatalf("requested only %s: got %+v, %v", tc.want.Symbology, got, err)
			}
		})
	}
}

func TestUnrequestedSymbologyIsNotFound(t *testing.T) {
	img := render(t, qrcode.NewQRCodeWriter(), "hello", gozxing.BarcodeFormat_QR_CODE, 200, 200, nil)
	request := scanner.Request{Title: "Scan", Symbologies: []scanner.Symbology{scanner.EAN13, scanner.Code128}}
	if _, err := Image(request, img); !errors.Is(err, ErrNotFound) {
		t.Fatalf("QR code with a 1D-only request: %v", err)
	}
}

func TestLongITFIsNotITF14(t *testing.T) {
	img := render(t, oned.NewITFWriter(), "1001234567890212", gozxing.BarcodeFormat_ITF, 400, 128, nil)
	request := scanner.Request{Title: "Scan", Symbologies: []scanner.Symbology{scanner.ITF14}}
	if got, err := Image(request, img); !errors.Is(err, ErrNotFound) {
		t.Fatalf("16-digit ITF read as ITF-14: %+v, %v", got, err)
	}
}

func TestSupported(t *testing.T) {
	if got := Supported(all); len(got) != 9 {
		t.Fatalf("all supported = %v", got)
	}
	if got := Supported(scanner.Request{Title: "Scan", Symbologies: []scanner.Symbology{scanner.PDF417, scanner.Aztec}}); len(got) != 0 {
		t.Fatalf("PDF417 and Aztec should be unsupported, got %v", got)
	}
	if got := Supported(scanner.Request{Title: "Scan", Symbologies: []scanner.Symbology{scanner.PDF417, scanner.QR}}); len(got) != 1 || got[0] != scanner.QR {
		t.Fatalf("mixed request = %v", got)
	}
}

func TestInvalidImageRequestAndMissingCode(t *testing.T) {
	blank := image.NewGray(image.Rect(0, 0, 64, 64))
	if _, err := Image(all, blank); !errors.Is(err, ErrNotFound) {
		t.Fatalf("blank image: %v", err)
	}
	if _, err := Luminance(all, []byte{1}, 2, 2); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatal("accepted truncated pixels")
	}
	if _, err := Luminance(all, nil, MaxDimension+1, 1); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatal("accepted oversized image")
	}
	if _, err := Image(scanner.Request{}, blank); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatal("accepted a request without a title")
	}
}

func TestDecodeGS1128(t *testing.T) {
	for _, tc := range []struct{ name, encoded, want string }{
		{"GS1", "ñ10LOT1ñ17251231", "]C110LOT1\x1d17251231"},
		{"non-GS1 FNC1", "LOT1ñ17251231", "LOT117251231"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := render(t, oned.NewCode128Writer(), tc.encoded, gozxing.BarcodeFormat_CODE_128, 400, 80, nil)
			got, err := Image(all, img)
			if err != nil || got != (scanner.Result{Value: tc.want, Symbology: scanner.Code128}) {
				t.Fatalf("got %+v, error %v; want Code 128 text %q", got, err, tc.want)
			}
		})
	}
}

func TestGS1128ChecksumIsNotData(t *testing.T) {
	for _, tc := range []struct{ encoded, want string }{
		{"ñ1027", "]C11027"},
		{"ñ1097ñ", "]C11097\x1d"},
	} {
		t.Run(tc.encoded, func(t *testing.T) {
			hints := map[gozxing.EncodeHintType]interface{}{gozxing.EncodeHintType_FORCE_CODE_SET: "C"}
			img := render(t, oned.NewCode128Writer(), tc.encoded, gozxing.BarcodeFormat_CODE_128, 400, 80, hints)
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
			got, err := Image(all, img)
			if err != nil || got != (scanner.Result{Value: tc.want, Symbology: scanner.Code128}) {
				t.Fatalf("got %q, error %v; want %q", got.Value, err, tc.want)
			}
		})
	}
}
