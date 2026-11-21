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

func TestSignInWithAppleRequiresNativeIOSAndProvisionedEntitlement(t *testing.T) {
	a := nativeApp(t)
	a.SignInWithApple = true
	if err := validateDeviceFeatures(a, "ios-sim"); err != nil {
		t.Fatal(err)
	}
	if validateDeviceFeatures(a, "android-emu") == nil {
		t.Fatal("Android accepted --sign-in-with-apple")
	}
	plist, err := render("templates/ios/Info.plist.tmpl", a)
	if err != nil || !bytes.Contains(plist, []byte("<key>GofastrSignInWithApple</key><true/>")) {
		t.Fatalf("missing app plist configuration: %v", err)
	}
	signed := entitlementSet{entSignInWithApple: []string{"Default"}}
	f := newFakeProfile(a.ID)
	if signed.allowedBy(parsedFake(t, f)) == nil {
		t.Fatal("profile without Sign in with Apple grants the entitlement")
	}
	f.appleSignIn = []string{"Default"}
	if err := signed.allowedBy(parsedFake(t, f)); err != nil {
		t.Fatal(err)
	}
}

func TestDeviceFeaturesAreOptInAndValidatePlatformConfiguration(t *testing.T) {
	a := nativeApp(t)
	a.NetworkStatus = true
	if err := validateDeviceFeatures(a, "android-emu"); err != nil {
		t.Fatal(err)
	}
	if validateDeviceFeatures(a, "ios-sim") == nil {
		t.Fatal("network accepted on iOS")
	}
	networkManifest, err := render("templates/android/AndroidManifest.xml.tmpl", a)
	if err != nil || !bytes.Contains(networkManifest, []byte("ACCESS_NETWORK_STATE")) {
		t.Fatalf("network permission missing: %v", err)
	}
	bootstrap, err := render("templates/android/NativeActivity.java.tmpl", a)
	if err != nil || !bytes.Contains(bootstrap, []byte("networkStatusEnabled() { return true; }")) {
		t.Fatalf("network bootstrap missing: %v", err)
	}
	a.Renderer = "webview"
	if validateDeviceFeatures(a, "android-emu") == nil {
		t.Fatal("network accepted on WebView")
	}
	a.Renderer = "native"
	a.NetworkStatus = false
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
	for _, absent := range []string{"ACCESS_NETWORK_STATE", "android.intent.action.SEND", "USE_BIOMETRIC", "HomeWidget"} {
		if bytes.Contains(manifest, []byte(absent)) {
			t.Error("unsolicited feature", absent)
		}
	}
	a.BackgroundRefresh = true
	if validateDeviceFeatures(a, "android-emu") == nil {
		t.Fatal("Android background refresh accepted")
	}
	if err := validateDeviceFeatures(a, "ios-device"); err != nil {
		t.Fatal(err)
	}
	plist, _ := render("templates/ios/Info.plist.tmpl", a)
	wellFormed(t, "Info.plist", plist)
	host, _ := render("templates/ios/NativeApp.swift.tmpl", a)
	for _, expected := range [][]byte{[]byte("<key>GofastrBackgroundRefresh</key><string>" + a.ID + ".refresh</string>"), []byte("BGTaskSchedulerPermittedIdentifiers"), []byte("<string>fetch</string>")} {
		if !bytes.Contains(plist, expected) {
			t.Errorf("Info.plist omits %s", expected)
		}
	}
	if !bytes.Contains(host, []byte("GofastrBackgroundRefresh.register(ready: renderer.afterStart")) || !bytes.Contains(host, []byte(a.Prefix+"CancelBackgroundRefresh()")) {
		t.Error("app delegate does not register background refresh")
	}
	plain = nativeApp(t)
	plist, _ = render("templates/ios/Info.plist.tmpl", plain)
	host, _ = render("templates/ios/NativeApp.swift.tmpl", plain)
	if bytes.Contains(plist, []byte("UIBackgroundModes")) || bytes.Contains(host, []byte("BackgroundRefresh")) {
		t.Error("unsolicited background refresh")
	}
	a.BackgroundRefresh = false
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
