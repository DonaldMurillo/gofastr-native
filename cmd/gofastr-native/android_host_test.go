package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	renderandroid "github.com/DonaldMurillo/gofastr-native/render/android"
)

func TestNativeAndroidCompilesAgainstSDKAndGeneratedBindings(t *testing.T) {
	sdk, err := findAndroidSDK(false)
	if err != nil {
		t.Skipf("Android SDK/JDK unavailable: %v", err)
	}
	a := nativeApp(t).forAndroid(sdk.api)
	directory, sources := androidHostSources(t, &a)
	for _, kind := range []string{"file", "component"} {
		files, err := scaffoldFiles("example.android-"+kind+"/v1", kind)
		if err != nil {
			t.Fatal(err)
		}
		for name, content := range files {
			if strings.HasSuffix(name, ".java") {
				path := filepath.Join(directory, "scaffold-"+kind, name)
				if err := writeSource(path, content); err != nil {
					t.Fatal(err)
				}
				sources = append(sources, path)
			}
		}
	}
	args := append([]string{"--release", "11", "-encoding", "UTF-8", "-classpath", sdk.androidJar, "-d", filepath.Join(directory, "classes")}, sources...)
	if output, err := exec.Command(sdk.javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("native Android SDK compile: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(directory, "classes", "dev", "gofastr", "nativeui", "DeviceLocation.class")); err != nil {
		t.Fatalf("DeviceLocation.java was not compiled: %v", err)
	}
	t.Log("SDK javac compiled DeviceLocation.java against generated Go bindings")
	entries, _ := renderandroid.Sources.ReadDir(".")
	for _, entry := range entries {
		want, _ := renderandroid.Sources.ReadFile(entry.Name())
		got, err := os.ReadFile(filepath.Join(directory, "renderer", entry.Name()))
		if err != nil || !bytes.Equal(want, got) {
			t.Fatalf("renderer source changed: %s %v", entry.Name(), err)
		}
	}
	bootstrap, _ := os.ReadFile(sources[0])
	if len(bootstrap) > 4096 || bytes.Contains(bootstrap, []byte("EditText")) || bytes.Contains(bootstrap, []byte("WebView")) {
		t.Fatal("native Android bootstrap contains rendering logic")
	}
	manifest, err := render("templates/android/AndroidManifest.xml.tmpl", a)
	if err != nil {
		t.Fatal(err)
	}
	wellFormed(t, "native Android manifest", manifest)
	for _, expected := range []string{"TransferProvider", "ReminderReceiver", "POST_NOTIFICATIONS"} {
		if !bytes.Contains(manifest, []byte(expected)) {
			t.Errorf("manifest omits %s", expected)
		}
	}
}

// androidHostSources discovers a's extensions, writes its Android host and
// returns the Java sources javac needs: host, renderer, extensions, the
// go.Seq stand-ins and gobind's output for every bound package.
func androidHostSources(t *testing.T, a *app) (string, []string) {
	t.Helper()
	var err error
	a.Extensions, err = discoverPlatformExtensions(&runner{ctx: context.Background(), out: io.Discard}, a.PkgPath, "android")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	sources, err := prepareAndroidHost(&runner{ctx: context.Background(), out: io.Discard}, *a, directory)
	if err != nil {
		t.Fatal(err)
	}
	// Only go.Seq/Universe need stand-ins. All Android APIs are checked against
	// the real SDK, and binding names/signatures against the pinned gobind.
	fixture, err := os.ReadFile(filepath.Join("testdata", "androidstubs.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, chunk := range strings.Split(strings.ReplaceAll(string(fixture), "\r\n", "\n"), "\n--- ")[1:] {
		name, body, _ := strings.Cut(chunk, "\n")
		if !strings.HasPrefix(name, "go/") {
			continue
		}
		path := filepath.Join(directory, filepath.FromSlash(name))
		if err := writeSource(path, []byte(body)); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	bindings := gobindPackageOut(t, a.androidBindPackages()...)
	java, err := listFiles(filepath.Join(bindings, "java"), ".java")
	if err != nil {
		t.Fatal(err)
	}
	return directory, append(sources, java...)
}

// The capture extension's camera, sensor and service Java compiles against
// the SDK and the bindings gomobile generates for its capturebind package,
// and the manifest declares its permissions and foreground service.
func TestCaptureExtensionCompilesAgainstSDKAndGeneratedBindings(t *testing.T) {
	sdk, err := findAndroidSDK(false)
	if err != nil {
		t.Skipf("Android SDK/JDK unavailable: %v", err)
	}
	application, err := newApp("github.com/DonaldMurillo/gofastr-native/examples/capture/capturenative", "capturenative", "dev.gofastr.capturenative", "Capture", "")
	if err != nil {
		t.Fatal(err)
	}
	application.Renderer = "native"
	a := application.forAndroid(sdk.api)
	directory, sources := androidHostSources(t, &a)
	if got := a.androidBindPackages(); len(got) != 2 || got[1] != "github.com/DonaldMurillo/gofastr-native/extension/capture/capturebind" {
		t.Fatalf("bind packages = %v", got)
	}
	copied := false
	for _, source := range sources {
		copied = copied || filepath.Base(source) == "GofastrCaptureEngine.java"
	}
	if !copied {
		t.Fatal("capture extension sources were not copied into the host")
	}
	args := append([]string{"--release", "11", "-encoding", "UTF-8", "-classpath", sdk.androidJar, "-d", filepath.Join(directory, "classes")}, sources...)
	if output, err := exec.Command(sdk.javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("capture Android SDK compile: %v\n%s", err, output)
	}
	manifest, err := render("templates/android/AndroidManifest.xml.tmpl", a)
	if err != nil {
		t.Fatal(err)
	}
	wellFormed(t, "capture Android manifest", manifest)
	for _, expected := range []string{
		`"android.permission.CAMERA"`, `"android.permission.ACCESS_FINE_LOCATION"`, `"android.permission.FOREGROUND_SERVICE_CAMERA"`,
		`android:name="dev.gofastr.nativeui.GofastrCaptureService"`, `android:foregroundServiceType="camera|location|microphone"`,
	} {
		if !bytes.Contains(manifest, []byte(expected)) {
			t.Errorf("manifest omits %s", expected)
		}
	}
	if n := bytes.Count(manifest, []byte(`"android.permission.CAMERA"`)); n != 1 {
		t.Errorf("manifest declares CAMERA %d times", n)
	}
}

func TestBarcodeExtensionCompilesAgainstSDKAndGeneratedBindings(t *testing.T) {
	sdk, err := findAndroidSDK(false)
	if err != nil {
		t.Skipf("Android SDK/JDK unavailable: %v", err)
	}
	application, err := newApp("github.com/DonaldMurillo/gofastr-native/examples/devicefeatures/featuresnative", "featuresnative", "dev.gofastr.featuresnative", "Device features", "")
	if err != nil {
		t.Fatal(err)
	}
	application.Renderer = "native"
	a := application.forAndroid(sdk.api)
	directory, sources := androidHostSources(t, &a)
	if got := a.androidBindPackages(); len(got) != 2 || got[1] != "github.com/DonaldMurillo/gofastr-native/extension/barcode/barcodebind" {
		t.Fatalf("bind packages = %v", got)
	}
	args := append([]string{"--release", "11", "-encoding", "UTF-8", "-classpath", sdk.androidJar, "-d", filepath.Join(directory, "classes")}, sources...)
	if output, err := exec.Command(sdk.javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("barcode Android SDK compile: %v\n%s", err, output)
	}
	manifest, err := render("templates/android/AndroidManifest.xml.tmpl", a)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(manifest, []byte(`"android.permission.CAMERA"`)) {
		t.Fatal("delegated camera capture should not declare CAMERA")
	}
}

// The WebView shell of an app that uses capture declares and grants the
// camera, location and microphone, and compiles against the SDK and the
// app's generated bindings.
func TestCaptureWebViewShellGrantsDeclaredFeatures(t *testing.T) {
	const pkg = "github.com/DonaldMurillo/gofastr-native/examples/capture/capturewebview"
	application, err := newApp(pkg, "capturewebview", "dev.gofastr.capturewebview", "Capture", "")
	if err != nil {
		t.Fatal(err)
	}
	application.Renderer = "webview"
	application.MicrophoneUsage = "Record sound with your video."
	a := application.forAndroid(34)
	a.WebViewFeatures, err = discoverWebViewFeatures(&runner{ctx: context.Background(), out: io.Discard}, pkg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(a.WebViewFeatures, ",") != "camera,geolocation" {
		t.Fatalf("WebView features = %v", a.WebViewFeatures)
	}
	manifest, err := render("templates/android/AndroidManifest.xml.tmpl", a)
	if err != nil {
		t.Fatal(err)
	}
	wellFormed(t, "capture WebView manifest", manifest)
	for _, expected := range []string{`"android.permission.CAMERA"`, `"android.permission.ACCESS_FINE_LOCATION"`, `"android.permission.ACCESS_COARSE_LOCATION"`, `"android.permission.RECORD_AUDIO"`} {
		if !bytes.Contains(manifest, []byte(expected)) {
			t.Errorf("manifest omits %s", expected)
		}
	}
	if bytes.Contains(manifest, []byte("FOREGROUND_SERVICE")) || bytes.Contains(manifest, []byte("GofastrCaptureService")) {
		t.Error("the WebView shell declares the native adapter's service")
	}
	activity, err := render("templates/android/MainActivity.java.tmpl", a)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"onPermissionRequest", "RESOURCE_VIDEO_CAPTURE", "RESOURCE_AUDIO_CAPTURE", "onGeolocationPermissionsShowPrompt", "setMediaPlaybackRequiresUserGesture(false)"} {
		if !bytes.Contains(activity, []byte(expected)) {
			t.Errorf("WebView shell omits %s", expected)
		}
	}

	// Without features or a microphone purpose, the shell grants nothing.
	plain := a
	plain.WebViewFeatures, plain.MicrophoneUsage = nil, ""
	if activity, _ := render("templates/android/MainActivity.java.tmpl", plain); bytes.Contains(activity, []byte("PermissionRequest")) {
		t.Error("a shell without features handles permission requests")
	}
	if manifest, _ := render("templates/android/AndroidManifest.xml.tmpl", plain); bytes.Contains(manifest, []byte("CAMERA")) || bytes.Contains(manifest, []byte("RECORD_AUDIO")) {
		t.Error("a shell without features declares device permissions")
	}

	sdk, err := findAndroidSDK(false)
	if err != nil {
		t.Skipf("Android SDK/JDK unavailable: %v", err)
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "dev", "gofastr", "capturewebview", "MainActivity.java")
	if err := writeSource(path, activity); err != nil {
		t.Fatal(err)
	}
	sources := []string{path}
	fixture, err := os.ReadFile(filepath.Join("testdata", "androidstubs.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, chunk := range strings.Split(strings.ReplaceAll(string(fixture), "\r\n", "\n"), "\n--- ")[1:] {
		name, body, _ := strings.Cut(chunk, "\n")
		if !strings.HasPrefix(name, "go/") {
			continue
		}
		stub := filepath.Join(directory, filepath.FromSlash(name))
		if err := writeSource(stub, []byte(body)); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, stub)
	}
	java, err := listFiles(filepath.Join(gobindPackageOut(t, pkg), "java"), ".java")
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "11", "-encoding", "UTF-8", "-classpath", sdk.androidJar, "-d", filepath.Join(directory, "classes")}, append(sources, java...)...)
	if output, err := exec.Command(sdk.javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("WebView shell SDK compile: %v\n%s", err, output)
	}
}

func TestNativeAndroidDryRunAndPermissionPackaging(t *testing.T) {
	var stdout, stderr bytes.Buffer
	directory := filepath.Join(t.TempDir(), "unwritten")
	args := []string{"build", "--platform", "android-emu", "--renderer", "native", "--pkg", "../../examples/notes/notesnative", "--url-scheme", "notes-test", "--microphone-usage", "Record private audio.", "--out", directory, "--dry-run"}
	if code := run(context.Background(), args, &stdout, &stderr); code != 0 {
		t.Fatalf("%d %s", code, &stderr)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("dry run wrote output")
	}
	if !strings.Contains(stdout.String(), "GofastrActivity.java") || !strings.Contains(stdout.String(), "Android SQLite") {
		t.Fatal("native Android plan omits renderer or SQLite compatibility")
	}
	a := nativeApp(t).forAndroid(36)
	a.MicrophoneUsage = "Record audio."
	a.URLScheme = "notes-test"
	manifest, err := render("templates/android/AndroidManifest.xml.tmpl", a)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"android.permission.RECORD_AUDIO", `android:scheme="notes-test"`} {
		if !bytes.Contains(manifest, []byte(expected)) {
			t.Errorf("manifest omits %s", expected)
		}
	}
	a.MicrophoneUsage = ""
	a.URLScheme = ""
	manifest, _ = render("templates/android/AndroidManifest.xml.tmpl", a)
	if bytes.Contains(manifest, []byte("RECORD_AUDIO")) || bytes.Contains(manifest, []byte("BROWSABLE")) {
		t.Fatal("optional permission/link registration was not removed")
	}
}

func TestDefaultAndroidAVDPrefersPhoneToTV(t *testing.T) {
	if got := preferredAndroidAVD([]string{"Homi_TV_API34", "Pixel_5_API_34"}); got != "Pixel_5_API_34" {
		t.Fatalf("phone APK would boot incompatible TV image: %s", got)
	}
	if got := preferredAndroidAVD([]string{"custom_avd"}); got != "custom_avd" {
		t.Fatal(got)
	}
}
