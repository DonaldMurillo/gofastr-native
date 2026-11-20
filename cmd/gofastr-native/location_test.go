package main

import (
	"bytes"
	"flag"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/extension"
)

func TestLocationFeatureConfiguration(t *testing.T) {
	for _, tt := range []struct {
		name, usage, renderer, platform string
		valid                           bool
	}{
		{"default", "", "native", "android-emu", true},
		{"enabled", "Show my position.", "native", "android-emu", true},
		{"ios", "Show my position.", "native", "ios-sim", false},
		{"ios device", "Show my position.", "native", "ios-device", false},
		{"webview", "Show my position.", "webview", "android-emu", false},
		{"control", "Show\nposition", "native", "android-emu", false},
		{"long", strings.Repeat("a", 513), "native", "android-emu", false},
		{"blank", "   ", "native", "android-emu", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := nativeApp(t)
			a.LocationUsage = tt.usage
			a.Renderer = tt.renderer
			if err := validateDeviceFeatures(a, tt.platform); (err == nil) != tt.valid {
				t.Fatalf("validateDeviceFeatures() = %v", err)
			}
		})
	}
	fs := flag.NewFlagSet("location", flag.ContinueOnError)
	flags := registerBuildFlags(fs)
	if err := fs.Parse([]string{"--location-usage", "Show my position."}); err != nil {
		t.Fatal(err)
	}
	if *flags.locationUsage != "Show my position." {
		t.Fatal("location flag not retained")
	}
}

func TestLocationManifestAndActivityAreOptIn(t *testing.T) {
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
		if enabled {
			a.LocationUsage = "Show my position."
		}
		manifest, err = render("templates/android/AndroidManifest.xml.tmpl", a)
		if err != nil {
			t.Fatal(err)
		}
		wellFormed(t, "location manifest", manifest)
		for _, permission := range []string{"ACCESS_COARSE_LOCATION", "ACCESS_FINE_LOCATION"} {
			want := 1
			if got := bytes.Count(manifest, []byte(permission)); got != want {
				t.Fatalf("%s count = %d, want %d", permission, got, want)
			}
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
		}
	}
}
