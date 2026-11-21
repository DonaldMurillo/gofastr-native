package nativeui

import (
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/network"
)

func TestNetworkServiceValidation(t *testing.T) {
	r := ServiceRequest{ID: "device", Network: &network.Request{Operation: network.Status}}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	if r.capability() != CapabilityNetwork {
		t.Fatal("wrong capability")
	}
	for _, result := range []ServiceResult{{Status: EffectCompleted}, {Status: EffectCompleted, Network: &network.Result{}}, {Status: EffectFailed, Network: &network.Result{Transport: "none"}}} {
		if r.accept(result) == nil {
			t.Fatalf("accepted %#v", result)
		}
	}
	if err := r.accept(ServiceResult{Status: EffectCompleted, Network: &network.Result{Connected: true, Transport: "wifi", Validated: true}}); err != nil {
		t.Fatal(err)
	}
	if (ServiceRequest{}).accept(ServiceResult{Status: EffectCompleted, Network: &network.Result{Transport: "none"}}) == nil {
		t.Fatal("accepted unrelated content")
	}
	r.Network.Operation = "subscribe"
	if r.Validate() == nil {
		t.Fatal("accepted invalid operation")
	}
}
