package barcode

import (
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/barcode"
	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/extension"
)

func TestScanContracts(t *testing.T) {
	definition := Definition()
	registry, err := extension.New(definition)
	if err != nil {
		t.Fatal(err)
	}
	if definition.Manifest.HasIOS() || definition.Manifest.HasWebView() {
		t.Fatal("scanner should not advertise non-Android adapters")
	}
	for source, want := range map[barcode.Source]string{
		barcode.Camera: "gofastr.barcode.camera/v1",
		barcode.Image:  "gofastr.barcode.image/v1",
	} {
		op, err := Scan(barcode.Request{Source: source})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := registry.Validate(*op); got != want || err != nil {
			t.Fatalf("capability = %q, %v; want %q", got, err, want)
		}
		document := files.Document{Name: filename, MediaType: "application/json", Data: []byte(`{"format":"QR_CODE","text":"hello"}`)}
		if err := registry.Accept(*op, document); err != nil {
			t.Fatal(err)
		}
		if result, err := ReadResult(document); result.Format != barcode.QRCode || result.Text != "hello" || err != nil {
			t.Fatalf("unexpected result: %+v, %v", result, err)
		}
		for _, invalid := range []string{`{"format":"QR_CODE","text":""}`, `{"format":"UNKNOWN","text":"hello"}`, `{"format":"QR_CODE","text":"hello","path":"/image.jpg"}`, `{"notFound":true,"format":"QR_CODE","text":"hello"}`, `{}`} {
			document.Data = []byte(invalid)
			if err := registry.Accept(*op, document); err == nil {
				t.Fatal("accepted invalid result")
			}
		}
	}
	if _, err := Scan(barcode.Request{Source: "video"}); err == nil {
		t.Fatal("accepted unknown source")
	}
}
