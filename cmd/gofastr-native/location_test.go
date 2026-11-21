package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/extension"
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
		{"android", "android-emu", "native", []string{"--location", "--location-usage", "Nearby"}, ""},
		{"android without purpose", "android-emu", "native", []string{"--location"}, ""},
		{"android purpose only", "android-emu", "native", []string{"--location-usage", "Nearby"}, "requires --location"},
		{"android webview", "android-emu", "webview", []string{"--location"}, "require --renderer native"},
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

func TestLocationAndroidManifestAndActivityAreOptIn(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		plain := nativeApp(t)
		manifest, err := render("templates/android/AndroidManifest.xml.tmpl", plain)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(manifest, []byte("ACCESS_COARSE_LOCATION")) || bytes.Contains(manifest, []byte("ACCESS_FINE_LOCATION")) {
			t.Fatal("unsolicited location permission")
		}
		a := nativeApp(t)
		a.Extensions = []compiledExtension{{manifest: extension.Manifest{Android: &extension.Android{Permissions: []string{"android.permission.ACCESS_COARSE_LOCATION", "android.permission.ACCESS_FINE_LOCATION"}}}}}
		a.Location = enabled
		manifest, err = render("templates/android/AndroidManifest.xml.tmpl", a)
		if err != nil {
			t.Fatal(err)
		}
		wellFormed(t, "location manifest", manifest)
		for _, permission := range []string{"ACCESS_COARSE_LOCATION", "ACCESS_FINE_LOCATION"} {
			if got := bytes.Count(manifest, []byte(permission)); got != 1 {
				t.Fatalf("%s count = %d, want 1", permission, got)
			}
		}
		if bytes.Contains(manifest, []byte("ACCESS_BACKGROUND_LOCATION")) {
			t.Fatal("unexpected background location permission")
		}
		activity, err := render("templates/android/NativeActivity.java.tmpl", a)
		if err != nil {
			t.Fatal(err)
		}
		expected := "locationEnabled() { return false; }"
		if enabled {
			expected = "locationEnabled() { return true; }"
		}
		if !bytes.Contains(activity, []byte(expected)) {
			t.Fatalf("missing %s", expected)
		}
		if enabled {
			for _, p := range a.AndroidExtensionPermissions() {
				if strings.HasSuffix(p, "ACCESS_FINE_LOCATION") || strings.HasSuffix(p, "ACCESS_COARSE_LOCATION") {
					t.Fatal("duplicate location permission")
				}
			}
			report := newReleaseReport(a, "android-emu")
			found := false
			for _, feature := range report.Features {
				found = found || feature == "location"
			}
			if !found {
				t.Fatal("release report omitted Android location")
			}
		}
	}
}
