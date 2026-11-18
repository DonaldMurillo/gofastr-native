// Package capturenative is a camera capture app built on the capture
// extension. It records video with location and camera direction, streams
// it over WHIP, or both. Under the native renderer capture keeps running in
// the background. The native host calls Start, Stop, Root, Render, Dispatch
// and Command.
package capturenative

import (
	"context"
	"errors"
	"sync"

	"github.com/DonaldMurillo/gofastr-native/extension"
	"github.com/DonaldMurillo/gofastr-native/extension/capture"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

var host struct {
	sync.RWMutex
	app     *nativeui.App
	runtime *nativeui.Runtime
}

// Start builds the app. Recordings live in the extension's own directory,
// so dataDir is unused.
func Start(dataDir string) error {
	host.Lock()
	defer host.Unlock()
	if host.app != nil {
		return errors.New("capturenative: already started")
	}
	app, err := New()
	if err != nil {
		return err
	}
	runtime, err := nativeui.NewRuntime(app, nativeui.RuntimeOptions{})
	if err != nil {
		return err
	}
	host.app, host.runtime = app, runtime
	return nil
}

// Stop releases the screens. A running capture session keeps going: it
// belongs to the foreground service, not to the screens.
func Stop() error {
	host.Lock()
	defer host.Unlock()
	host.app, host.runtime = nil, nil
	return nil
}

// Root is the first route the renderer opens.
func Root() string { return root }

// Render returns one screen as JSON.
func Render(route, values string) (string, error) {
	host.RLock()
	defer host.RUnlock()
	if host.app == nil {
		return "", errors.New("capturenative: not started")
	}
	return host.app.RenderJSON(context.Background(), route, values)
}

// Dispatch runs a screen action and returns its result as JSON.
func Dispatch(route, action, values string) (string, error) {
	host.RLock()
	defer host.RUnlock()
	if host.app == nil {
		return "", errors.New("capturenative: not started")
	}
	return host.app.DispatchJSON(context.Background(), route, action, values)
}

// Command runs the renderer's editor and live-update protocol.
func Command(payload string) (string, error) {
	host.RLock()
	defer host.RUnlock()
	if host.runtime == nil {
		return "", errors.New("capturenative: not started")
	}
	return host.runtime.CommandJSON(context.Background(), payload)
}

// New returns the capture screens for the native renderer, whose capture
// keeps running in the background.
func New() (*nativeui.App, error) {
	return nativeui.NewWithExtensions(root, []extension.Definition{capture.Definition()}, routes(true)...)
}

// NewForeground returns the same screens for a renderer whose capture pauses
// while the app is in the background, such as the WebView renderer.
func NewForeground() (*nativeui.App, error) {
	return nativeui.NewWithExtensions(root, []extension.Definition{capture.Definition()}, routes(false)...)
}
