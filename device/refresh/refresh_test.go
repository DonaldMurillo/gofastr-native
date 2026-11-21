package refresh

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRequestValidation(t *testing.T) {
	for _, request := range []Request{{Operation: Schedule, IntervalMinutes: 15}, {Operation: Schedule, IntervalMinutes: MaxIntervalMinutes}, {Operation: Cancel}, {Operation: Status}} {
		if err := request.Validate(); err != nil {
			t.Fatalf("%+v: %v", request, err)
		}
	}
	for _, request := range []Request{{Operation: Schedule}, {Operation: Schedule, IntervalMinutes: 14}, {Operation: Schedule, IntervalMinutes: MaxIntervalMinutes + 1}, {Operation: Cancel, IntervalMinutes: 15}, {Operation: "run"}} {
		if request.Validate() == nil {
			t.Fatalf("accepted %+v", request)
		}
	}
}

func TestAcceptMatchesRequestedChange(t *testing.T) {
	schedule, cancel, status := Request{Operation: Schedule, IntervalMinutes: 60}, Request{Operation: Cancel}, Request{Operation: Status}
	if schedule.Accept(State{Scheduled: false}) == nil || cancel.Accept(State{Scheduled: true}) == nil {
		t.Fatal("accepted a result that contradicts the request")
	}
	for _, check := range []error{schedule.Accept(State{Scheduled: true}), cancel.Accept(State{}), status.Accept(State{}), status.Accept(State{Scheduled: true})} {
		if check != nil {
			t.Fatal(check)
		}
	}
}

func TestRunnerRunsOnceAndCancels(t *testing.T) {
	started := make(chan struct{})
	runner := NewRunner(func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return nil
	})
	done := make(chan error, 1)
	go func() { done <- runner.Run(10_000) }()
	<-started
	if err := runner.Run(10_000); err == nil {
		t.Fatal("ran two refreshes at once")
	}
	runner.Cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled run returned %v", err)
	}
	ran := false
	runner = NewRunner(func(context.Context) error { ran = true; return nil })
	if err := runner.Run(0); err != nil || !ran {
		t.Fatalf("run = %v, ran = %t", err, ran)
	}
	runner.Cancel()
}

func TestRunnerTimeout(t *testing.T) {
	runner := NewRunner(func(ctx context.Context) error { <-ctx.Done(); return nil })
	start := time.Now()
	if err := runner.Run(20); !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 5*time.Second {
		t.Fatalf("timeout run returned %v", err)
	}
	if (*Runner)(nil).Run(1) == nil {
		t.Fatal("nil runner ran")
	}
}
