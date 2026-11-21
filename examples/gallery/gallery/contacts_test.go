package gallery

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/contacts"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func TestContactsDemo(t *testing.T) {
	demo := &state{saved: map[string]string{}}
	route := contactsRoute(demo)
	ctx := context.Background()
	request := nativeui.Request{}
	screen, err := route.Screen(ctx, request)
	if err != nil || screen.Actions[0].Label != "Choose contact" || screen.Requires[0].Capability != nativeui.CapabilityContacts {
		t.Fatalf("screen: %+v %v", screen, err)
	}
	result, err := route.Actions["choose"](ctx, request)
	if err != nil || result.Service == nil || result.Service.ID != "contact" || result.Service.Contacts == nil {
		t.Fatalf("action: %+v %v", result, err)
	}
	if err := result.Service.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := result.Service.Contacts; got.Multiple || len(got.Fields) != 1 || got.Fields[0] != contacts.Phone {
		t.Fatalf("request: %+v", got)
	}
	respond := route.Services["contact"]
	if _, err := respond(ctx, request, nativeui.ServiceResult{Status: nativeui.EffectCancelled}); err != nil || demo.saved["contact"] != "" {
		t.Fatal("cancelled outcome changed selection")
	}
	selection := &contacts.Selection{Contacts: []contacts.Contact{{ID: "lookup", GivenName: "Ada", Phones: []contacts.Labeled{{Label: "Mobile", Value: "123"}}}}}
	if _, err := respond(ctx, request, nativeui.ServiceResult{Status: nativeui.EffectCompleted, Contacts: selection}); err != nil {
		t.Fatal(err)
	}
	screen, err = route.Screen(ctx, request)
	if err != nil || !strings.Contains(screen.Text, "Ada\n123") {
		t.Fatalf("selection not displayed: %+v %v", screen, err)
	}
	if _, err := respond(ctx, request, nativeui.ServiceResult{Status: nativeui.EffectCancelled}); err != nil || !strings.Contains(demo.saved["contact"], "Ada") {
		t.Fatal("cancellation dropped the previous selection")
	}
}
