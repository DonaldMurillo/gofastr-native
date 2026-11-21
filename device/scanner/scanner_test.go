package scanner

import (
	"strings"
	"testing"
)

func TestRequest(t *testing.T) {
	for _, request := range []Request{{Title: "Scan"}, {Title: strings.Repeat("é", 100), Symbologies: []Symbology{QR, EAN13, EAN8, UPCE, Code39, Code93, Code128, ITF14, PDF417, Aztec, DataMatrix}}} {
		if err := request.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, request := range []Request{{}, {Title: " "}, {Title: "bad\n"}, {Title: "\xff"}, {Title: strings.Repeat("é", 101)}, {Title: "Scan", Symbologies: []Symbology{"unknown"}}, {Title: "Scan", Symbologies: []Symbology{QR, QR}}} {
		if request.Validate() == nil {
			t.Errorf("accepted %#v", request)
		}
	}
}

func TestAccept(t *testing.T) {
	request := Request{Title: "Scan", Symbologies: []Symbology{QR}}
	for _, value := range []string{"hello", strings.Repeat("x", 4096), "line\ntext"} {
		if err := request.Accept(Result{Value: value, Symbology: QR}); err != nil {
			t.Fatal(err)
		}
	}
	for _, result := range []Result{{Symbology: QR}, {Value: "\xff", Symbology: QR}, {Value: strings.Repeat("x", 4097), Symbology: QR}, {Value: "x", Symbology: EAN13}, {Value: "x", Symbology: "unknown"}} {
		if request.Accept(result) == nil {
			t.Errorf("accepted %#v", result)
		}
	}
	if err := (Request{Title: "All"}).Accept(Result{Value: "x", Symbology: EAN13}); err != nil {
		t.Fatal(err)
	}
	if (Request{}).Accept(Result{Value: "x", Symbology: QR}) == nil {
		t.Fatal("accepted invalid request")
	}
}
