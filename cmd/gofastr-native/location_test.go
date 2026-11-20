package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestLocationFlagsAndValidation(t *testing.T) {
	for _, test := range []struct {
		name, platform, renderer string
		flags                    []string
		want                     string
	}{
		{"simulator", "ios-sim", "native", []string{"--location", "--location-usage", "Show nearby places."}, ""},
		{"missing purpose", "ios-sim", "native", []string{"--location"}, "requires --location-usage"},
		{"device missing purpose", "ios-device", "native", []string{"--location"}, "requires --location-usage"},
		{"purpose only", "ios-sim", "native", []string{"--location-usage", "Nearby"}, "requires --location"},
		{"blank purpose", "ios-sim", "native", []string{"--location", "--location-usage", "  "}, "requires --location-usage"},
		{"control", "ios-sim", "native", []string{"--location", "--location-usage", "Show\nnearby"}, "without control characters"},
		{"oversized", "ios-sim", "native", []string{"--location", "--location-usage", strings.Repeat("a", 513)}, "at most 512"},
		{"webview", "ios-sim", "webview", []string{"--location", "--location-usage", "Nearby"}, "require --renderer native"},
		{"android", "android-emu", "native", []string{"--location", "--location-usage", "Nearby"}, "supported only on iOS"},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := []string{"build", "--platform", test.platform, "--pkg", nativePkg, "--renderer", test.renderer, "--dry-run"}
			code, stdout, stderr := runCLI(t, append(args, test.flags...)...)
			if test.want == "" {
				if code != 0 {
					t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
				}
			} else if code != 2 || !strings.Contains(stderr, test.want) {
				t.Fatalf("exit %d, stderr %q; want %q", code, stderr, test.want)
			}
		})
	}
}

func TestLocationPlistAndReleaseReport(t *testing.T) {
	a := nativeApp(t)
	plain, err := render("templates/ios/Info.plist.tmpl", a)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(plain, []byte("Location")) {
		t.Fatal("unsolicited location purpose or opt-in")
	}
	a.Location, a.LocationUsage = true, "Show places <nearby> & directions."
	if err := validateDeviceFeatures(a, "ios-device"); err != nil {
		t.Fatal(err)
	}
	plist, err := render("templates/ios/Info.plist.tmpl", a)
	if err != nil {
		t.Fatal(err)
	}
	wellFormed(t, "location plist", plist)
	for _, expected := range []string{"<key>GofastrLocation</key><true/>", "NSLocationWhenInUseUsageDescription", "Show places &lt;nearby&gt; &amp; directions."} {
		if !bytes.Contains(plist, []byte(expected)) {
			t.Fatalf("missing %s in %s", expected, plist)
		}
	}
	for _, forbidden := range []string{"NSLocationAlways", "UIBackgroundModes"} {
		if bytes.Contains(plist, []byte(forbidden)) {
			t.Fatalf("unexpected background permission: %s", forbidden)
		}
	}
	for _, platform := range []string{"ios-sim", "ios-device"} {
		report := newReleaseReport(a, platform)
		found := false
		for _, feature := range report.Features {
			if feature == "location" {
				found = true
			}
		}
		if !found {
			t.Fatal("release report omitted location")
		}
	}
}

func TestInspectRecognizesLocationCapability(t *testing.T) {
	found := false
	for _, capability := range inspectCapabilities {
		if capability == "location/v1" {
			found = true
		}
	}
	if !found {
		t.Fatal("inspect drops location capability")
	}
	for _, profile := range inspectProfiles() {
		if profile.Caps["location/v1"] == "available" {
			t.Fatal("default profile advertises opt-in location")
		}
	}
}
