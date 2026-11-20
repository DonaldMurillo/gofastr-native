package nativeui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-native/device/background"
)

func TestBackgroundDispatch(t *testing.T) {
	app, err := New("/", Route{Path: "/", Screen: func(context.Context, Request) (Screen, error) { return Screen{Title: "Test"}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	for _, handlers := range []map[string]BackgroundHandler{{"": func(context.Context) error { return nil }}, {"bad name": func(context.Context) error { return nil }}, {"nil": nil}} {
		if _, err := app.WithBackground(handlers); err == nil {
			t.Fatal("invalid registration")
		}
	}
	handlers := map[string]BackgroundHandler{
		"ok": func(ctx context.Context) error {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > BackgroundDeadline {
				t.Error("missing bounded deadline")
			}
			return nil
		},
		"retry":  func(context.Context) error { return ErrBackgroundRetry },
		"failed": func(context.Context) error { return errors.New("failed") },
		"panic":  func(context.Context) error { panic("failed") },
		"wait":   func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() },
	}
	app, err = app.WithBackground(handlers)
	if err != nil {
		t.Fatal(err)
	}
	delete(handlers, "ok")
	r, err := NewRuntime(app, RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for task, want := range map[string]string{"ok": "completed", "retry": "retry", "failed": "failed", "panic": "failed", "missing": "failed", "": "failed"} {
		got := r.Handle(context.Background(), Command{Version: 1, Kind: "background/run", BackgroundID: "job", BackgroundTask: task})
		if got.Background == nil || got.Background.Status != want {
			t.Fatalf("%s: %+v", task, got)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if got := r.Handle(ctx, Command{Version: 1, Kind: "background/run", BackgroundID: "job", BackgroundTask: "wait"}); got.Background.Status != "retry" {
		t.Fatal(got)
	}
	if got := r.Handle(ctx, Command{Version: 1, Kind: "background/run", BackgroundTask: "ok"}); got.Background.Status != "failed" {
		t.Fatal(got)
	}
	r.Handle(context.Background(), Command{Version: 1, Kind: "background/stop", BackgroundID: "stopped"})
	if got := r.Handle(context.Background(), Command{Version: 1, Kind: "background/run", BackgroundID: "stopped", BackgroundTask: "ok"}); got.Background.Status != "retry" {
		t.Fatal(got)
	}
}

func TestBackgroundServiceWiring(t *testing.T) {
	request := ServiceRequest{ID: "job", Background: &background.Request{Operation: background.Schedule, Identifier: "job", Task: "ok"}}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	if request.capability() != CapabilityBackground {
		t.Fatal("wrong capability")
	}
	for _, result := range []ServiceResult{{Status: EffectCompleted}, {Status: EffectCompleted, Background: &background.State{}}, {Status: EffectFailed, Background: &background.State{Scheduled: true}}} {
		if request.accept(result) == nil {
			t.Fatal("accepted invalid result", result)
		}
	}
	if err := request.accept(ServiceResult{Status: EffectCompleted, Background: &background.State{Scheduled: true}}); err != nil {
		t.Fatal(err)
	}
	if (ServiceRequest{}).accept(ServiceResult{Status: EffectCompleted, Background: &background.State{}}) == nil {
		t.Fatal("unrelated result")
	}
	copy := cloneService(&request)
	copy.Background.Task = "changed"
	if request.Background.Task != "ok" {
		t.Fatal("not cloned")
	}
	app, _ := New("/", Route{Path: "/", Screen: func(context.Context, Request) (Screen, error) { return Screen{Title: "Test"}, nil }, Services: map[string]ServiceRespond{"job": func(context.Context, Request, ServiceResult) (Result, error) { return Result{}, nil }}})
	route := app.routes[0]
	if _, err := app.result(context.Background(), route, Result{Service: &request}); err == nil {
		t.Fatal("unregistered task accepted")
	}
	app, _ = app.WithBackground(map[string]BackgroundHandler{"ok": func(context.Context) error { return nil }})
	if _, err := app.result(context.Background(), route, Result{Service: &request}); err != nil {
		t.Fatal(err)
	}
}

func TestBackgroundStopAndDuplicateRun(t *testing.T) {
	entered := make(chan struct{})
	app, _ := New("/", Route{Path: "/", Screen: func(context.Context, Request) (Screen, error) { return Screen{Title: "Test"}, nil }})
	app, _ = app.WithBackground(map[string]BackgroundHandler{"wait": func(ctx context.Context) error { close(entered); <-ctx.Done(); return ctx.Err() }})
	r, _ := NewRuntime(app, RuntimeOptions{})
	command := Command{Version: 1, Kind: "background/run", BackgroundID: "job", BackgroundTask: "wait"}
	done := make(chan Reply, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() { done <- r.Handle(ctx, command) }()
	<-entered
	if got := r.Handle(context.Background(), command); got.Background.Status != "failed" {
		t.Fatal("duplicate admitted", got)
	}
	r.Handle(context.Background(), Command{Version: 1, Kind: "background/stop", BackgroundID: "job"})
	if got := <-done; got.Background.Status != "retry" {
		t.Fatal(got)
	}
}
