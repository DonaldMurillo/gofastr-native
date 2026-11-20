package featuresnative

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/extension/barcode/barcodebind"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func TestEveryDemoActionUsesValidSharedServiceContracts(t *testing.T) {
	app, err := newApp()
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/features", "/features/inbox", "/features/secure", "/features/widgets"} {
		screen, err := app.Render(context.Background(), route, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, action := range screen.Actions {
			response, err := app.Dispatch(context.Background(), route, action.ID, nil)
			if err != nil {
				t.Fatalf("%s/%s: %v", route, action.ID, err)
			}
			if response.Service != nil {
				if err := response.Service.Validate(); err != nil {
					t.Fatalf("%s/%s: %v", route, action.ID, err)
				}
			}
		}
	}
}

func TestBarcodeNoMatchAnnouncesWithoutTreatingInvalidInputAsNoMatch(t *testing.T) {
	blank := make([]byte, 640*480)
	for i := range blank {
		blank[i] = 255
	}
	payload, err := barcodebind.Decode(blank, 640, 480)
	if err != nil {
		t.Fatal(err)
	}
	file := &files.Document{Name: "Barcode.json", MediaType: "application/json", Data: []byte(payload)}
	result, err := barcodeRoute().Files["scan"](context.Background(), nativeui.Request{}, nativeui.FileResult{Status: nativeui.EffectCompleted, File: file})
	if err != nil || result.Announcement != "No barcode found." {
		t.Fatalf("blank image: announcement %q, error %v", result.Announcement, err)
	}
	if _, err := barcodebind.Decode(blank[:len(blank)-1], 640, 480); err == nil {
		t.Fatal("malformed image became a no-code result")
	}
}
