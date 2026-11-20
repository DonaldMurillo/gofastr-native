package ios

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/location"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func TestLocationSwiftContract(t *testing.T) {
	requireSwift(t)
	directory := t.TempDir()
	sources := swiftSources(t, directory, Sources, rendererSourceFiles...)
	fixture := map[string]any{
		"requests": []nativeui.ServiceRequest{
			{ID: "status", Location: &location.Request{Operation: location.Status}},
			{ID: "authorize", Location: &location.Request{Operation: location.Authorize, Reason: "Nearby places"}},
			{ID: "current", Location: &location.Request{Operation: location.Current, Accuracy: location.Best}},
			{ID: "track", Location: &location.Request{Operation: location.Track, Accuracy: location.Balanced, MaxSamples: 5, DurationMillis: 1000, DistanceFilterMeters: 10}},
		},
		"result": location.Result{Permission: location.WhenInUse, ServicesEnabled: true, Fixes: []location.Fix{{Latitude: 37.3349, Longitude: -122.009, HorizontalAccuracyMeters: 5, VerticalAccuracyMeters: -1, SpeedMetersPerSecond: -1, CourseDegrees: -1, TimestampMillis: 1234}}},
	}
	content, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	fixturePath, resultPath := filepath.Join(directory, "wire.json"), filepath.Join(directory, "result.json")
	if err := os.WriteFile(fixturePath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	compileRunChecks(t, directory, "location-checks", locationChecks,
		"location wire checks compile", "location wire checks", sources, []string{fixturePath, resultPath})
	content, err = os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var result nativeui.ServiceResult
	if err := json.Unmarshal(content, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != nativeui.EffectCompleted || result.Location == nil {
		t.Fatalf("missing location: %s", content)
	}
	if err := (location.Request{Operation: location.Current}).Accept(*result.Location); err != nil {
		t.Fatal(err)
	}
}

func TestLocationUIKitTypechecks(t *testing.T) {
	directory := t.TempDir()
	arguments := uikitSimulatorArguments(t, directory, "-parse-as-library", "-typecheck", "-module-name", "GofastrLocationChecks")
	arguments = append(arguments, swiftDirectorySources(t, directory, Sources)...)
	requireXcrun(t, "location adapter and service runner must compile without Go bindings", arguments...)
}

func TestLocationServiceNegotiationAndLifecycle(t *testing.T) {
	requireTokenMap(t, map[string][]string{
		"GofastrServiceRunner.swift": {`"location/v1": GofastrLocation.enabled ? "available" : "unavailable"`, "location.perform(locationRequest)", "if locationActive { location.cancel(); return }"},
		"GofastrLocation.swift":      {"import CoreLocation", "manager.requestWhenInUseAuthorization()", "locationManagerDidChangeAuthorization", "manager === self.manager", "timer?.invalidate()", "manager?.stopUpdatingLocation()", "manager?.delegate = nil", "self.completion = nil", "UIApplicationDidEnterBackgroundNotification", "kCLLocationAccuracyBest", "kCLLocationAccuracyHundredMeters", "kCLLocationAccuracyKilometer", "manager.allowsBackgroundLocationUpdates = false"},
	}, "%s lost location lifecycle wiring")
	native := readSource(t, Sources, "GofastrNative.swift")
	start, end := strings.Index(native, "func connect("), strings.Index(native, "func open(")
	if start < 0 || end <= start {
		t.Fatal("missing host negotiation")
	}
	if !strings.Contains(native[start:end], ".merging(GofastrServiceRunner.capabilities())") {
		t.Fatal("host omits location capabilities")
	}
}

const locationChecks = `
import Foundation
struct LocationFixture: Decodable { let requests: [MobileServiceRequest]; let result: MobileLocationResult }
@main struct Checks {
    static func main() throws {
        let fixture = try JSONDecoder().decode(LocationFixture.self, from: Data(contentsOf: URL(fileURLWithPath: CommandLine.arguments[1])))
        for request in fixture.requests { try request.validate() }
        precondition(fixture.result.fixes?.first?.latitude == 37.3349)
        precondition(fixture.result.wire["servicesEnabled"] as? Bool == true)
        precondition(fixture.result.wire["precise"] as? Bool == false)
        let empty = MobileLocationResult(permission: "not_determined", servicesEnabled: false, precise: false)
        precondition(empty.wire["servicesEnabled"] as? Bool == false && empty.wire["precise"] as? Bool == false)
        precondition(empty.wire["fixes"] == nil)
        for invalid in [
            #"{"operation":"unknown"}"#,
            #"{"operation":"status","reason":"prompt"}"#,
            #"{"operation":"authorize","accuracy":"best"}"#,
            #"{"operation":"current","accuracy":"invalid"}"#,
            #"{"operation":"current","timeoutMillis":60001}"#,
            #"{"operation":"current","maxSamples":1}"#,
            #"{"operation":"current","distanceFilterMeters":1}"#,
            #"{"operation":"track","maxSamples":101,"durationMillis":1000}"#,
            #"{"operation":"track","maxSamples":5,"durationMillis":999}"#,
            #"{"operation":"track","maxSamples":5,"durationMillis":120001}"#,
            #"{"operation":"track","maxSamples":5,"durationMillis":1000,"timeoutMillis":1}"#
        ] {
            let request = try JSONDecoder().decode(MobileLocationRequest.self, from: Data(invalid.utf8))
            do { try request.validate(); fatalError("accepted invalid location request") } catch {}
        }
        for wire in [#"{"permission":"denied","precise":false}"#, #"{"permission":"denied","servicesEnabled":false}"#] {
            do { _ = try JSONDecoder().decode(MobileLocationResult.self, from: Data(wire.utf8)); fatalError("accepted missing required flag") } catch {}
        }
        for status in ["cancelled", "failed", "denied", "unsupported"] {
            let result = MobileServiceResult(status: status, permission: "denied", location: fixture.result)
            precondition(result.wire["location"] == nil && result.wire["permission"] as? String == "denied")
        }
        let result = MobileServiceResult(status: "completed", location: fixture.result)
        try JSONSerialization.data(withJSONObject: result.wire).write(to: URL(fileURLWithPath: CommandLine.arguments[2]))
    }
}
`
