package nativeui

import (
	"context"
	"github.com/DonaldMurillo/gofastr-native/device/contacts"
	"testing"
)

func TestContactsRuntimeOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		multiple bool
		status   EffectStatus
		count    int
		valid    bool
	}{
		{"completed", false, EffectCompleted, 1, true}, {"cancelled", false, EffectCancelled, 0, true},
		{"multiple", true, EffectCompleted, 2, true}, {"single", false, EffectCompleted, 2, false},
		{"missing", false, EffectCompleted, 0, false}, {"cancelled-payload", false, EffectCancelled, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			definition := &ServiceRequest{ID: "device", Contacts: &contacts.Request{Multiple: tc.multiple}}
			calls := 0
			runtime, command := serviceFixture(t, definition, func(context.Context, Request, ServiceResult) (Result, error) { calls++; return Result{}, nil }, nil)
			shown := admitService(t, runtime, command)
			result := &ServiceResult{Status: tc.status}
			if tc.count > 0 {
				result.Contacts = &contacts.Selection{}
				for i := 0; i < tc.count; i++ {
					result.Contacts.Contacts = append(result.Contacts.Contacts, contacts.Contact{ID: "opaque", GivenName: "Ada"})
				}
			}
			command.Kind = "service-result"
			command.RequestID = "observed"
			command.ServiceID = shown.ServiceID
			command.ServiceResult = result
			reply := serviceCommand(t, context.Background(), runtime, command)
			if (reply.Error == nil) != tc.valid {
				t.Fatalf("unexpected result: %+v", reply.Error)
			}
			if tc.valid && calls != 1 {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
	other := ServiceRequest{ID: "other", Clipboard: nil}
	if other.accept(ServiceResult{Status: EffectCompleted, Contacts: &contacts.Selection{}}) == nil {
		t.Fatal("unrequested payload accepted")
	}
}
