package main

import (
	"bytes"
	"flag"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/extension"
)

func TestBackgroundFlagAndManifest(t *testing.T) {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	flags := registerBuildFlags(fs)
	if err := fs.Parse([]string{"--background-tasks"}); err != nil {
		t.Fatal(err)
	}
	if !*flags.backgroundTasks {
		t.Fatal("flag not parsed")
	}
	a := nativeApp(t)
	plain, _ := render("templates/android/AndroidManifest.xml.tmpl", a)
	if bytes.Contains(plain, []byte("BackgroundService")) {
		t.Fatal("enabled by default")
	}
	const networkPermission = "android.permission.ACCESS_NETWORK_STATE"
	if bytes.Contains(plain, []byte(networkPermission)) {
		t.Fatal("network-state permission enabled by default")
	}
	a.BackgroundTasks = true
	if err := validateDeviceFeatures(a, "android-emu"); err != nil {
		t.Fatal(err)
	}
	for _, platform := range []string{"ios-sim", "ios-device"} {
		if validateDeviceFeatures(a, platform) == nil {
			t.Fatal("accepted iOS")
		}
	}
	manifest, err := render("templates/android/AndroidManifest.xml.tmpl", a)
	if err != nil {
		t.Fatal(err)
	}
	wellFormed(t, "background manifest", manifest)
	if bytes.Count(manifest, []byte(networkPermission)) != 1 {
		t.Fatal("missing network-state permission")
	}
	a.Extensions = []compiledExtension{{manifest: extension.Manifest{Android: &extension.Android{Permissions: []string{networkPermission}}}}}
	manifest, err = render("templates/android/AndroidManifest.xml.tmpl", a)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(manifest, []byte(networkPermission)) != 1 {
		t.Fatal("duplicate network-state permission")
	}
	for _, value := range []string{`android:name=".BackgroundService"`, `android:permission="android.permission.BIND_JOB_SERVICE"`, `android:exported="false"`} {
		if !bytes.Contains(manifest, []byte(value)) {
			t.Fatal("missing", value)
		}
	}
	a.Renderer = "webview"
	if validateDeviceFeatures(a, "android-emu") == nil {
		t.Fatal("accepted WebView")
	}
}

func TestBackgroundHostCompilesAgainstSDK(t *testing.T) {
	sdk, err := findAndroidSDK(false)
	if err != nil {
		t.Skipf("Android SDK/JDK unavailable: %v", err)
	}
	a := nativeApp(t).forAndroid(sdk.api)
	a.BackgroundTasks = true
	directory, sources := androidHostSources(t, &a)
	args := append([]string{"--release", "11", "-encoding", "UTF-8", "-classpath", sdk.androidJar, "-d", filepath.Join(directory, "classes")}, sources...)
	if output, err := exec.Command(sdk.javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("background Android SDK compile: %v\n%s", err, output)
	}
}
