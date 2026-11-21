package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"slices"
	"strings"
	"testing"
)

func TestURLSchemePackagingIsOptIn(t *testing.T) {
	application := nativeApp(t)
	for _, scheme := range []string{"", "notes-demo", "dev.gofastr.notes"} {
		application.URLScheme = scheme
		plist, err := render("templates/ios/Info.plist.tmpl", application)
		if err != nil {
			t.Fatal(err)
		}
		wellFormed(t, "deep-link Info.plist", plist)
		swift, err := render("templates/ios/App.swift.tmpl", application)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(swift, []byte("applicationDidEnterBackground")) || !bytes.Contains(swift, []byte("renderer.checkpoint()")) {
			t.Fatal("native lifecycle does not checkpoint Go-backed editor state")
		}
		if bytes.Contains(plist, []byte("CFBundleURLTypes")) != (scheme != "") || bytes.Contains(swift, []byte("renderer.openURL(url)")) != (scheme != "") {
			t.Fatalf("deep-link registration is not opt-in for %q", scheme)
		}
		if scheme != "" {
			for _, expected := range []string{"options?[.url]", "guard url.scheme == \"" + scheme + "\"", "renderer.openURL(url)"} {
				if !bytes.Contains(swift, []byte(expected)) {
					t.Fatalf("deep-link bootstrap lacks %s", expected)
				}
			}
			if !bytes.Contains(plist, []byte("<string>"+scheme+"</string>")) || bytes.Count(swift, []byte("\n")) > 75 {
				t.Fatal("deep-link packaging lost its scheme or introduced screen behavior into bootstrap")
			}
		}
	}
}

func TestAndroidAppLinkPackaging(t *testing.T) {
	application := nativeApp(t).forAndroid(36)
	application.URLScheme = "notes-demo"
	hosts := []string{"links.example.org", "xn--bcher-kva.example"}
	for _, enabled := range []bool{false, true} {
		if enabled {
			application.AppLinkHosts = hosts
		}
		manifest, err := render("templates/android/AndroidManifest.xml.tmpl", application)
		if err != nil {
			t.Fatal(err)
		}
		var parsed struct {
			Filters []struct {
				Verify bool `xml:"autoVerify,attr"`
				Action struct {
					Name string `xml:"name,attr"`
				} `xml:"action"`
				Categories []struct {
					Name string `xml:"name,attr"`
				} `xml:"category"`
				Data []struct {
					Host   string `xml:"host,attr"`
					Scheme string `xml:"scheme,attr"`
				} `xml:"data"`
			} `xml:"application>activity>intent-filter"`
		}
		if err := xml.Unmarshal(manifest, &parsed); err != nil {
			t.Fatal(err)
		}
		var registered []string
		for _, filter := range parsed.Filters {
			if !filter.Verify {
				continue
			}
			if filter.Action.Name != "android.intent.action.VIEW" || len(filter.Categories) != 2 ||
				filter.Categories[0].Name != "android.intent.category.DEFAULT" || filter.Categories[1].Name != "android.intent.category.BROWSABLE" {
				t.Fatalf("wrong action or categories: %+v", filter)
			}
			if len(filter.Data) != 3 || filter.Data[0].Scheme != "https" || filter.Data[1].Scheme != "http" || filter.Data[2].Host == "" {
				t.Fatalf("wrong App Link data: %+v", filter.Data)
			}
			registered = append(registered, filter.Data[2].Host)
		}
		if !slices.Equal(registered, application.AppLinkHosts) || !bytes.Contains(manifest, []byte(`android:scheme="notes-demo"`)) {
			t.Fatalf("expected hosts %v and custom scheme; got hosts %v", application.AppLinkHosts, registered)
		}
		java, err := render("templates/android/MainActivity.java.tmpl", application)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(java, []byte(`Arrays.asList("links.example.org", "xn--bcher-kva.example")`)) != enabled {
			t.Fatalf("host allowlist did not match the manifest: %s", java)
		}
	}
}

func TestAppLinkHostValidation(t *testing.T) {
	want := []string{"links.example.org", "xn--bcher-kva.example"}
	hosts, err := parseAppLinkHosts("links.example.org, xn--bcher-kva.example,links.example.org")
	if err != nil || !slices.Equal(hosts, want) {
		t.Fatalf("hosts = %v, %v; want %v", hosts, err, want)
	}
	for _, invalid := range []string{"HTTPS://example.org", "EXAMPLE.org", "*.example.org", "example.org.", "example.org:443",
		"example.org/path", "example..org", "bad-.org", "user@example.org", `"injected.org`, "localhost", "127.0.0.1", "example.1org",
		"example.org,", strings.Repeat("a", 64) + ".org"} {
		if _, err := parseAppLinkHosts(invalid); err == nil {
			t.Errorf("accepted unsafe or invalid host %q", invalid)
		}
	}
	for _, flags := range [][]string{
		{"--platform", "ios-sim", "--renderer", "native", "--app-link-hosts", "example.org"},
		{"--platform", "android", "--renderer", "webview", "--app-link-hosts", "example.org"},
		{"--platform", "android", "--renderer", "native", "--app-link-hosts", "*.example.org"},
	} {
		var stdout, stderr bytes.Buffer
		args := append([]string{"build", "--pkg", "./unused", "--dry-run"}, flags...)
		if code := run(context.Background(), args, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "--app-link-hosts") || stdout.Len() != 0 {
			t.Fatalf("invalid hosts reached build: %v: %d %s %s", flags, code, &stdout, &stderr)
		}
	}
}

func TestURLSchemeCLIValidation(t *testing.T) {
	for _, scheme := range []string{"https", "file", "javascript", "gofastr", "BadScheme", "notes://", "notes_demo", "a\x00b", strings.Repeat("n", 65)} {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"build", "--platform", "ios-sim", "--renderer", "native", "--pkg", "./examples/notes/notesnative", "--url-scheme", scheme, "--dry-run"}, &stdout, &stderr)
		if code != 2 || !strings.Contains(stderr.String(), "--url-scheme") || stdout.Len() != 0 {
			t.Fatalf("unsafe scheme reached build: %q: %d %s %s", scheme, code, &stdout, &stderr)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"build", "--platform", "ios-sim", "--pkg", "./examples/notes/notesmobile", "--url-scheme", "notes-demo", "--dry-run"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "--renderer native") {
		t.Fatalf("unsupported host silently accepted deep links: %d %s", code, &stderr)
	}
}

func TestInspectRecognizesOpenURLCapability(t *testing.T) {
	if !slices.Contains(inspectCapabilities, "open-url/v1") {
		t.Fatal("inspect drops open-url capability")
	}
	for _, profile := range inspectProfiles() {
		want := ""
		if profile.Name == "android" {
			want = "available"
		}
		if got := profile.Caps["open-url/v1"]; got != want {
			t.Fatalf("%s profile open-url/v1 = %q, want %q", profile.Name, got, want)
		}
	}
}
