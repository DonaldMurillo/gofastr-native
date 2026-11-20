package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDeviceFeaturesAreOptInAndValidatePlatformConfiguration(t *testing.T) {
	a := nativeApp(t)
	if err := validateDeviceFeatures(a, "android-emu"); err != nil {
		t.Fatal(err)
	}
	a.Widgets = true
	if validateDeviceFeatures(a, "android-emu") == nil {
		t.Fatal("widget without scheme accepted")
	}
	a.URLScheme = "feature-test"
	if validateDeviceFeatures(a, "ios-sim") == nil {
		t.Fatal("iOS widget without group accepted")
	}
	a.AppGroup = "group.dev.gofastr.test"
	a.ReceiveShares, a.SecureStorage = true, true
	if validateDeviceFeatures(a, "ios-sim") == nil {
		t.Fatal("iOS biometrics without purpose accepted")
	}
	a.BiometricUsage = "Authenticate to read the demo secret."
	if err := validateDeviceFeatures(a, "ios-sim"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"templates/android/AndroidManifest.xml.tmpl", "templates/android/widget.xml.tmpl", "templates/ios/Info.plist.tmpl", "templates/ios/ShareExtension.plist.tmpl", "templates/ios/WidgetExtension.plist.tmpl", "templates/ios/DeviceFeatures.entitlements.tmpl"} {
		content, err := render(name, a)
		if err != nil {
			t.Fatal(err)
		}
		wellFormed(t, name, content)
	}
	manifest, _ := render("templates/android/AndroidManifest.xml.tmpl", a)
	for _, expected := range []string{"android.intent.action.SEND", "USE_BIOMETRIC", "HomeWidget", "@xml/gofastr_widget"} {
		if !bytes.Contains(manifest, []byte(expected)) {
			t.Error("missing", expected)
		}
	}
	plain := nativeApp(t)
	manifest, _ = render("templates/android/AndroidManifest.xml.tmpl", plain)
	for _, absent := range []string{"android.intent.action.SEND", "USE_BIOMETRIC", "HomeWidget"} {
		if bytes.Contains(manifest, []byte(absent)) {
			t.Error("unsolicited feature", absent)
		}
	}
	a.Renderer = "webview"
	if validateDeviceFeatures(a, "android-emu") == nil {
		t.Fatal("native flags accepted by WebView")
	}
}

func TestPushOptIn(t *testing.T) {
	a := nativeApp(t)
	for _, name := range []string{"templates/ios/Info.plist.tmpl", "templates/ios/DeviceFeatures.entitlements.tmpl"} {
		plain, err := render(name, a)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(plain, []byte("GofastrPush")) || bytes.Contains(plain, []byte("aps-environment")) {
			t.Fatal("push enabled without opt-in")
		}
		a.Push = true
		content, err := render(name, a)
		if err != nil {
			t.Fatal(err)
		}
		wellFormed(t, name, content)
		if !bytes.Contains(content, []byte("GofastrPush")) && !bytes.Contains(content, []byte("<key>aps-environment</key><string>development</string>")) {
			t.Fatal("missing push opt-in")
		}
		a.Push = false
	}
	a.Push = true
	var plan bytes.Buffer
	if _, err := buildIOS(&runner{ctx: context.Background(), dryRun: true, out: &plan}, a, filepath.Join(t.TempDir(), "push")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "__TEXT -Xlinker __entitlements") {
		t.Fatal("simulator APNs entitlements are not embedded")
	}
	if err := validateDeviceFeatures(a, "ios-sim"); err != nil {
		t.Fatal(err)
	}
	if validateDeviceFeatures(a, "android-emu") == nil {
		t.Fatal("iOS-only push accepted on Android")
	}
	a.Renderer = "webview"
	if validateDeviceFeatures(a, "ios-sim") == nil {
		t.Fatal("native push accepted on WebView")
	}
	for _, environment := range []string{"development", "production"} {
		if err := (entitlementSet{"aps-environment": environment}).allowedBy(provisioningProfile{APSEnvironment: environment}); err != nil {
			t.Fatal(err)
		}
	}
	if (entitlementSet{"aps-environment": "development"}).allowedBy(provisioningProfile{}) == nil {
		t.Fatal("ungranted APNs entitlement accepted")
	}
}

func TestIOSFeatureExtensionPlanKeepsExecutablesAndKeychainSeparate(t *testing.T) {
	a := nativeApp(t)
	a.ReceiveShares, a.Widgets, a.SecureStorage = true, true, true
	a.AppGroup, a.URLScheme = "group.dev.gofastr.features", "feature-test"
	var plan bytes.Buffer
	directory := filepath.Join(t.TempDir(), "unwritten")
	if err := buildIOSFeatureExtensions(&runner{ctx: context.Background(), dryRun: true, out: &plan}, a, directory, filepath.Join(directory, "Features.app"), "<sdk>", "arm64-apple-ios16.0-simulator"); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"-application-extension", "-Xlinker -e -Xlinker _NSExtensionMain", "GofastrShareViewController.swift", "GofastrWidgetExtension.swift", "PlugIns", "--entitlements"} {
		if !strings.Contains(plan.String(), expected) {
			t.Error("extension plan omits", expected)
		}
	}
	if strings.Contains(plan.String(), "-framework Notesnative") {
		t.Fatal("extension links the Go runtime")
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("dry run wrote extension output")
	}
	content, _ := render("templates/ios/DeviceFeatures.entitlements.tmpl", a)
	if !bytes.Contains(content, []byte("keychain-access-groups")) {
		t.Fatal("host has no private Keychain group")
	}
	a.SecureStorage = false
	content, _ = render("templates/ios/DeviceFeatures.entitlements.tmpl", a)
	if bytes.Contains(content, []byte("keychain-access-groups")) || bytes.Contains(content, []byte("application-identifier")) {
		t.Fatal("extension inherits host secret access")
	}
}

func TestAndroidWidgetResourcesCompileAndLink(t *testing.T) {
	sdk, err := findAndroidSDK(false)
	if err != nil {
		t.Skip(err)
	}
	a := nativeApp(t).forAndroid(sdk.api)
	a.Widgets, a.ReceiveShares, a.SecureStorage = true, true, true
	a.URLScheme, a.BiometricUsage = "feature-test", "Read a demo secret."
	dir := t.TempDir()
	r := &runner{ctx: context.Background(), out: io.Discard}
	resources, err := prepareAndroidFeatureResources(r, sdk, a, dir)
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, "AndroidManifest.xml")
	if err := writeRendered("templates/android/AndroidManifest.xml.tmpl", manifest, a); err != nil {
		t.Fatal(err)
	}
	if err := r.run("link feature resources", sdk.tool("aapt2"), append([]string{"link", "-o", filepath.Join(dir, "features.apk"), "--manifest", manifest, "-I", sdk.androidJar}, resources...)...); err != nil {
		t.Fatal(err)
	}
}

func TestIOSDeviceFeatureExtensionsCompileAgainstSimulatorSDK(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("iOS extension compilation requires macOS and Xcode")
	}
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("Xcode toolchain unavailable")
	}
	a := nativeApp(t)
	a.ReceiveShares, a.Widgets = true, true
	a.AppGroup, a.URLScheme = "group.dev.gofastr.featuretest", "feature-test"
	var output bytes.Buffer
	t.Cleanup(func() { t.Log(output.String()) })
	r := &runner{ctx: context.Background(), out: &output}
	sdk, err := r.output("find simulator SDK", "", "xcrun", "--sdk", "iphonesimulator", "--show-sdk-path")
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	if err := buildIOSFeatureExtensions(r, a, dir, filepath.Join(dir, "FeatureTest.app"), sdk, simArch()+"-apple-ios16.0-simulator"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Share", "Widget"} {
		bundle := filepath.Join(dir, "FeatureTest.app", "PlugIns", name+".appex")
		if info, err := os.Stat(filepath.Join(bundle, name)); err != nil || info.Size() == 0 {
			t.Fatalf("missing compiled %s extension: %v", name, err)
		}
		if err := r.run("verify extension signature", "codesign", "--verify", "--strict", bundle); err != nil {
			t.Fatal(err)
		}
	}
}
