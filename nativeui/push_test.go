package nativeui

import (
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/push"
	"github.com/DonaldMurillo/gofastr-native/device/widgets"
)

func TestPushServiceContract(t *testing.T) {
	request := ServiceRequest{ID: "register", Push: &push.Request{Operation: push.Register}}
	state := &push.State{Permission: "authorized", Registered: true, Token: "abcd"}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	if request.capability() != CapabilityPush {
		t.Fatal("wrong capability")
	}
	if err := request.accept(ServiceResult{Status: EffectCompleted, Push: state}); err != nil {
		t.Fatal(err)
	}
	for _, result := range []ServiceResult{
		{Status: EffectCompleted},
		{Status: EffectFailed, Push: state},
		{Status: EffectCompleted, Push: state, Widget: &widgets.State{}},
		{Status: EffectCompleted, Push: &push.State{}},
	} {
		if request.accept(result) == nil {
			t.Fatalf("accepted invalid result: %+v", result)
		}
	}
	other := ServiceRequest{ID: "widget", Widget: &widgets.Request{Operation: widgets.Status}}
	if other.accept(ServiceResult{Status: EffectCompleted, Push: state}) == nil {
		t.Fatal("unrelated push accepted")
	}
	request.Widget = other.Widget
	if request.Validate() == nil {
		t.Fatal("multiple operations accepted")
	}
}
