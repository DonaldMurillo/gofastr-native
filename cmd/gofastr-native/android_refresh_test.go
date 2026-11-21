package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"testing"
)

const featuresPkg = "github.com/DonaldMurillo/gofastr-native/examples/devicefeatures/featuresnative"

func TestAndroidBackgroundRefreshManifestAndHost(t *testing.T) {
	a := nativeApp(t)
	plain, _ := render("templates/android/AndroidManifest.xml.tmpl", a)
	if bytes.Contains(plain, []byte("BackgroundService")) {
		t.Fatal("background refresh service declared by default")
	}
	activity, _ := render("templates/android/NativeActivity.java.tmpl", a)
	if !bytes.Contains(activity, []byte("backgroundRefreshEnabled() { return false; }")) {
		t.Fatal("background refresh enabled by default")
	}
	a.BackgroundRefresh = true
	for _, platform := range []string{"android-emu", "android", "ios-sim", "ios-device"} {
		if err := validateDeviceFeatures(a, platform); err != nil {
			t.Fatal(platform, err)
		}
	}
	manifest, err := render("templates/android/AndroidManifest.xml.tmpl", a)
	if err != nil {
		t.Fatal(err)
	}
	wellFormed(t, "background refresh manifest", manifest)
	for _, value := range []string{`android:name=".BackgroundService"`, `android:permission="android.permission.BIND_JOB_SERVICE"`, `android:exported="false"`, "android.permission.RECEIVE_BOOT_COMPLETED"} {
		if !bytes.Contains(manifest, []byte(value)) {
			t.Fatal("missing", value)
		}
	}
	activity, _ = render("templates/android/NativeActivity.java.tmpl", a)
	if !bytes.Contains(activity, []byte("backgroundRefreshEnabled() { return true; }")) {
		t.Fatal("activity does not enable background refresh")
	}
	service, err := render("templates/android/BackgroundService.java.tmpl", a)
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []string{a.JavaClass + ".backgroundRefresh(timeoutMillis)", a.JavaClass + ".cancelBackgroundRefresh()"} {
		if !bytes.Contains(service, []byte(call)) {
			t.Fatal("job service does not call", call)
		}
	}
	a.Renderer = "webview"
	if validateDeviceFeatures(a, "android-emu") == nil {
		t.Fatal("accepted WebView")
	}
}

// The job service calls the bound package's BackgroundRefresh and
// CancelBackgroundRefresh, so it compiles only against a package that exports them.
func TestAndroidBackgroundRefreshCompilesAgainstSDK(t *testing.T) {
	sdk, err := findAndroidSDK(false)
	if err != nil {
		t.Skipf("Android SDK/JDK unavailable: %v", err)
	}
	application, err := newApp(featuresPkg, "featuresnative", "dev.gofastr.features", "Device Features", "")
	if err != nil {
		t.Fatal(err)
	}
	application.Renderer = "native"
	a := application.forAndroid(sdk.api)
	a.BackgroundRefresh = true
	directory, sources := androidHostSources(t, &a)
	args := append([]string{"--release", "11", "-encoding", "UTF-8", "-classpath", sdk.androidJar, "-d", filepath.Join(directory, "classes")}, sources...)
	if output, err := exec.Command(sdk.javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("background refresh Android SDK compile: %v\n%s", err, output)
	}
}
