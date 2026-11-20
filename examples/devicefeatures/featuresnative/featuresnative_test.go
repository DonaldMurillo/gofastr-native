package featuresnative

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/deviceinfo"
)

func TestEveryDemoActionUsesValidSharedServiceContracts(t *testing.T) {
	app, err := newApp()
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/features", "/features/inbox", "/features/secure", "/features/widgets", "/features/device"} {
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
