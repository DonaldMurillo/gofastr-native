// Package extension defines compiled, versioned mobile renderer extensions.
package extension

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr-native/device/files"
)

const ManifestName = "gofastr-native.extension.json"
const MaxPayload = 8192

const MaxComponentPayload = 2 << 20

type Kind string

const (
	FileKind      Kind = "file"
	ComponentKind Kind = "component"
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9.-]{0,79}/v[1-9][0-9]{0,2}$`)
var factoryPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,99}$`)
var bindPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}(/[a-z][a-z0-9_]{0,39}){0,3}$`)
var permissionPattern = regexp.MustCompile(`^android\.permission\.[A-Z][A-Z_]{0,63}$`)

// foregroundServiceTypes are the manifest types a library may declare.
var foregroundServiceTypes = map[string]bool{"camera": true, "location": true, "microphone": true, "dataSync": true, "mediaPlayback": true, "connectedDevice": true}

// FileOperation carries bounded JSON as a string so every renderer can decode it
// without introducing an untyped object tree into the shared wire contract.
type FileOperation struct {
	Name    string `json:"name"`
	Payload string `json:"payload"`
}

// Manifest identifies library-owned adapters compiled into the application.
type Manifest struct {
	Name         string   `json:"name"`
	Kind         Kind     `json:"kind,omitempty"`
	Capabilities []string `json:"capabilities"`
	IOS          IOS      `json:"ios,omitzero"`
	Android      *Android `json:"android,omitempty"`
	WebView      WebView  `json:"webview,omitzero"`
}

// IOS names ordinary Swift sources and their compiled factory type. A zero IOS
// means the library has no UIKit adapter: iOS builds leave it out and the
// renderer reports its capabilities as missing.
type IOS struct {
	Factory string   `json:"factory"`
	Sources []string `json:"sources"`
}

// Android names ordinary Java sources compiled with the host, without Gradle.
//
// Bind lists package-relative Go packages that gomobile binds beside the app
// package, so library Java can call library Go without app-written glue.
// Permissions and Services are merged into the generated AndroidManifest.xml.
type Android struct {
	Factory     string           `json:"factory"`
	Sources     []string         `json:"sources"`
	Bind        []string         `json:"bind,omitempty"`
	Permissions []string         `json:"permissions,omitempty"`
	Services    []AndroidService `json:"services,omitempty"`
}

// AndroidService declares a library-owned android.app.Service in package
// dev.gofastr.nativeui. ForegroundServiceType uses the manifest's
// "camera|location" syntax and may be empty for a background-only service.
type AndroidService struct {
	Class                 string `json:"class"`
	ForegroundServiceType string `json:"foregroundServiceType,omitempty"`
}

// WebView names a bundled adapter, never a remote script URL. A zero WebView
// means the library has no WebView adapter.
//
// Permissions lists the browser features the adapter asks for: "camera" and
// "geolocation". The Android WebView shell declares the matching Android
// permissions and grants them to the app's own pages. Microphone access stays
// the app's choice through --microphone-usage.
type WebView struct {
	Script      string   `json:"script"`
	Permissions []string `json:"permissions,omitempty"`
}

// WebViewPermissions are the feature names WebView.Permissions accepts.
var WebViewPermissions = []string{"camera", "geolocation"}

// Definition binds typed request/result validation to bundled renderer assets.
// Validate returns one of the manifest's capabilities for this request.
type Definition struct {
	Manifest          Manifest
	Assets            fs.ReadFileFS
	Validate          func(FileOperation) (string, error)
	Accept            func(FileOperation, files.Document) error
	ValidateComponent func(Component) error
}

// Decode rejects unknown fields and extra JSON values in a bounded payload.
func Decode(payload string, target any) error {
	return decode(payload, target, MaxPayload)
}

func decode(payload string, target any, maximum int) error {
	if len(payload) == 0 || len(payload) > maximum {
		return errors.New("extension: invalid payload size")
	}
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("extension: trailing payload content")
	}
	return nil
}

func ParseManifest(content []byte) (Manifest, error) {
	var manifest Manifest
	if err := Decode(string(content), &manifest); err != nil {
		return manifest, err
	}
	return manifest, manifest.Validate()
}

// Validate checks names and package-relative asset paths, not OS permissions.
func (manifest Manifest) Validate() error {
	if manifest.Android != nil {
		if err := manifest.Android.validate(); err != nil {
			return err
		}
	}
	hasIOS, hasWebView := manifest.HasIOS(), manifest.HasWebView()
	if !hasIOS && !hasWebView && manifest.Android == nil {
		return errors.New("extension: declare at least one platform adapter")
	}
	if manifest.Kind != "" && manifest.Kind != FileKind && manifest.Kind != ComponentKind {
		return errors.New("extension: unknown adapter kind")
	}
	if manifest.Kind == ComponentKind && (len(manifest.Capabilities) != 1 || manifest.Capabilities[0] != manifest.Name) {
		return errors.New("extension: components must declare their namespaced rendering capability")
	}
	if !namePattern.MatchString(manifest.Name) || len(manifest.Capabilities) == 0 || len(manifest.Capabilities) > 8 ||
		hasIOS && (!factoryPattern.MatchString(manifest.IOS.Factory) || len(manifest.IOS.Sources) == 0 || len(manifest.IOS.Sources) > 16) {
		return errors.New("extension: invalid manifest")
	}
	seen := map[string]bool{}
	for _, capability := range manifest.Capabilities {
		if !namePattern.MatchString(capability) || seen[capability] {
			return errors.New("extension: invalid or duplicate capability")
		}
		seen[capability] = true
	}
	seen = map[string]bool{}
	for _, source := range manifest.IOS.Sources {
		if !strings.HasSuffix(source, ".swift") {
			return errors.New("extension: iOS assets must be Swift sources")
		}
	}
	if hasWebView && !strings.HasSuffix(manifest.WebView.Script, ".js") {
		return errors.New("extension: WebView asset must be JavaScript")
	}
	if len(manifest.WebView.Permissions) > 0 && !hasWebView {
		return errors.New("extension: WebView permissions need a WebView adapter")
	}
	for _, permission := range manifest.WebView.Permissions {
		if !slices.Contains(WebViewPermissions, permission) || seen["webview:"+permission] {
			return fmt.Errorf("extension: unsupported or duplicate WebView permission %q", permission)
		}
		seen["webview:"+permission] = true
	}
	for _, source := range manifest.sharedAssets() {
		if !fs.ValidPath(source) || strings.Contains(source, "\\") || seen[source] ||
			!(strings.HasSuffix(source, ".swift") || source == manifest.WebView.Script && strings.HasSuffix(source, ".js")) {
			return errors.New("extension: invalid or duplicate asset path")
		}
		seen[source] = true
	}
	return nil
}

// HasIOS reports whether the library ships a UIKit adapter.
func (manifest Manifest) HasIOS() bool {
	return manifest.IOS.Factory != "" || len(manifest.IOS.Sources) > 0
}

// HasWebView reports whether the library ships a WebView adapter.
func (manifest Manifest) HasWebView() bool { return manifest.WebView.Script != "" }

// sharedAssets lists the iOS and WebView asset paths the manifest declares.
func (manifest Manifest) sharedAssets() []string {
	assets := append([]string(nil), manifest.IOS.Sources...)
	if manifest.WebView.Script != "" {
		assets = append(assets, manifest.WebView.Script)
	}
	return assets
}

func (adapter *Android) validate() error {
	if !factoryPattern.MatchString(adapter.Factory) || len(adapter.Sources) == 0 || len(adapter.Sources) > 16 ||
		len(adapter.Bind) > 4 || len(adapter.Permissions) > 16 || len(adapter.Services) > 4 {
		return errors.New("extension: invalid Android adapter")
	}
	seen := map[string]bool{}
	for _, source := range adapter.Sources {
		if !fs.ValidPath(source) || strings.Contains(source, "\\") || !strings.HasSuffix(source, ".java") || seen[source] {
			return errors.New("extension: invalid Android source")
		}
		seen[source] = true
	}
	for _, bind := range adapter.Bind {
		if !bindPattern.MatchString(bind) || seen["bind:"+bind] {
			return errors.New("extension: invalid Android bind package")
		}
		seen["bind:"+bind] = true
	}
	for _, permission := range adapter.Permissions {
		if !permissionPattern.MatchString(permission) || seen[permission] {
			return errors.New("extension: invalid Android permission")
		}
		seen[permission] = true
	}
	for _, service := range adapter.Services {
		if !factoryPattern.MatchString(service.Class) || seen["service:"+service.Class] {
			return errors.New("extension: invalid Android service")
		}
		seen["service:"+service.Class] = true
		if service.ForegroundServiceType == "" {
			continue
		}
		for _, kind := range strings.Split(service.ForegroundServiceType, "|") {
			if !foregroundServiceTypes[kind] {
				return fmt.Errorf("extension: unsupported foreground service type %q", kind)
			}
		}
	}
	return nil
}

// Registry is an immutable per-application allowlist, not a global plugin loader.
type Registry struct {
	definitions map[string]Definition
	ordered     []Definition
}

func New(definitions ...Definition) (*Registry, error) {
	if len(definitions) > 16 {
		return nil, errors.New("extension: too many compiled definitions")
	}
	registry := &Registry{definitions: map[string]Definition{}}
	capabilities := map[string]bool{}
	for _, definition := range definitions {
		manifest := definition.Manifest
		if err := manifest.Validate(); err != nil {
			return nil, err
		}
		_, duplicate := registry.definitions[manifest.Name]
		if duplicate || definition.Assets == nil ||
			(manifest.Kind == ComponentKind && (definition.ValidateComponent == nil || definition.Validate != nil || definition.Accept != nil)) ||
			(manifest.Kind != ComponentKind && (definition.Validate == nil || definition.Accept == nil || definition.ValidateComponent != nil)) {
			return nil, errors.New("extension: duplicate or incomplete definition")
		}
		for _, capability := range manifest.Capabilities {
			if capabilities[capability] {
				return nil, errors.New("extension: capability has multiple owners")
			}
			capabilities[capability] = true
		}
		stored, err := definition.Assets.ReadFile(ManifestName)
		if err != nil {
			return nil, err
		}
		encoded, _ := json.Marshal(manifest)
		parsed, err := ParseManifest(stored)
		actual, _ := json.Marshal(parsed)
		if err != nil || !bytes.Equal(encoded, actual) {
			return nil, errors.New("extension: embedded manifest does not match definition")
		}
		assets := make(assetFiles)
		paths := manifest.sharedAssets()
		if manifest.Android != nil {
			paths = append(paths, manifest.Android.Sources...)
		}
		for _, path := range paths {
			content, err := definition.Assets.ReadFile(path)
			if err != nil || len(content) == 0 || len(content) > 512*1024 {
				return nil, fmt.Errorf("extension: missing or oversized asset %q", path)
			}
			assets[path] = bytes.Clone(content)
		}
		assets[ManifestName] = bytes.Clone(stored)
		definition.Manifest.Capabilities = append([]string(nil), manifest.Capabilities...)
		definition.Manifest.IOS.Sources = append([]string(nil), manifest.IOS.Sources...)
		definition.Manifest.WebView.Permissions = append([]string(nil), manifest.WebView.Permissions...)
		if manifest.Android != nil {
			adapter := *manifest.Android
			adapter.Sources = append([]string(nil), adapter.Sources...)
			adapter.Bind = append([]string(nil), adapter.Bind...)
			adapter.Permissions = append([]string(nil), adapter.Permissions...)
			adapter.Services = append([]AndroidService(nil), adapter.Services...)
			definition.Manifest.Android = &adapter
		}
		definition.Assets = assets
		registry.definitions[manifest.Name] = definition
		registry.ordered = append(registry.ordered, definition)
	}
	return registry, nil
}

type assetFiles map[string][]byte

func (assets assetFiles) Open(name string) (fs.File, error) {
	content, err := assets.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return &assetFile{Reader: bytes.NewReader(content), name: name}, nil
}

type assetFile struct {
	*bytes.Reader
	name string
}

func (file *assetFile) Close() error { return nil }
func (file *assetFile) Stat() (fs.FileInfo, error) {
	return assetInfo{path.Base(file.name), file.Size()}, nil
}

type assetInfo struct {
	name string
	size int64
}

func (info assetInfo) Name() string  { return info.name }
func (info assetInfo) Size() int64   { return info.size }
func (assetInfo) Mode() fs.FileMode  { return 0444 }
func (assetInfo) ModTime() time.Time { return time.Time{} }
func (assetInfo) IsDir() bool        { return false }
func (assetInfo) Sys() any           { return nil }
func (assets assetFiles) ReadFile(path string) ([]byte, error) {
	content, exists := assets[path]
	if !exists {
		return nil, fs.ErrNotExist
	}
	return bytes.Clone(content), nil
}

func (registry *Registry) Definitions() []Definition {
	result := append([]Definition(nil), registry.ordered...)
	for index := range result {
		result[index].Manifest.Capabilities = append([]string(nil), result[index].Manifest.Capabilities...)
		result[index].Manifest.IOS.Sources = append([]string(nil), result[index].Manifest.IOS.Sources...)
	}
	return result
}

func (registry *Registry) Allows(capability string) bool {
	for _, definition := range registry.ordered {
		for _, declared := range definition.Manifest.Capabilities {
			if declared == capability {
				return true
			}
		}
	}
	return false
}

func (registry *Registry) Validate(operation FileOperation) (string, error) {
	definition, exists := registry.definitions[operation.Name]
	if !exists || definition.Manifest.Kind == ComponentKind || len(operation.Payload) == 0 || len(operation.Payload) > MaxPayload || !json.Valid([]byte(operation.Payload)) {
		return "", errors.New("extension: unregistered operation or invalid payload")
	}
	capability, err := definition.Validate(operation)
	if err != nil {
		return "", err
	}
	for _, declared := range definition.Manifest.Capabilities {
		if declared == capability {
			return capability, nil
		}
	}
	return "", errors.New("extension: validator returned undeclared capability")
}

func (registry *Registry) Accept(operation FileOperation, document files.Document) error {
	if _, err := registry.Validate(operation); err != nil {
		return err
	}
	if err := document.Validate(); err != nil {
		return err
	}
	return registry.definitions[operation.Name].Accept(operation, document)
}
