package nativeui

import (
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/calendar"
)

func TestCalendarService(t *testing.T) {
	request := ServiceRequest{ID: "calendar", Calendar: &calendar.Request{Operation: calendar.Status}}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	if request.capability() != CapabilityCalendar || !knownCapability(CapabilityCalendar) {
		t.Fatal("missing capability")
	}
	good := ServiceResult{Status: EffectCompleted, Calendar: &calendar.Result{Authorization: "full"}}
	if err := request.accept(good); err != nil {
		t.Fatal(err)
	}
	for _, result := range []ServiceResult{{Status: EffectCompleted}, {Status: EffectDenied, Calendar: good.Calendar}, {Status: EffectCompleted, Calendar: &calendar.Result{}}, {Status: EffectCompleted, Calendar: good.Calendar, Identifier: "unexpected"}} {
		if request.accept(result) == nil {
			t.Fatalf("accepted %#v", result)
		}
	}
	request.Calendar.Operation = calendar.Read
	if request.Validate() == nil {
		t.Fatal("accepted missing range")
	}
	request.Calendar.Operation = calendar.Status
	clone := cloneService(&request)
	clone.Calendar.Operation = calendar.Add
	if request.Calendar.Operation != calendar.Status {
		t.Fatal("aliased request")
	}
	request.Calendar = nil
	if request.accept(good) == nil {
		t.Fatal("accepted unrelated content")
	}
}
