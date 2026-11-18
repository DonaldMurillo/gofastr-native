// Package dashcam is a dash cam app built on the capture extension for the
// native renderer. It records in short files, deletes the oldest unlocked
// ones to stay within a loop size, and locks the footage around an impact
// or a press of Lock. Recording continues with the screen off. The native
// host calls Start, Stop, Root, Render, Dispatch and Command.
package dashcam

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

// Start builds the app. Settings persist in dataDir; recordings live in the
// capture extension's own directory.
func Start(dataDir string) error {
	host.Lock()
	defer host.Unlock()
	if host.app != nil {
		return errors.New("dashcam: already started")
	}
	app, err := New(dataDir)
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

// Stop releases the screens. A recording keeps going: it belongs to the
// capture service, not to the screens.
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
		return "", errors.New("dashcam: not started")
	}
	return host.app.RenderJSON(context.Background(), route, values)
}

// Dispatch runs a screen action and returns its result as JSON.
func Dispatch(route, action, values string) (string, error) {
	host.RLock()
	defer host.RUnlock()
	if host.app == nil {
		return "", errors.New("dashcam: not started")
	}
	return host.app.DispatchJSON(context.Background(), route, action, values)
}

// Command runs the renderer's editor and live-update protocol.
func Command(payload string) (string, error) {
	host.RLock()
	defer host.RUnlock()
	if host.runtime == nil {
		return "", errors.New("dashcam: not started")
	}
	return host.runtime.CommandJSON(context.Background(), payload)
}

// New returns the dash cam screens, with settings saved in dataDir. An
// empty dataDir keeps them in memory.
func New(dataDir string) (*nativeui.App, error) {
	a := &app{settings: openStore(dataDir)}
	return nativeui.NewWithExtensions(root, []extension.Definition{capture.Definition()}, a.routes()...)
}
