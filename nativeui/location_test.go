package nativeui

import (
	"encoding/json"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/location"
)

func TestLocationService(t *testing.T) {
	request := ServiceRequest{ID: "location", Location: &location.Request{Operation: location.Status}}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	if request.capability() != CapabilityLocation || !knownCapability(CapabilityLocation) {
		t.Fatal("location capability missing")
	}
	result := ServiceResult{Status: EffectCompleted, Location: &location.Result{Permission: "undetermined"}}
	if err := request.accept(result); err != nil {
		t.Fatal(err)
	}
	for _, status := range []EffectStatus{EffectFailed, EffectDenied, EffectCancelled, EffectUnsupported} {
		result.Status = status
		if request.accept(result) == nil {
			t.Fatalf("%s accepted device content", status)
		}
		if err := request.accept(ServiceResult{Status: status}); err != nil {
			t.Fatal(err)
		}
	}
	if request.accept(ServiceResult{Status: EffectCompleted}) == nil {
		t.Fatal("missing location accepted")
	}
	if (ServiceRequest{}).accept(ServiceResult{Status: EffectCompleted, Location: result.Location}) == nil {
		t.Fatal("unrelated location accepted")
	}
	request.Location.Accuracy = location.Precise
	if request.Validate() == nil {
		t.Fatal("invalid status accepted")
	}
	request.Location.Accuracy = ""
	request.Location.Operation = location.Current
	request.Location.Accuracy = location.Precise
	if request.accept(ServiceResult{Status: EffectCompleted, Location: &location.Result{Permission: "precise", ServicesEnabled: true}}) == nil {
		t.Fatal("current result without a fix accepted")
	}
	request.Location = &location.Request{Operation: location.Status}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"location":{"operation":"status"},"id":"location"}` && string(encoded) != `{"id":"location","location":{"operation":"status"}}` {
		t.Fatal(string(encoded))
	}
}
