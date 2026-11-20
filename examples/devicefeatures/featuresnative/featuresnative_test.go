package featuresnative

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func TestEveryDemoActionUsesValidSharedServiceContracts(t *testing.T) {
	app, err := newApp()
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/features", "/features/inbox", "/features/secure", "/features/widgets", "/features/background"} {
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

func TestBackgroundStartsWithoutRendererAndPersistsTimestamp(t *testing.T) {
	directory := t.TempDir()
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	if err := Start(directory); err != nil {
		t.Fatal("idempotent startup", err)
	}
	encoded, err := Command(`{"version":1,"kind":"background/run","backgroundId":"job","backgroundTask":"timestamp"}`)
	if err != nil {
		t.Fatal(err)
	}
	var reply nativeui.Reply
	if err := json.Unmarshal([]byte(encoded), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Background == nil || reply.Background.Status != "completed" {
		t.Fatal(encoded)
	}
	if data, err := os.ReadFile(filepath.Join(directory, "background-last-run")); err != nil || len(data) == 0 {
		t.Fatal(string(data), err)
	}
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	screen, err := screens.Render(context.Background(), "/features/background", nil)
	if err != nil || screen.Text == "Last recorded run: Not run yet." {
		t.Fatal(screen, err)
	}
}
