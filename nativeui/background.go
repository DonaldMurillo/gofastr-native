package nativeui

import (
	"context"
	"errors"
	"maps"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr-native/internal/uitext"
)

// BackgroundHandler must honor cancellation and tolerate repeated execution.
// Register handlers before sharing the app or creating its runtime.
type BackgroundHandler func(context.Context) error

var ErrBackgroundRetry = errors.New("nativeui: retry background task")

const BackgroundDeadline = 9 * time.Minute

type BackgroundOutcome struct {
	Status string `json:"status"`
}

// WithBackground returns a copy with the named, compiled Go handlers registered.
func (app *App) WithBackground(handlers map[string]BackgroundHandler) (*App, error) {
	for name, handler := range handlers {
		if name == "" || !uitext.Token(name, 128) || handler == nil {
			return nil, errors.New("nativeui: invalid background handler")
		}
	}
	clone := *app
	clone.background = maps.Clone(handlers)
	return &clone, nil
}

type backgroundRuns struct {
	sync.Mutex
	active  map[string]context.CancelFunc
	stopped map[string]bool
}

func (runtime *Runtime) backgroundCommand(ctx context.Context, command Command) *BackgroundOutcome {
	outcome := &BackgroundOutcome{Status: "failed"}
	if command.BackgroundID == "" || !uitext.Token(command.BackgroundID, 128) {
		return outcome
	}
	runs := &runtime.background
	runs.Lock()
	if command.Kind == "background/stop" {
		if cancel := runs.active[command.BackgroundID]; cancel != nil {
			cancel()
		}
		if runs.stopped == nil {
			runs.stopped = make(map[string]bool)
		}
		if len(runs.stopped) >= 128 {
			clear(runs.stopped)
		}
		runs.stopped[command.BackgroundID] = true
		runs.Unlock()
		return outcome
	}
	handler := runtime.app.background[command.BackgroundTask]
	if runs.stopped[command.BackgroundID] {
		delete(runs.stopped, command.BackgroundID)
		runs.Unlock()
		outcome.Status = "retry"
		return outcome
	}
	if handler == nil || runs.active[command.BackgroundID] != nil {
		runs.Unlock()
		return outcome
	}
	ctx, cancel := context.WithTimeout(ctx, BackgroundDeadline)
	if runs.active == nil {
		runs.active = make(map[string]context.CancelFunc)
	}
	runs.active[command.BackgroundID] = cancel
	runs.Unlock()
	defer func() { cancel(); runs.Lock(); delete(runs.active, command.BackgroundID); runs.Unlock() }()
	err := callBackground(ctx, handler)
	switch {
	case ctx.Err() != nil, errors.Is(err, ErrBackgroundRetry):
		outcome.Status = "retry"
	case err == nil:
		outcome.Status = "completed"
	}
	return outcome
}

func callBackground(ctx context.Context, handler BackgroundHandler) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("nativeui: background handler panicked")
		}
	}()
	return handler(ctx)
}
