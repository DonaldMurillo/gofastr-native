package nativeui

import (
	"encoding/json"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/location"
)

func TestLocationServiceWire(t *testing.T) {
	request := ServiceRequest{ID: "locate", Location: &location.Request{Operation: location.Current}}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	if request.capability() != CapabilityLocation || !knownCapability(CapabilityLocation) {
		t.Fatal("missing location capability")
	}
	cloned := cloneService(&request)
	cloned.Location.Operation = location.Status
	if request.Location.Operation != location.Current {
		t.Fatal("aliased location request")
	}
	result := ServiceResult{Status: EffectCompleted, Location: &location.Result{Permission: location.WhenInUse, ServicesEnabled: true, Fixes: []location.Fix{{Latitude: 37.3349, Longitude: -122.009, HorizontalAccuracyMeters: 1}}}}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ServiceResult
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := request.accept(decoded); err != nil {
		t.Fatal(err)
	}
	for _, status := range []EffectStatus{EffectDenied, EffectUnsupported, EffectFailed, EffectCancelled} {
		if err := request.accept(ServiceResult{Status: status, Permission: "denied"}); err != nil {
			t.Fatal(err)
		}
		decoded.Status = status
		if request.accept(decoded) == nil {
			t.Fatal("failure carried fixes")
		}
	}
	if request.accept(ServiceResult{Status: EffectCompleted}) == nil {
		t.Fatal("missing content accepted")
	}
	if (ServiceRequest{ID: "other"}).accept(result) == nil {
		t.Fatal("unrelated location accepted")
	}
}
