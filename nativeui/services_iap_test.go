package nativeui

import (
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/iap"
	"github.com/DonaldMurillo/gofastr-native/device/sharing"
)

func TestIAPServiceValidation(t *testing.T) {
	request := ServiceRequest{ID: "store", IAP: &iap.Request{Operation: iap.Purchase, ProductID: "demo.product"}}
	if err := request.Validate(); err != nil || request.capability() != CapabilityIAP || !knownCapability(CapabilityIAP) {
		t.Fatalf("IAP request or capability not recognized: %v", err)
	}
	if cloned := cloneService(&request); cloned.IAP == nil || cloned.IAP == request.IAP || cloned.IAP.ProductID != request.IAP.ProductID {
		t.Fatal("IAP operation was not independently cloned")
	}
	pending := ServiceResult{Status: EffectCompleted, IAP: &iap.Result{PurchaseState: iap.Pending}}
	if err := request.accept(pending); err != nil {
		t.Fatal(err)
	}
	if err := request.accept(ServiceResult{Status: EffectUnsupported, Message: "Requires native iOS."}); err != nil {
		t.Fatal(err)
	}
	if request.accept(ServiceResult{Status: EffectCompleted}) == nil || request.accept(ServiceResult{Status: EffectCancelled, IAP: pending.IAP}) == nil {
		t.Fatal("accepted missing or unsolicited IAP content")
	}
	other := ServiceRequest{ID: "copy", Clipboard: &sharing.Clipboard{Text: "plain"}}
	if other.accept(pending) == nil {
		t.Fatal("unrelated service accepted IAP content")
	}
	request.Clipboard = other.Clipboard
	if request.Validate() == nil {
		t.Fatal("accepted a mixed request")
	}
}
