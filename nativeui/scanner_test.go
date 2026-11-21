package nativeui

import (
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/scanner"
	"github.com/DonaldMurillo/gofastr-native/device/sharing"
)

func TestScannerService(t *testing.T) {
	r := ServiceRequest{ID: "scan", Scan: &scanner.Request{Title: "Scan", Symbologies: []scanner.Symbology{scanner.QR}}}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	if r.capability() != CapabilityScanner {
		t.Fatal("wrong capability")
	}
	good := &scanner.Result{Value: "fixture", Symbology: scanner.QR}
	if err := r.accept(ServiceResult{Status: EffectCompleted, Scan: good}); err != nil {
		t.Fatal(err)
	}
	for _, status := range []EffectStatus{EffectCancelled, EffectDenied, EffectUnsupported, EffectFailed} {
		if err := r.accept(ServiceResult{Status: status}); err != nil {
			t.Fatal(err)
		}
		if r.accept(ServiceResult{Status: status, Scan: good}) == nil {
			t.Fatal("accepted unsuccessful content")
		}
	}
	for _, outcome := range []ServiceResult{{Status: EffectCompleted}, {Status: EffectCompleted, Scan: &scanner.Result{}}, {Status: EffectCompleted, Scan: &scanner.Result{Value: "x", Symbology: scanner.EAN8}}} {
		if r.accept(outcome) == nil {
			t.Fatal("accepted invalid result")
		}
	}
	other := ServiceRequest{ID: "copy", Clipboard: &sharing.Clipboard{Text: "x"}}
	if other.accept(ServiceResult{Status: EffectCompleted, Scan: good}) == nil {
		t.Fatal("accepted unrelated content")
	}
	r.Clipboard = other.Clipboard
	if r.Validate() == nil {
		t.Fatal("accepted multiple operations")
	}
}
