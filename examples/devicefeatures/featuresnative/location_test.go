package featuresnative

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/location"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func TestLocationResultsAreVisibleWithStableIDs(t *testing.T) {
	routes, ctx := newLocationRoutes(), context.Background()
	route := routes[0]
	for index, operation := range []string{"status", "authorize", "current", "track"} {
		response, err := route.Actions[operation](ctx, nativeui.Request{})
		if err != nil {
			t.Fatal(err)
		}
		if err := response.Service.Validate(); err != nil {
			t.Fatal(err)
		}
		if string(response.Service.Location.Operation) != operation {
			t.Fatal("wrong location operation")
		}
		if operation == "track" && (response.Service.Location.MaxSamples != 5 || response.Service.Location.DurationMillis != 5000) {
			t.Fatal("tracking must be bounded to 5 samples over 5 seconds")
		}
		observed := &location.Result{Permission: location.WhenInUse, ServicesEnabled: true}
		if operation == "current" || operation == "track" {
			observed.Fixes = []location.Fix{{Latitude: 37.3349, Longitude: -122.009}}
		}
		for _, result := range []nativeui.ServiceResult{
			{Status: nativeui.EffectCompleted, Location: observed},
			{Status: nativeui.EffectDenied, Permission: "denied"}, {Status: nativeui.EffectFailed},
		} {
			if _, err := route.Services["location"](ctx, nativeui.Request{Service: response.Service}, result); err != nil {
				t.Fatal(err)
			}
			screen, err := route.Screen(ctx, nativeui.Request{})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, row := range screen.List.Rows {
				if row.ID != "location-"+operation {
					continue
				}
				found = true
				if !strings.Contains(row.Subtitle, string(result.Status)) {
					t.Fatal("missing observed status")
				}
				if result.Location != nil && len(result.Location.Fixes) > 0 && !strings.Contains(row.Subtitle, "37.3349, -122.0090") {
					t.Fatal("missing visible coordinates")
				}
				if result.Status != nativeui.EffectCompleted && strings.Contains(row.Subtitle, "37.3349") {
					t.Fatal("failed result kept stale coordinates")
				}
				detail, err := routes[index+1].Screen(ctx, nativeui.Request{})
				if err != nil {
					t.Fatal(err)
				}
				if detail.Text != row.Subtitle {
					t.Fatal("detail must display the full result")
				}
			}
			if !found {
				t.Fatal("missing stable result row")
			}
		}
	}
}

func TestLocationInitiallyShowsNoCoordinates(t *testing.T) {
	routes := newLocationRoutes()
	screen, err := routes[0].Screen(context.Background(), nativeui.Request{})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range screen.List.Rows {
		if row.Subtitle != "Not requested" {
			t.Fatal("unexpected initial location")
		}
	}
	for _, route := range routes[1:] {
		detail, err := route.Screen(context.Background(), nativeui.Request{})
		if err != nil {
			t.Fatal(err)
		}
		if detail.Text != "Not requested" {
			t.Fatal("unexpected initial detail")
		}
	}
}

func TestLocationFailureMessageIsShown(t *testing.T) {
	route, ctx := newLocationRoutes()[0], context.Background()
	response, err := route.Actions["current"](ctx, nativeui.Request{})
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range []struct {
		result nativeui.ServiceResult
		want   string
	}{
		{nativeui.ServiceResult{Status: nativeui.EffectUnsupported, Permission: "when_in_use", Message: "Location services are off."}, "unsupported; permission: when_in_use; Location services are off."},
		{nativeui.ServiceResult{Status: nativeui.EffectFailed, Message: "No location fix arrived in time."}, "failed; No location fix arrived in time."},
		{nativeui.ServiceResult{Status: nativeui.EffectDenied, Permission: "denied"}, "denied; permission: denied"},
	} {
		got, err := route.Services["location"](ctx, nativeui.Request{Service: response.Service}, outcome.result)
		if err != nil {
			t.Fatal(err)
		}
		if got.Announcement != outcome.want {
			t.Fatalf("announcement = %q, want %q", got.Announcement, outcome.want)
		}
	}
}
