package desktop

import (
	"context"
	"errors"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// FrameTiming carries the CPU timestamps of one rendered frame of the
// DirectX game loop. UpdateStart and UpdateEnd bracket the game's Update
// callback (view refresh, scene layout, input polling). DrawStart and
// DrawEnd bracket the Draw callback, which records the frame's GPU
// commands. DrawEnd does not mean the GPU finished: presentation happens
// after Draw returns inside the graphics driver, and the display scanout
// after that. Neither is observable through this API; the next frame's
// UpdateStart marks the loop resuming once the driver call returned.
type FrameTiming struct {
	// Frame counts Draw calls since the run started, starting at zero.
	Frame int
	// Updates is how many Update calls ran since the previous frame.
	// Zero marks a redraw with no new update, one a regular frame, and
	// more than one the loop catching up after a slow frame.
	Updates     int
	UpdateStart time.Time
	UpdateEnd   time.Time
	DrawStart   time.Time
	DrawEnd     time.Time
	// InputAt marks the first update that observed input since the last draw.
	// It is a CPU timestamp from inside the window's event loop.
	InputAt time.Time
}

func (t FrameTiming) UpdateDuration() time.Duration { return t.UpdateEnd.Sub(t.UpdateStart) }
func (t FrameTiming) DrawDuration() time.Duration   { return t.DrawEnd.Sub(t.DrawStart) }

// InstrumentOptions configures an instrumented window run.
type InstrumentOptions struct {
	// Hidden runs the graphics stack without ever showing the window.
	// The game loop still ticks and renders: Update and Draw run, GPU
	// commands are submitted, but no frame is presented to a display.
	// Presentation and vsync waits are therefore not measured.
	Hidden bool
	// ObserveInput timestamps key presses, button presses and wheel activity
	// received by the focused window.
	ObserveInput bool
}

// benchGame wraps a gpuWindow so every rendered frame is timestamped.
type benchGame struct {
	*gpuWindow
	onFrame         func(FrameTiming, *ebiten.Image) bool
	updateStart     time.Time
	updateEnd       time.Time
	updatesSinceHit int
	frame           int
	observeInput    bool
	inputAt         time.Time
	keys            []ebiten.Key
}

func (g *benchGame) Update() error {
	g.updateStart = time.Now()
	if g.observeInput && g.inputAt.IsZero() && ebiten.IsFocused() {
		g.keys = inpututil.AppendJustPressedKeys(g.keys[:0])
		x, y := ebiten.Wheel()
		if len(g.keys) > 0 || x != 0 || y != 0 || inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
			g.inputAt = g.updateStart
		}
	}
	err := g.gpuWindow.Update()
	g.updateEnd = time.Now()
	g.updatesSinceHit++
	return err
}

func (g *benchGame) Draw(screen *ebiten.Image) {
	start := time.Now()
	g.gpuWindow.Draw(screen)
	timing := FrameTiming{
		Frame: g.frame, Updates: g.updatesSinceHit,
		UpdateStart: g.updateStart, UpdateEnd: g.updateEnd,
		DrawStart: start, DrawEnd: time.Now(), InputAt: g.inputAt,
	}
	g.frame++
	g.updatesSinceHit = 0
	g.inputAt = time.Time{}
	g.onFrame(timing, screen)
}

// RunInstrumented opens the DirectX window like Run and reports every
// rendered frame's CPU timestamps, plus the frame's back buffer, to onFrame.
// Returning true from onFrame closes the window; canceling ctx does too.
// onFrame runs on the game thread between Draw and the driver's present
// call, so it must stay cheap: work done there is not part of FrameTiming
// but delays the presentation of the frame it observes.
func RunInstrumented(
	ctx context.Context,
	view func() View,
	updates <-chan struct{},
	options InstrumentOptions,
	onFrame func(FrameTiming, *ebiten.Image) bool,
) error {
	if view == nil {
		return errors.New("desktop: a view function is required")
	}
	if onFrame == nil {
		return errors.New("desktop: a frame callback is required")
	}
	if ctx.Err() != nil {
		return nil
	}
	if options.Hidden {
		ebiten.SetWindowVisible(false)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	window, err := newGPUWindow(runCtx, view)
	if err != nil {
		return err
	}
	game := &benchGame{gpuWindow: window, observeInput: options.ObserveInput, onFrame: func(timing FrameTiming, screen *ebiten.Image) bool {
		if onFrame(timing, screen) {
			cancel()
			return true
		}
		return false
	}}
	return runGPUWindow(window, updates, game)
}
