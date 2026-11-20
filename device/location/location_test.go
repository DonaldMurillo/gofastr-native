package location

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	for _, request := range []Request{{Operation: Status}, {Operation: Authorize, Reason: "Show nearby places"}, {Operation: Current}, {Operation: Current, Accuracy: Coarse, TimeoutMillis: 60000}, {Operation: Track, MaxSamples: 100, DurationMillis: 120000, DistanceFilterMeters: 5}} {
		if err := request.Validate(); err != nil {
			t.Fatalf("%+v: %v", request, err)
		}
	}
	for _, request := range []Request{{}, {Operation: Status, Reason: "prompt"}, {Operation: Authorize, Accuracy: Best}, {Operation: Current, TimeoutMillis: -1}, {Operation: Current, TimeoutMillis: 60001}, {Operation: Current, MaxSamples: 1}, {Operation: Current, DurationMillis: 1000}, {Operation: Current, DistanceFilterMeters: 1}, {Operation: Current, Accuracy: "invalid"}, {Operation: Current, Reason: "bad\nreason"}, {Operation: Track, MaxSamples: 1}, {Operation: Track, MaxSamples: 101, DurationMillis: 1000}, {Operation: Track, MaxSamples: 1, DurationMillis: 120001}, {Operation: Track, MaxSamples: 1, DurationMillis: 1000, TimeoutMillis: 1}, {Operation: Track, MaxSamples: 1, DurationMillis: 1000, DistanceFilterMeters: math.NaN()}} {
		if request.Validate() == nil {
			t.Fatalf("accepted %+v", request)
		}
	}
}

func TestAccept(t *testing.T) {
	fix := Fix{Latitude: 37.3349, Longitude: -122.009, HorizontalAccuracyMeters: 5, VerticalAccuracyMeters: -1, SpeedMetersPerSecond: -1, CourseDegrees: -1, TimestampMillis: 1}
	result := Result{Permission: WhenInUse, ServicesEnabled: true, Precise: true, Fixes: []Fix{fix}}
	for _, request := range []Request{{Operation: Current}, {Operation: Track, MaxSamples: 5, DurationMillis: 1000}} {
		if err := request.Accept(result); err != nil {
			t.Fatal(err)
		}
		empty := result
		empty.Fixes = nil
		if request.Accept(empty) == nil {
			t.Fatal("accepted no fixes")
		}
	}
	for _, permission := range []Permission{NotDetermined, Denied, Restricted, WhenInUse, Always} {
		if err := (Request{Operation: Status}).Accept(Result{Permission: permission}); err != nil {
			t.Fatal(err)
		}
	}
	if (Request{Operation: Status}).Accept(result) == nil {
		t.Fatal("status accepted fixes")
	}
	if (Request{Operation: Authorize}).Accept(result) == nil {
		t.Fatal("authorize accepted fixes")
	}
	for _, mutate := range []func(*Result){
		func(r *Result) { r.Permission = "unknown" }, func(r *Result) { r.Permission = Denied }, func(r *Result) { r.ServicesEnabled = false },
		func(r *Result) { r.Fixes = append(r.Fixes, fix) }, func(r *Result) { r.Fixes[0].Latitude = 91 }, func(r *Result) { r.Fixes[0].Longitude = -181 },
		func(r *Result) { r.Fixes[0].HorizontalAccuracyMeters = -1 }, func(r *Result) { r.Fixes[0].AltitudeMeters = math.Inf(1) },
		func(r *Result) { r.Fixes[0].VerticalAccuracyMeters = -2 }, func(r *Result) { r.Fixes[0].SpeedMetersPerSecond = -2 }, func(r *Result) { r.Fixes[0].CourseDegrees = 360 },
	} {
		bad := result
		bad.Fixes = []Fix{fix}
		mutate(&bad)
		if (Request{Operation: Current}).Accept(bad) == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
}

func TestRequiredWireFlags(t *testing.T) {
	encoded, err := json.Marshal(Result{Permission: NotDetermined})
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{`"servicesEnabled":false`, `"precise":false`} {
		if !strings.Contains(string(encoded), flag) {
			t.Fatalf("missing %s: %s", flag, encoded)
		}
	}
}
