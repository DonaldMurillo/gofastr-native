package location

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	for _, tt := range []struct {
		name    string
		request Request
		valid   bool
	}{
		{"empty", Request{}, false}, {"status", Request{Operation: Status}, true},
		{"status accuracy", Request{Operation: Status, Accuracy: Precise}, false},
		{"status age", Request{Operation: Status, MaxAgeSeconds: 1}, false},
		{"status timeout", Request{Operation: Status, TimeoutSeconds: 1}, false},
		{"missing accuracy", Request{Operation: Current}, false},
		{"precise", Request{Operation: Current, Accuracy: Precise}, true},
		{"approximate bounds", Request{Current, Approximate, 600, 60}, true},
		{"negative age", Request{Current, Precise, -1, 0}, false},
		{"old age", Request{Current, Precise, 601, 0}, false},
		{"negative timeout", Request{Current, Precise, 0, -1}, false},
		{"long timeout", Request{Current, Precise, 0, 61}, false},
		{"unknown accuracy", Request{Current, "other", 0, 0}, false},
		{"unknown operation", Request{Operation: "other"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.request.Validate(); (err == nil) != tt.valid {
				t.Fatalf("Validate() = %v", err)
			}
		})
	}
}

func TestAccept(t *testing.T) {
	current := Request{Operation: Current, Accuracy: Precise}
	for _, tt := range []struct {
		name   string
		change func(*Result)
		valid  bool
	}{
		{"normal", func(r *Result) {}, true},
		{"approximate grant", func(r *Result) { r.Permission = "approximate" }, true},
		{"empty", func(r *Result) { *r = Result{} }, false},
		{"no fix", func(r *Result) { r.Fix = nil }, false},
		{"denied", func(r *Result) { r.Permission = "denied" }, false},
		{"undetermined", func(r *Result) { r.Permission = "undetermined" }, false},
		{"unknown permission", func(r *Result) { r.Permission = "other" }, false},
		{"services off", func(r *Result) { r.ServicesEnabled = false }, false},
		{"latitude", func(r *Result) { r.Fix.Latitude = 91 }, false},
		{"longitude", func(r *Result) { r.Fix.Longitude = -181 }, false},
		{"accuracy", func(r *Result) { r.Fix.AccuracyMeters = -1 }, false},
		{"nan", func(r *Result) { r.Fix.Latitude = math.NaN() }, false},
		{"inf", func(r *Result) { r.Fix.Longitude = math.Inf(1) }, false},
		{"accuracy inf", func(r *Result) { r.Fix.AccuracyMeters = math.Inf(-1) }, false},
		{"no time", func(r *Result) { r.Fix.Time = time.Time{} }, false},
		{"altitude nan", func(r *Result) { v := math.NaN(); r.Fix.Altitude = &v }, false},
		{"speed negative", func(r *Result) { v := -1.0; r.Fix.Speed = &v }, false},
		{"speed inf", func(r *Result) { v := math.Inf(1); r.Fix.Speed = &v }, false},
		{"bearing negative", func(r *Result) { v := -1.0; r.Fix.Bearing = &v }, false},
		{"bearing full circle", func(r *Result) { v := 360.0; r.Fix.Bearing = &v }, false},
		{"bearing nan", func(r *Result) { v := math.NaN(); r.Fix.Bearing = &v }, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := Result{Fix: &Fix{Latitude: 37.422, Longitude: -122.084, AccuracyMeters: 5, Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, Permission: "precise", ServicesEnabled: true}
			tt.change(&r)
			if err := current.Accept(r); (err == nil) != tt.valid {
				t.Fatalf("Accept() = %v", err)
			}
		})
	}
	status := Request{Operation: Status}
	for _, permission := range []string{"precise", "approximate", "denied", "undetermined"} {
		if err := status.Accept(Result{Permission: permission}); err != nil {
			t.Fatal(err)
		}
	}
	if status.Accept(Result{Permission: "precise", Fix: &Fix{}}) == nil || status.Accept(Result{}) == nil || (Request{}).Accept(Result{}) == nil {
		t.Fatal("invalid result accepted")
	}
}

func TestWireShape(t *testing.T) {
	encoded, err := json.Marshal(Request{Current, Approximate, 60, 10})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"operation":"current","accuracy":"approximate","maxAgeSeconds":60,"timeoutSeconds":10}` {
		t.Fatal(string(encoded))
	}
	encoded, err = json.Marshal(Result{Permission: "undetermined"})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"permission":"undetermined","servicesEnabled":false}` {
		t.Fatal(string(encoded))
	}
	encoded, err = json.Marshal(Fix{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "altitude") || !strings.Contains(string(encoded), `"time":"2026-01-01T00:00:00Z"`) {
		t.Fatal(string(encoded))
	}
}
