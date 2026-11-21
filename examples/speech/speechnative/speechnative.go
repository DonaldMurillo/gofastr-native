// Package speechnative shows foreground speech services without saving text or audio.
package speechnative

import (
	"context"
	"errors"
	"sync"

	"github.com/DonaldMurillo/gofastr-native/device/speech"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

var state struct {
	sync.RWMutex
	runtime *nativeui.Runtime
	app     *nativeui.App
}

func Root() string { return "/speech" }

func Start(_ string) error {
	state.Lock()
	defer state.Unlock()
	if state.runtime != nil {
		return errors.New("speechnative: already started")
	}
	app, err := newApp()
	if err != nil {
		return err
	}
	runtime, err := nativeui.NewRuntime(app, nativeui.RuntimeOptions{})
	if err != nil {
		return err
	}
	state.app, state.runtime = app, runtime
	return nil
}

func Stop() error {
	state.Lock()
	defer state.Unlock()
	state.app, state.runtime = nil, nil
	return nil
}

func Render(route, values string) (string, error) {
	state.RLock()
	defer state.RUnlock()
	if state.app == nil {
		return "", errors.New("speechnative: not started")
	}
	return state.app.RenderJSON(context.Background(), route, values)
}

func Dispatch(route, action, values string) (string, error) {
	state.RLock()
	defer state.RUnlock()
	if state.app == nil {
		return "", errors.New("speechnative: not started")
	}
	return state.app.DispatchJSON(context.Background(), route, action, values)
}

func Command(payload string) (string, error) {
	state.RLock()
	defer state.RUnlock()
	if state.runtime == nil {
		return "", errors.New("speechnative: not started")
	}
	return state.runtime.CommandJSON(context.Background(), payload)
}

func newApp() (*nativeui.App, error) {
	service := func(request speech.Request) nativeui.Result {
		return nativeui.Result{Service: &nativeui.ServiceRequest{ID: "speech", Speech: &request}}
	}
	return nativeui.New(Root(), nativeui.Route{
		Path: Root(),
		Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
			return nativeui.Screen{
				Title:  "Speech demo",
				Prompt: "Speak the text below, or use the microphone to turn one utterance into text. Your Android speech provider may process input online.",
				Form: &nativeui.Form{Fields: []nativeui.Field{
					{Key: "text", Label: "Text to speak", Kind: "multiline", Value: "Hello from the Go Fastr speech demo."},
				}},
				Actions: []nativeui.Action{
					{ID: "speak", Label: "Speak", Primary: true},
					{ID: "recognize", Label: "Dictate", Leading: true},
				},
			}, nil
		},
		Actions: map[string]nativeui.Handle{
			"speak": func(_ context.Context, request nativeui.Request) (nativeui.Result, error) {
				spoken := speech.Request{Operation: speech.Speak, Text: request.Values["text"]}
				if err := spoken.Validate(); err != nil {
					return nativeui.Result{Announcement: "Enter nonblank text of at most 4000 bytes."}, nil
				}
				return service(spoken), nil
			},
			"recognize": func(context.Context, nativeui.Request) (nativeui.Result, error) {
				return service(speech.Request{Operation: speech.Recognize}), nil
			},
		},
		Services: map[string]nativeui.ServiceRespond{
			"speech": func(_ context.Context, _ nativeui.Request, result nativeui.ServiceResult) (nativeui.Result, error) {
				if result.Transcript != "" {
					return nativeui.Result{Announcement: "Transcript: " + result.Transcript}, nil
				}
				if result.Message != "" {
					return nativeui.Result{Announcement: result.Message}, nil
				}
				return nativeui.Result{Announcement: "Speech " + string(result.Status) + "."}, nil
			},
		},
	})
}
