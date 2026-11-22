// Package desktop draws Go views in a GPU-backed desktop window.
package desktop

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework"
)

// View describes a window. Content is the GPU renderer's composable view.
// The workspace fields also support the Windows system-control renderer.
type View struct {
	Title   string
	Content Node
	Buttons []Button
	Filter  Choice
	Table   Table
	Detail  Detail
	Status  string
}

type Button struct {
	Label    string
	Disabled bool
	Press    func()
}

type Choice struct {
	Options  []string
	Selected int
	Change   func(int)
}

type Column struct {
	Title string
	Width int // initial width at 96 DPI
}

// Table is a virtual list. The renderer asks for individual cells as needed.
// Within one key, existing rows must retain their indices when rows are added.
// Changing Table.Key replaces the rows and resets scroll. The key passed to
// VirtualTable identifies the control independently of the current rows.
type Table struct {
	Key      string
	Columns  []Column
	Count    int
	Follow   bool
	Selected int // -1 when no row is selected
	Cell     func(row, column int) string
	Select   func(row int)
	Copy     string // optional clipboard text for the selected row
}

type Detail struct {
	Title string
	Text  string
	Copy  string
}

// Run opens a window until it closes or ctx is canceled. The renderer calls
// view and its actions on the window thread. updates coalesces data-change
// notifications from workers and may be nil for static views.
func Run(ctx context.Context, view func() View, updates <-chan struct{}) error {
	return run(ctx, view, updates)
}

// Host runs a window after GoFastr boots the app's services.
type Host struct {
	View    func() View
	Updates <-chan struct{}
	// NativeControls selects the optional Windows system-control renderer.
	NativeControls bool
}

func (h Host) Run(ctx context.Context, _ *framework.Session) error {
	if h.NativeControls {
		return runNative(ctx, h.View, h.Updates)
	}
	return Run(ctx, h.View, h.Updates)
}

// Stop has no resources to release. GoFastr cancels the window's context
// before calling Stop.
func (Host) Stop(context.Context) error { return nil }
