package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestARCoreIsOptIn(t *testing.T) {
	a := nativeApp(t).forAndroid(34)
	plain, err := render("templates/android/AndroidManifest.xml.tmpl", a)
	if err != nil {
		t.Fatal(err)
	}
	a.ARCoreAAR = "core.aar"
	enabled, err := render("templates/android/AndroidManifest.xml.tmpl", a)
	if err != nil {
		t.Fatal(err)
	}
	wellFormed(t, "AR Optional manifest", enabled)
	for _, marker := range []string{"android.permission.CAMERA", "com.google.ar.core", "ARSessionActivity"} {
		if bytes.Contains(plain, []byte(marker)) || !bytes.Contains(enabled, []byte(marker)) {
			t.Errorf("%q should only occur in the explicitly enabled manifest", marker)
		}
	}
	if !bytes.Contains(enabled, []byte(`android:name="com.google.ar.core" android:value="optional"`)) {
		t.Fatal("AR must not exclude non-AR hardware")
	}
	if err := validateDeviceFeatures(a, "android-emu"); err != nil {
		t.Fatal(err)
	}
	if validateDeviceFeatures(a, "ios-sim") == nil {
		t.Fatal("ARCore works only on Android")
	}
	a.Renderer = "webview"
	if validateDeviceFeatures(a, "android-emu") == nil {
		t.Fatal("ARCore works only in the native renderer")
	}
}

func TestARCoreCompilesAgainstSDKAndGeneratedBindings(t *testing.T) {
	aar := os.Getenv("GOFASTR_ARCORE_AAR")
	if aar == "" {
		t.Skip("GOFASTR_ARCORE_AAR must point to the opted-in ARCore AAR")
	}
	sdk, err := findAndroidSDK(false)
	if err != nil {
		t.Skipf("Android SDK/JDK unavailable: %v", err)
	}
	a := nativeApp(t).forAndroid(sdk.api)
	a.ARCoreAAR = aar
	dir, sources := androidHostSources(t, &a)
	r := &runner{ctx: context.Background(), out: &bytes.Buffer{}}
	jni := filepath.Join(dir, "jni")
	if err := os.MkdirAll(jni, 0o700); err != nil {
		t.Fatal(err)
	}
	jars, err := prepareARCore(r, &a, dir, jni)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkAARABIs(jni, a.ABIs); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, "AndroidManifest.xml")
	if err := writeRendered("templates/android/AndroidManifest.xml.tmpl", manifest, a); err != nil {
		t.Fatal(err)
	}
	for _, component := range []string{"com.google.ar.core.min_apk_version", "com.google.ar.core.InstallActivity"} {
		if !strings.Contains(a.ARCoreComponents, component) {
			t.Errorf("SDK manifest component %q missing", component)
		}
	}
	resources, err := compileARCoreResources(r, sdk, dir)
	if err != nil {
		t.Fatal(err)
	}
	generated, _, err := linkAndroidLibraryResources(r, sdk, a, dir, manifest, append([]string{"--auto-add-overlay"}, resources...), []string{"com.google.ar.core"})
	if err != nil {
		t.Fatal(err)
	}
	classes := filepath.Join(dir, "classes")
	classpath := strings.Join(append([]string{sdk.androidJar}, jars...), string(os.PathListSeparator))
	args := append([]string{"--release", "11", "-encoding", "UTF-8", "-classpath", classpath, "-d", classes}, sources...)
	args = append(args, generated...)
	if output, err := exec.Command(sdk.javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("ARCore SDK compile: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(classes, "dev", "gofastr", "nativeui", "arcore", "ARSessionActivity.class")); err != nil {
		t.Fatal(err)
	}
}
