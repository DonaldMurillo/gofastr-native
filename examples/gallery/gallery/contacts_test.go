package gallery

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func TestContactsDemo(t *testing.T) {
	demo := &state{saved: map[string]string{}}
	route := contactsRoute(demo)
	ctx := context.Background()
	request := nativeui.Request{}
	screen, err := route.Screen(ctx, request)
	if err != nil || screen.Actions[0].Label != "Choose contact" || screen.Requires[0].Capability != "contacts/v1" {
		t.Fatalf("screen: %+v %v", screen, err)
	}
	result, err := route.Actions["choose"](ctx, request)
	if err != nil || result.Files.Extension.Name != "gofastr.contacts/v1" {
		t.Fatalf("action: %+v %v", result, err)
	}
	respond := route.Files["contact"]
	if _, err := respond(ctx, request, nativeui.FileResult{}); err != nil || demo.saved["contact"] != "" {
		t.Fatal("empty outcome changed selection")
	}
	document := &files.Document{Name: "Contact.json", MediaType: "application/json", Data: []byte(`{}`)}
	if _, err := respond(ctx, request, nativeui.FileResult{Status: nativeui.EffectCompleted, File: document}); err == nil {
		t.Fatal("invalid contact accepted")
	}
	document.Data = []byte(`{"name":"Ada","phone":"123","email":""}`)
	if _, err := respond(ctx, request, nativeui.FileResult{Status: nativeui.EffectCompleted, File: document}); err != nil {
		t.Fatal(err)
	}
	screen, err = route.Screen(ctx, request)
	if err != nil || !strings.Contains(screen.Text, "Ada\n123") {
		t.Fatalf("selection not displayed: %+v %v", screen, err)
	}
}
