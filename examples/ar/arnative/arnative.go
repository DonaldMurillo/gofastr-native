// Package arnative is the Go binding for the local AR example.
package arnative

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/DonaldMurillo/gofastr-native/device/ar"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

var lifecycle sync.RWMutex
var app *nativeui.App
var runtime *nativeui.Runtime

func Start(_ string) error {
	lifecycle.Lock()
	defer lifecycle.Unlock()
	if runtime != nil {
		return errors.New("AR example is already running")
	}
	a, err := nativeui.New(Root(), arRoute())
	if err != nil {
		return err
	}
	r, err := nativeui.NewRuntime(a, nativeui.RuntimeOptions{})
	if err == nil {
		app, runtime = a, r
	}
	return err
}

func Stop() error {
	lifecycle.Lock()
	defer lifecycle.Unlock()
	app, runtime = nil, nil
	return nil
}

func Root() string { return "/ar" }

func Command(payload string) (string, error) {
	lifecycle.RLock()
	defer lifecycle.RUnlock()
	if runtime == nil {
		return "", errors.New("AR example is not running")
	}
	return runtime.CommandJSON(context.Background(), payload)
}

func Render(route, values string) (string, error) {
	lifecycle.RLock()
	defer lifecycle.RUnlock()
	if app == nil {
		return "", errors.New("AR example is not running")
	}
	return app.RenderJSON(context.Background(), route, values)
}

func arRoute() nativeui.Route {
	action := func(op ar.Operation) nativeui.Handle {
		return func(context.Context, nativeui.Request) (nativeui.Result, error) {
			return nativeui.Result{Service: &nativeui.ServiceRequest{ID: string(op), AR: &ar.Request{Operation: op}}}, nil
		}
	}
	respond := func(_ context.Context, _ nativeui.Request, result nativeui.ServiceResult) (nativeui.Result, error) {
		message := result.Message
		switch {
		case result.AR != nil && result.AR.Availability != "":
			message = "AR availability: " + string(result.AR.Availability) + "."
		case result.AR != nil:
			p := result.AR.Position
			message = fmt.Sprintf("Anchor: x %.2f, y %.2f, z %.2f m (session-local).", p[0], p[1], p[2])
		case message == "":
			message = "AR: " + string(result.Status) + "."
		}
		return nativeui.Result{Announcement: message}, nil
	}
	return nativeui.Route{
		Path: Root(),
		Screen: func(ctx context.Context, _ nativeui.Request) (nativeui.Screen, error) {
			if contract, ok := nativeui.RendererContract(ctx); !ok || contract.Capabilities[nativeui.CapabilityAR] != nativeui.SupportAvailable {
				return nativeui.Screen{
					Title: "Augmented reality",
					Text:  "Native AR is unavailable in this build. Enable ARCore in a native Android build to check support and place anchors.",
				}, nil
			}
			return nativeui.Screen{
				Title:    "Augmented reality",
				Text:     "Check availability without opening the camera, or place one anchor on a real surface. Camera frames are not saved or sent to Go. ARCore is provided by Google. Its use is subject to the Google Terms of Service and Google Privacy Policy.",
				Requires: []nativeui.Requirement{{Capability: nativeui.CapabilityAR}},
				Actions: []nativeui.Action{
					{ID: "check", Label: "Check availability"},
					{ID: "place", Label: "Place an anchor", Primary: true},
				},
			}, nil
		},
		Actions:  map[string]nativeui.Handle{"check": action(ar.Check), "place": action(ar.Place)},
		Services: map[string]nativeui.ServiceRespond{"check": respond, "place": respond},
	}
}
