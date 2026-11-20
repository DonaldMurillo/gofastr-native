// Package refresh schedules periodic background work while the app is closed.
// The OS decides when, and whether, the work runs; the app owns what it does.
package refresh

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	Schedule = "schedule"
	Cancel   = "cancel"
	Status   = "status"

	// MinIntervalMinutes is the shortest interval either platform honors.
	MinIntervalMinutes = 15
	// MaxIntervalMinutes is one week.
	MaxIntervalMinutes = 7 * 24 * 60
	// MaxRunDuration bounds one background run, whatever the OS allows.
	MaxRunDuration = 25 * time.Second
)

// Request schedules, cancels, or inspects the app's single periodic refresh.
// IntervalMinutes is the earliest time between runs, not a guarantee: the OS
// may run the work later, less often, or not at all.
type Request struct {
	Operation       string `json:"operation"`
	IntervalMinutes int    `json:"intervalMinutes,omitempty"`
}

// State reports whether a refresh is scheduled. It does not report when the
// OS will run it.
type State struct {
	Scheduled bool `json:"scheduled"`
}

func (request Request) Validate() error {
	switch request.Operation {
	case Schedule:
		if request.IntervalMinutes < MinIntervalMinutes || request.IntervalMinutes > MaxIntervalMinutes {
			return errors.New("refresh: interval must be between 15 minutes and one week")
		}
		return nil
	case Cancel, Status:
		if request.IntervalMinutes != 0 {
			return errors.New("refresh: only scheduling accepts an interval")
		}
		return nil
	default:
		return errors.New("refresh: unsupported operation")
	}
}

// Accept checks a completed outcome against the requested state change.
func (request Request) Accept(state State) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if request.Operation == Schedule && !state.Scheduled || request.Operation == Cancel && state.Scheduled {
		return errors.New("refresh: result does not match the requested change")
	}
	return nil
}

// Runner runs the app's refresh work when the OS wakes the app. Bind packages
// expose Run and Cancel to the native host:
//
//	var background = refresh.NewRunner(sync)
//	func BackgroundRefresh(timeoutMillis int64) error { return background.Run(timeoutMillis) }
//	func CancelBackgroundRefresh()                    { background.Cancel() }
type Runner struct {
	work   func(context.Context) error
	mu     sync.Mutex
	cancel context.CancelFunc
}

// NewRunner wraps work, which must stop promptly when its context ends.
func NewRunner(work func(context.Context) error) *Runner {
	return &Runner{work: work}
}

// Run performs one refresh within timeoutMillis, capped at MaxRunDuration.
// Only one run happens at a time; an overlapping call fails without running.
func (runner *Runner) Run(timeoutMillis int64) error {
	if runner == nil || runner.work == nil {
		return errors.New("refresh: no refresh work is registered")
	}
	timeout := time.Duration(timeoutMillis) * time.Millisecond
	if timeout <= 0 || timeout > MaxRunDuration {
		timeout = MaxRunDuration
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	runner.mu.Lock()
	if runner.cancel != nil {
		runner.mu.Unlock()
		return errors.New("refresh: a refresh is already running")
	}
	runner.cancel = cancel
	runner.mu.Unlock()
	defer func() {
		runner.mu.Lock()
		runner.cancel = nil
		runner.mu.Unlock()
	}()
	if err := runner.work(ctx); err != nil {
		return err
	}
	return ctx.Err()
}

// Cancel ends the running refresh's context, for example when the OS expires
// the background task. It is safe to call when nothing is running.
func (runner *Runner) Cancel() {
	if runner == nil {
		return
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.cancel != nil {
		runner.cancel()
	}
}
