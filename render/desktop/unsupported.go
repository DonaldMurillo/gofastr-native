//go:build !windows

package desktop

import (
	"context"
	"errors"
)

func run(context.Context, func() View, <-chan struct{}) error {
	return errors.New("the desktop renderer currently requires Windows")
}

func runNative(ctx context.Context, view func() View, updates <-chan struct{}) error {
	return run(ctx, view, updates)
}
