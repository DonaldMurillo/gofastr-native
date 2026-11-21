package main

import (
	"strings"
	"testing"
)

func TestCalendarBuildGating(t *testing.T) {
	a := app{Renderer: "native", Calendar: true, CalendarUsage: "Read & add selected events"}
	if err := validateDeviceFeatures(a, "ios-sim"); err != nil {
		t.Fatal(err)
	}
	out, err := render("templates/ios/Info.plist.tmpl", a)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"GofastrCalendar", "NSCalendarsFullAccessUsageDescription", "NSCalendarsWriteOnlyAccessUsageDescription", "NSCalendarsUsageDescription", "Read &amp; add selected events"} {
		if !strings.Contains(string(out), key) {
			t.Fatalf("missing %s", key)
		}
	}
	a.Calendar = false
	out, err = render("templates/ios/Info.plist.tmpl", a)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "NSCalendars") {
		t.Fatal("calendar enabled by default")
	}
	a.Calendar = true
	a.CalendarUsage = ""
	if validateDeviceFeatures(a, "ios-sim") == nil {
		t.Fatal("accepted empty purpose")
	}
	a.CalendarUsage = "Read"
	a.Renderer = "webview"
	if validateDeviceFeatures(a, "ios-sim") == nil {
		t.Fatal("accepted webview")
	}
}
