package nativeui

import (
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/browser"
	"github.com/DonaldMurillo/gofastr-native/device/sharing"
)

func TestBrowserService(t *testing.T) {
	request := ServiceRequest{ID: "docs", Browser: &browser.Request{URL: "https://example.com"}}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	if request.capability() != CapabilityBrowser {
		t.Fatal("wrong capability")
	}
	for _, status := range []EffectStatus{EffectCompleted, EffectFailed, EffectUnsupported} {
		if err := request.accept(ServiceResult{Status: status}); err != nil {
			t.Fatal(err)
		}
	}
	if err := request.accept(ServiceResult{Status: EffectCompleted, DurationMillis: 1}); err == nil {
		t.Fatal("accepted device content")
	}
	request.Share = &sharing.Request{Text: "hello"}
	if err := request.Validate(); err == nil {
		t.Fatal("accepted two operations")
	}
	request.Share = nil
	request.Browser.URL = ""
	if err := request.Validate(); err == nil {
		t.Fatal("accepted empty URL")
	}
}
