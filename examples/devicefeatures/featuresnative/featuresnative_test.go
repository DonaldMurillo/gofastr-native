package featuresnative

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-native/device/calendar"
	"github.com/DonaldMurillo/gofastr-native/device/deviceinfo"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func TestEveryDemoActionUsesValidSharedServiceContracts(t *testing.T) {
	app, err := newApp()
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/features", "/features/network", "/features/inbox", "/features/secure", "/features/widgets", "/features/location", "/features/sign-in", "/features/calendar", "/features/refresh", "/features/device"} {
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

func TestDeviceSnapshotText(t *testing.T) {
	for _, test := range []struct {
		result deviceinfo.Result
		want   string
	}{
		{deviceinfo.Result{Info: &deviceinfo.Info{Manufacturer: "Google", Model: "Pixel", Platform: "android", OSVersion: "14", APILevel: 34, Locale: "en-US", AppVersion: "1.0"}}, "Google Pixel; android 14 (API 34); locale en-US; app 1.0"},
		{deviceinfo.Result{Info: &deviceinfo.Info{}}, "app unknown"},
		{deviceinfo.Result{Battery: &deviceinfo.Battery{LevelKnown: true, Level: 42, State: deviceinfo.Charging, PowerSource: deviceinfo.AC}}, "Battery: 42% charging (ac), power save off."},
		{deviceinfo.Result{Battery: &deviceinfo.Battery{State: deviceinfo.UnknownState, PowerSource: deviceinfo.UnknownSource, PowerSave: true}}, "Battery: unknown unknown (unknown), power save on."},
		{deviceinfo.Result{}, "Device snapshot unavailable."},
	} {
		if got := snapshotText(test.result); !strings.Contains(got, test.want) {
			t.Fatalf("%q does not contain %q", got, test.want)
		}
	}
}

func TestCalendarMessages(t *testing.T) {
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		result calendar.Result
		want   string
	}{
		{calendar.Result{Authorization: "full"}, "Calendar authorization: full"},
		{calendar.Result{Identifier: "event"}, "Sample event added."},
		{calendar.Result{}, "No calendar events"},
		{calendar.Result{Events: []calendar.Event{{Title: "GoFastr sample event", Start: now}}}, "GoFastr sample event — 2026-01-02T12:00:00Z"},
	} {
		if got := calendarMessage(test.result); !strings.Contains(got, test.want) {
			t.Fatalf("got %q, want %q", got, test.want)
		}
	}
}

func TestBackgroundRefreshRecordsTheLastRun(t *testing.T) {
	if err := Start(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	if lastRefresh() != "never" {
		t.Fatal("refresh reported before it ran")
	}
	if err := BackgroundRefresh(1_000); err != nil {
		t.Fatal(err)
	}
	if lastRefresh() == "never" {
		t.Fatal("refresh did not record its run")
	}
	CancelBackgroundRefresh()
}

// Android serves the root screen's Scan code action through the compiled
// barcode adapter; iOS serves the same scanner/v1 request natively.
func TestScannerAdapterIsRegisteredForAndroid(t *testing.T) {
	app, err := newApp()
	if err != nil {
		t.Fatal(err)
	}
	for _, definition := range app.Extensions() {
		for _, capability := range definition.Manifest.Capabilities {
			if capability == string(nativeui.CapabilityScanner) && definition.Manifest.Android != nil {
				return
			}
		}
	}
	t.Fatal("no Android scanner/v1 adapter is registered")
}
