package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr-native/extension"
	renderandroid "github.com/DonaldMurillo/gofastr-native/render/android"
	renderarcore "github.com/DonaldMurillo/gofastr-native/render/android/arcore"
	renderfcm "github.com/DonaldMurillo/gofastr-native/render/android/fcm"
)

func prepareAndroidHost(r *runner, a app, dir string) ([]string, error) {
	activity := filepath.Join(dir, "src", filepath.FromSlash(strings.ReplaceAll(a.ID, ".", "/")), "MainActivity.java")
	if err := r.do("generate Android binding bootstrap", func() error { return writeRendered("templates/android/MainActivity.java.tmpl", activity, a) }); err != nil {
		return nil, err
	}
	sources := []string{activity}
	if a.Renderer != "native" {
		return sources, nil
	}
	if a.BackgroundRefresh {
		service := filepath.Join(filepath.Dir(activity), "BackgroundService.java")
		if err := r.do("generate background refresh job binding", func() error { return writeRendered("templates/android/BackgroundService.java.tmpl", service, a) }); err != nil {
			return nil, err
		}
		sources = append(sources, service)
	}
	entries, err := renderandroid.Sources.ReadDir(".")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		dst := filepath.Join(dir, "renderer", entry.Name())
		if err := r.do("copy native Android renderer "+entry.Name(), func() error {
			content, err := renderandroid.Sources.ReadFile(entry.Name())
			if err != nil {
				return err
			}
			return writeSource(dst, content)
		}); err != nil {
			return nil, err
		}
		sources = append(sources, dst)
	}
	var factories []string
	if a.ARCoreAAR != "" {
		entries, err := renderarcore.Sources.ReadDir(".")
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			dst := filepath.Join(dir, "renderer", "arcore", entry.Name())
			if err := r.do("copy Android ARCore provider "+entry.Name(), func() error {
				content, err := renderarcore.Sources.ReadFile(entry.Name())
				if err != nil {
					return err
				}
				return writeSource(dst, content)
			}); err != nil {
				return nil, err
			}
			sources = append(sources, dst)
		}
		factories = append(factories, "        dev.gofastr.nativeui.arcore.ARCore.install();\n")
	}
	if a.PushConfig != "" {
		entries, err := renderfcm.Sources.ReadDir(".")
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			dst := filepath.Join(dir, "renderer", "fcm", entry.Name())
			if err := r.do("copy Android push provider "+entry.Name(), func() error {
				content, err := renderfcm.Sources.ReadFile(entry.Name())
				if err != nil {
					return err
				}
				return writeSource(dst, content)
			}); err != nil {
				return nil, err
			}
			sources = append(sources, dst)
		}
		factories = append(factories, "        dev.gofastr.nativeui.fcm.FcmPush.install();\n")
	}
	seen := map[string]bool{}
	for index, definition := range a.Extensions {
		adapter := definition.manifest.Android
		if adapter == nil {
			return nil, fmt.Errorf("extension %s has no native Android adapter", definition.manifest.Name)
		}
		if seen[adapter.Factory] {
			return nil, fmt.Errorf("duplicate Android extension factory %s", adapter.Factory)
		}
		seen[adapter.Factory] = true
		factories = append(factories, "        Extensions.install("+strconv.Quote(definition.manifest.Name)+", new dev.gofastr.nativeui."+adapter.Factory+"());\n")
		for _, source := range adapter.Sources {
			dst := filepath.Join(dir, fmt.Sprintf("extension-%d", index), filepath.Base(source))
			if err := r.do("copy Android extension "+source, func() error {
				content, err := extensionAsset(definition.directory, source, 512*1024)
				if err != nil {
					return err
				}
				return writeSource(dst, content)
			}); err != nil {
				return nil, err
			}
			sources = append(sources, dst)
		}
	}
	catalog := filepath.Join(dir, "src", filepath.FromSlash(strings.ReplaceAll(a.ID, ".", "/")), "CompiledExtensions.java")
	if err := r.do("generate Android extension catalog", func() error {
		return writeSource(catalog, []byte("package "+a.ID+";\nimport dev.gofastr.nativeui.Extensions;\nfinal class CompiledExtensions { static void install() {\n"+strings.Join(factories, "")+"} }\n"))
	}); err != nil {
		return nil, err
	}
	return append(sources, catalog), nil
}

// androidBindPackages is the app package followed by every Go package a
// compiled extension asks gomobile to bind beside it.
func (a app) androidBindPackages() []string {
	packages := []string{a.PkgPath}
	for _, definition := range a.Extensions {
		if definition.manifest.Android == nil {
			continue
		}
		for _, bind := range definition.manifest.Android.Bind {
			packages = append(packages, definition.importPath+"/"+bind)
		}
	}
	return packages
}

// AndroidExtensionPermissions lists extension permissions the manifest
// template does not already declare, in a stable order.
func (a app) AndroidExtensionPermissions() []string {
	seen := map[string]bool{"android.permission.ACCESS_COARSE_LOCATION": a.Location,
		"android.permission.ACCESS_FINE_LOCATION": a.Location,
		"android.permission.INTERNET":             true, "android.permission.POST_NOTIFICATIONS": true,
		"android.permission.RECEIVE_BOOT_COMPLETED": true, "android.permission.RECORD_AUDIO": a.MicrophoneUsage != "",
		"android.permission.USE_BIOMETRIC": a.SecureStorage && a.BiometricUsage != "", "android.permission.ACCESS_NETWORK_STATE": a.NetworkStatus,
		"android.permission.CAMERA": a.ARCoreAAR != ""}
	var permissions []string
	for _, definition := range a.Extensions {
		if definition.manifest.Android == nil {
			continue
		}
		for _, permission := range definition.manifest.Android.Permissions {
			if !seen[permission] {
				seen[permission] = true
				permissions = append(permissions, permission)
			}
		}
	}
	sort.Strings(permissions)
	return permissions
}

// discoverWebViewFeatures collects the browser features the WebView
// adapters in the app's dependency graph declare.
func discoverWebViewFeatures(r *runner, packagePath string) ([]string, error) {
	definitions, err := discoverPlatformExtensions(r, packagePath, "android")
	if err != nil {
		return nil, err
	}
	var features []string
	for _, definition := range definitions {
		if !definition.manifest.HasWebView() {
			continue
		}
		for _, feature := range definition.manifest.WebView.Permissions {
			if !slices.Contains(features, feature) {
				features = append(features, feature)
			}
		}
	}
	sort.Strings(features)
	return features, nil
}

// WebViewCamera, WebViewGeolocation and WebViewMicrophone report what the
// WebView shell may grant to the app's pages.
func (a app) WebViewCamera() bool      { return slices.Contains(a.WebViewFeatures, "camera") }
func (a app) WebViewGeolocation() bool { return slices.Contains(a.WebViewFeatures, "geolocation") }
func (a app) WebViewMicrophone() bool  { return a.MicrophoneUsage != "" }

// AndroidWebViewPermissions lists the Android permissions behind the
// WebView features, in a stable order.
func (a app) AndroidWebViewPermissions() []string {
	var permissions []string
	if a.WebViewGeolocation() {
		permissions = append(permissions, "android.permission.ACCESS_COARSE_LOCATION", "android.permission.ACCESS_FINE_LOCATION")
	}
	if a.WebViewCamera() {
		permissions = append(permissions, "android.permission.CAMERA")
	}
	if a.WebViewMicrophone() {
		permissions = append(permissions, "android.permission.RECORD_AUDIO")
	}
	return permissions
}

// AndroidExtensionServices lists library-owned services for the manifest.
func (a app) AndroidExtensionServices() []extension.AndroidService {
	var services []extension.AndroidService
	for _, definition := range a.Extensions {
		if definition.manifest.Android != nil {
			services = append(services, definition.manifest.Android.Services...)
		}
	}
	return services
}

func writeSource(path string, content []byte) error {
	// Reuse the template writer's directory and permission policy.
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return writeOwnerOnly(path, content)
}

func prepareAndroidFeatureResources(r *runner, sdk androidSDK, a app, dir string) ([]string, error) {
	if !a.Widgets {
		return nil, nil
	}
	resource := filepath.Join(dir, "res", "xml", "gofastr_widget.xml")
	layout := filepath.Join(dir, "res", "layout", "gofastr_widget.xml")
	if err := r.do("generate widget provider resources", func() error {
		content, err := renderandroid.Resources.ReadFile("resources/layout/gofastr_widget.xml")
		if err != nil {
			return err
		}
		if err := writeSource(layout, content); err != nil {
			return err
		}
		return writeRendered("templates/android/widget.xml.tmpl", resource, a)
	}); err != nil {
		return nil, err
	}
	compiled := filepath.Join(dir, "widget-resources.zip")
	if err := r.run("compile widget provider resources", sdk.tool("aapt2"), "compile", "-o", compiled, resource, layout); err != nil {
		return nil, err
	}
	return []string{"-R", compiled}, nil
}
