package barcode

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/scanner"
	"github.com/DonaldMurillo/gofastr-native/extension"
	"github.com/DonaldMurillo/gofastr-native/extension/barcode/barcodebind"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

func TestDefinitionProvidesScannerServiceOnly(t *testing.T) {
	definition := Definition()
	registry, err := extension.New(definition)
	if err != nil {
		t.Fatal(err)
	}
	if definition.Manifest.HasIOS() || definition.Manifest.HasWebView() {
		t.Fatal("the adapter should be Android-only; iOS implements scanner/v1 in the renderer")
	}
	if !registry.Allows(string(nativeui.CapabilityScanner)) {
		t.Fatal("adapter does not declare scanner/v1")
	}
	if _, err := registry.Validate(extension.FileOperation{Name: definition.Manifest.Name, Payload: `{"source":"camera"}`}); err == nil {
		t.Fatal("accepted a file operation; scans go through ServiceRequest.Scan")
	}
	app, err := nativeui.NewWithExtensions("/", []extension.Definition{definition}, nativeui.Route{Path: "/", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		return nativeui.Screen{Title: "Scan", Text: "Scan"}, nil
	}})
	if err != nil || app == nil {
		t.Fatalf("app with the scanner adapter: %v", err)
	}
}

func TestBindingReturnsAcceptedScannerResult(t *testing.T) {
	code, err := qrcode.NewQRCodeWriter().Encode("hello", gozxing.BarcodeFormat_QR_CODE, 200, 200, nil)
	if err != nil {
		t.Fatal(err)
	}
	width, height := code.GetWidth(), code.GetHeight()
	img := image.NewGray(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if !code.Get(x, y) {
				img.SetGray(x, y, color.Gray{Y: 255})
			}
		}
	}
	request := scanner.Request{Title: "Scan code", Symbologies: []scanner.Symbology{scanner.QR}}
	encoded, _ := json.Marshal(request)
	if !barcodebind.Supports(string(encoded)) {
		t.Fatal("QR request reported unsupported")
	}
	payload, err := barcodebind.Decode(string(encoded), img.Pix, width, height)
	if err != nil {
		t.Fatal(err)
	}
	var result scanner.Result
	if err := json.Unmarshal([]byte(payload), &result); err != nil || request.Accept(result) != nil || result.Value != "hello" {
		t.Fatalf("payload %q: %+v, %v", payload, result, err)
	}

	blank := make([]byte, 640*480)
	for i := range blank {
		blank[i] = 255
	}
	if payload, err := barcodebind.Decode(string(encoded), blank, 640, 480); payload != "" || err != nil {
		t.Fatalf("blank image: %q, %v", payload, err)
	}
	if _, err := barcodebind.Decode(string(encoded), blank[:len(blank)-1], 640, 480); err == nil {
		t.Fatal("malformed image became a no-code result")
	}
	for _, invalid := range []string{`{"title":""}`, `{"title":"Scan","symbologies":["pdf417"]}`, `not json`} {
		if barcodebind.Supports(invalid) {
			t.Fatalf("supported %s", invalid)
		}
	}
	if _, err := barcodebind.Decode(`{"title":""}`, blank, 640, 480); err == nil {
		t.Fatal("decoded with an invalid request")
	}
}
