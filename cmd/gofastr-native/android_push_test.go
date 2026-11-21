package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestPushLibrariesArePinnedOnce(t *testing.T) {
	hash := regexp.MustCompile(`^[0-9a-f]{64}$`)
	seen := map[string]bool{}
	for _, library := range pushLibraries {
		if !hash.MatchString(library.sha256) || seen[library.group+":"+library.name] {
			t.Fatalf("bad or repeated pin %+v", library)
		}
		seen[library.group+":"+library.name] = true
		if !strings.HasPrefix(library.url(), "https://") || (library.ext != "aar" && library.ext != "jar") {
			t.Fatalf("bad artifact %+v", library)
		}
	}
	for name := range seen {
		if base, ok := strings.CutSuffix(name, "-jvm"); ok && seen[base+"-android"] {
			t.Fatalf("%s duplicates the Android variant", name)
		}
	}
	if !seen["com.google.firebase:firebase-messaging"] {
		t.Fatal("firebase-messaging is not pinned")
	}
}

func TestLibraryManifestsMergeComponentsAndPermissions(t *testing.T) {
	common := `<manifest xmlns:android="http://schemas.android.com/apk/res/android" xmlns:tools="http://schemas.android.com/tools" package="com.example.common">
  <uses-permission android:name="android.permission.INTERNET" />
  <permission android:name="${applicationId}.DYNAMIC" android:protectionLevel="signature" />
  <application android:appComponentFactory="x">
    <provider android:name="com.example.InitProvider" android:authorities="${applicationId}.init" android:exported="false" tools:targetApi="n" />
    <service android:name="com.example.Discovery" android:exported="false">
      <meta-data android:name="registrar:a" android:value="Registrar" />
    </service>
  </application>
</manifest>`
	messaging := `<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.example.messaging">
  <uses-permission android:name="android.permission.WAKE_LOCK" />
  <uses-permission android:name="android.permission.WAKE_LOCK" />
  <application>
    <service android:name="com.example.Discovery" android:exported="false">
      <meta-data android:name="registrar:a" android:value="Registrar" />
      <meta-data android:name="registrar:b" android:value="Registrar &amp; more" />
    </service>
  </application>
</manifest>`
	permissions, components, err := mergeLibraryManifests([][]byte{[]byte(common), []byte(messaging)}, "dev.example.app", map[string]bool{"android.permission.INTERNET": true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(permissions, "INTERNET") || strings.Count(permissions, "WAKE_LOCK") != 1 || !strings.Contains(permissions, `<permission android:name="dev.example.app.DYNAMIC" android:protectionLevel="signature" />`) {
		t.Fatalf("permissions:\n%s", permissions)
	}
	if strings.Count(components, "com.example.Discovery") != 1 || strings.Count(components, "registrar:") != 2 || !strings.Contains(components, "Registrar &amp; more") {
		t.Fatalf("components:\n%s", components)
	}
	if !strings.Contains(components, `android:authorities="dev.example.app.init"`) || strings.Contains(components, "targetApi") || strings.Contains(components, "appComponentFactory") {
		t.Fatalf("components:\n%s", components)
	}
	if _, _, err := mergeLibraryManifests([][]byte{[]byte("<resources/>")}, "dev.example.app", map[string]bool{}); err == nil {
		t.Fatal("accepted a non-manifest")
	}
}

func TestGoogleServicesSelectsTheApplicationClient(t *testing.T) {
	file := filepath.Join(t.TempDir(), "google-services.json")
	config := `{"project_info":{"project_number":"1234","project_id":"demo","storage_bucket":"demo.appspot.com"},
 "client":[{"client_info":{"mobilesdk_app_id":"1:1234:android:other","android_client_info":{"package_name":"dev.example.other"}},"api_key":[{"current_key":"key-other"}]},
 {"client_info":{"mobilesdk_app_id":"1:1234:android:app","android_client_info":{"package_name":"dev.example.app"}},"api_key":[{"current_key":"key<&>"}]}]}`
	if err := os.WriteFile(file, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	values, err := readGoogleServices(file, "dev.example.app")
	if err != nil {
		t.Fatal(err)
	}
	if values.AppID != "1:1234:android:app" || values.SenderID != "1234" || values.APIKey != "key<&>" {
		t.Fatalf("%+v", values)
	}
	if resource := string(values.resource()); !strings.Contains(resource, `<string name="google_api_key" translatable="false">key&lt;&amp;&gt;</string>`) || !strings.Contains(resource, `name="gcm_defaultSenderId"`) {
		t.Fatal(resource)
	}
	if _, err := readGoogleServices(file, "dev.example.missing"); err == nil || !strings.Contains(err.Error(), "no Android client") {
		t.Fatalf("missing client: %v", err)
	}
}

func TestPushFlagsAreAndroidNativeAndPaired(t *testing.T) {
	a := app{Renderer: "native", PushConfig: "/config/google-services.json", PushLibraries: "/libs"}
	if err := validateDeviceFeatures(a, "android-emu"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		a        app
		platform string
	}{
		{app{Renderer: "native", PushConfig: "/config.json"}, "android-emu"},
		{app{Renderer: "webview", PushConfig: "/config.json", PushLibraries: "/libs"}, "android-emu"},
		{a, "ios-sim"},
	} {
		if validateDeviceFeatures(c.a, c.platform) == nil {
			t.Fatalf("accepted %+v on %s", c.a, c.platform)
		}
	}
}

func TestDexOutputsIncludesEverySecondaryDex(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"classes.dex", "classes2.dex", "classes3.dex", "classes5.dex"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := dexOutputs(filepath.Join(dir, "classes.dex"))
	if err != nil || len(files) != 3 || filepath.Base(files[2]) != "classes3.dex" {
		t.Fatalf("%v %v", files, err)
	}
}
