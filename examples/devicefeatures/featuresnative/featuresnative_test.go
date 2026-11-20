package featuresnative

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-native/device/calendar"
)

func TestEveryDemoActionUsesValidSharedServiceContracts(t *testing.T) {
	app, err := newApp()
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/features", "/features/inbox", "/features/secure", "/features/widgets", "/features/calendar"} {
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
