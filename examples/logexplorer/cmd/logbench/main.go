//go:build windows

// Command logbench measures the Log Explorer window on its real DirectX
// path: cold startup, deterministic replay, steady redraw, and idle.
// Build and run it with CGO disabled, for example:
//
//	$env:GOWORK='off'; $env:CGO_ENABLED='0'
//	go build -o logbench.exe ./examples/logexplorer/cmd/logbench
//	./logbench.exe -mode suite
//
// By default the graphics stack runs hidden: no window appears on the
// desktop, nothing takes the foreground, and other GUI work is not
// disturbed. Update and Draw run on the real DirectX device and their GPU
// commands are submitted, but no frame is presented to a display, so
// presentation and vsync waits are not measured. Pass -visible for a real
// window; that mode needs a desktop slot where a foreground window is
// acceptable.
//
// Timing sources, in the order the frame pipeline touches them:
//
//   - FrameTiming brackets the game loop's Update and Draw callbacks: CPU
//     work for the view refresh, scene layout, input poll, and draw
//     command recording. They do not timestamp the GPU.
//   - Event-to-draw times run from the fixture worker's data notification
//     (a synthetic command; no keyboard or mouse is involved) to the
//     Draw callback of the frame that rendered the new data.
//   - Presentation happens after Draw returns, inside the graphics
//     driver, and is not directly observable. In -mode steady with
//     -fps-mode vsync and -visible, the frame period beyond Update+Draw
//     approximates the present-and-wait-for-vsync time.
//   - Back-buffer readbacks (ReadPixels) force a GPU-to-CPU
//     synchronization. Their latency is reported separately and those
//     frames are excluded from the frame-period statistics.
//
// Interactive mode measures the focused window from input polling in Update
// to return from Draw. Run it with -visible.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"os"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/DonaldMurillo/gofastr-native/examples/logexplorer"
	"github.com/DonaldMurillo/gofastr-native/render/desktop"
)

// mainEntry is stamped before any package-level work runs; the interval
// from the OS process creation to this point is loader and runtime init.
var mainEntry = time.Now()

const tool = "logbench"

// frameRecord is one drawn frame's CPU cost. periodMS spans DrawEnd to
// DrawEnd, so it covers whatever the game loop did between the two
// frames, including any present wait.
type frameRecord struct {
	updateMS float64
	drawMS   float64
	periodMS float64
	updates  int
	readback bool
}

// bench accumulates measurements from the game thread while a mode runs.
type bench struct {
	viewFn      func() desktop.View
	frames      []frameRecord
	lastDrawEnd time.Time

	// View observations, written on the game thread.
	servedRows int

	// Replay correlation.
	eventAt     atomic.Pointer[time.Time]
	drawnEvent  *time.Time
	eventsTotal atomic.Int64
	finalRows   int

	// Phase marks, written on the game thread, read after the run.
	firstUpdateMS  float64
	firstDrawMS    float64
	firstFrameWall time.Time
	firstFrameMem  memorySample
	replayFirstIdx int
	replayDoneIdx  int
	replayDoneWall time.Time
	eventLatencyMS []float64
	readbackFrames []int
	readbackPixels []string
	readbackMS     []float64

	// Cross-goroutine control.
	framesDrawn atomic.Int64
	replayDone  atomic.Bool

	inputSamples []interactiveLatency
}

func (b *bench) noteView(view desktop.View) {
	b.servedRows = view.Table.Count
}

// forwardEvents stamps each fixture notification and forwards it to the
// window. The stamp anchors event-to-draw measurements.
func (b *bench) forwardEvents(ctx context.Context, source <-chan struct{}) chan struct{} {
	forwarded := make(chan struct{}, 1)
	go func() {
		defer close(forwarded)
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-source:
				if !ok {
					return
				}
			}
			now := time.Now()
			b.eventAt.Store(&now)
			b.eventsTotal.Add(1)
			select {
			case forwarded <- struct{}{}:
			default:
			}
		}
	}()
	return forwarded
}

func (b *bench) frame(t desktop.FrameTiming, screen *ebiten.Image, phase *phaseControl) bool {
	record := frameRecord{
		updateMS: durationMS(t.UpdateDuration()),
		drawMS:   durationMS(t.DrawDuration()),
		updates:  t.Updates,
	}
	if !b.lastDrawEnd.IsZero() {
		record.periodMS = durationMS(t.DrawEnd.Sub(b.lastDrawEnd))
	}
	b.lastDrawEnd = t.DrawEnd

	if t.Frame == 0 {
		b.firstUpdateMS, b.firstDrawMS = record.updateMS, record.drawMS
		b.firstFrameWall = t.DrawEnd
		b.firstFrameMem = sampleMemory()
	}
	doneNow := false
	if phase.replay {
		served := b.servedRows
		if b.replayFirstIdx < 0 && served > logexplorer.InitialEntries {
			b.replayFirstIdx = t.Frame
		}
		if pending := b.drawnEvent; pending != nil {
			b.eventLatencyMS = append(b.eventLatencyMS, durationMS(t.DrawEnd.Sub(*pending)))
			b.drawnEvent = nil
		}
		if b.replayDoneIdx < 0 && served == b.finalRows {
			b.replayDoneIdx = t.Frame
			b.replayDoneWall = t.DrawEnd
			b.replayDone.Store(true)
			doneNow = true
		}
	}
	if phase.samplePixels && (t.Frame == phase.pixelFrameA || (phase.pixelFrameB < 0 && doneNow)) {
		start := time.Now()
		b.readbackPixels = append(b.readbackPixels, sampleBackBuffer(screen))
		b.readbackMS = append(b.readbackMS, durationMS(time.Since(start)))
		b.readbackFrames = append(b.readbackFrames, t.Frame)
		record.readback = true
	}
	b.frames = append(b.frames, record)
	b.framesDrawn.Add(1)
	if !t.InputAt.IsZero() {
		b.inputSamples = append(b.inputSamples, interactiveLatency{Frame: t.Frame, LatencyMS: durationMS(t.DrawEnd.Sub(t.InputAt))})
	}
	return phase.stopAtFrame == t.Frame
}

// sampleBackBuffer hashes a region of the table, including the drawn text.
// Read the whole region in one GPU-to-CPU synchronization.
func sampleBackBuffer(screen *ebiten.Image) string {
	bounds := screen.Bounds()
	region := image.Rect(bounds.Dx()/24, bounds.Dy()/4, bounds.Dx()/3, 3*bounds.Dy()/4).Add(bounds.Min)
	pixels := make([]byte, 4*region.Dx()*region.Dy())
	screen.SubImage(region).(*ebiten.Image).ReadPixels(pixels)
	return fmt.Sprintf("%x", sha256.Sum256(pixels))
}

// phaseControl carries the per-mode decisions the frame callback needs.
type phaseControl struct {
	replay       bool
	samplePixels bool
	pixelFrameA  int
	pixelFrameB  int
	stopAtFrame  int // -1 keeps running; the named frame stops the run
	fpsModeVsync bool
	observeInput bool
	onStart      func()
}

type dist struct {
	N    int     `json:"n"`
	Mean float64 `json:"mean"`
	P50  float64 `json:"p50"`
	P95  float64 `json:"p95"`
	P99  float64 `json:"p99"`
	Max  float64 `json:"max"`
}

func distribution(values []float64) dist {
	if len(values) == 0 {
		return dist{}
	}
	sorted := append([]float64(nil), values...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	sum := 0.0
	for _, v := range sorted {
		sum += v
	}
	at := func(p float64) float64 { return sorted[min(len(sorted)-1, int(p*float64(len(sorted))))] }
	return dist{
		N: len(sorted), Mean: sum / float64(len(sorted)),
		P50: at(0.50), P95: at(0.95), P99: at(0.99), Max: sorted[len(sorted)-1],
	}
}

type durationStats struct {
	UpdateMS       dist `json:"update_ms"`
	DrawMS         dist `json:"draw_ms"`
	UpdatePlusDraw dist `json:"update_plus_draw_ms"`
	PeriodMS       dist `json:"period_ms"`
	EventToDrawMS  dist `json:"event_to_draw_ms"`
}

func statsFor(frames []frameRecord, events []float64) durationStats {
	updates, draws, combined, periods := make([]float64, 0, len(frames)), make([]float64, 0, len(frames)), make([]float64, 0, len(frames)), make([]float64, 0, len(frames))
	for _, f := range frames {
		if f.readback {
			continue
		}
		updates = append(updates, f.updateMS)
		draws = append(draws, f.drawMS)
		combined = append(combined, f.updateMS+f.drawMS)
		if f.periodMS > 0 {
			periods = append(periods, f.periodMS)
		}
	}
	return durationStats{
		UpdateMS: distribution(updates), DrawMS: distribution(draws),
		UpdatePlusDraw: distribution(combined), PeriodMS: distribution(periods),
		EventToDrawMS: distribution(events),
	}
}

type startupReport struct {
	ProcessCreateToMainMS float64 `json:"process_create_to_main_ms"`
	MainToFirstDrawEndMS  float64 `json:"main_to_first_draw_end_ms"`
	FirstUpdateMS         float64 `json:"first_update_ms"`
	FirstDrawMS           float64 `json:"first_draw_ms"`
	WorkingSetMB          float64 `json:"working_set_mb"`
	Note                  string  `json:"note"`
}

type replayReport struct {
	Seconds        float64       `json:"seconds"`
	Events         int64         `json:"events"`
	Frames         int           `json:"frames"`
	RowsFinal      int           `json:"rows_final"`
	MaxCatchup     int           `json:"max_updates_per_frame"`
	InputSource    string        `json:"input_source"`
	Durations      durationStats `json:"durations"`
	ReadbackFrames []int         `json:"readback_frames"`
	ReadbackPixels []string      `json:"readback_pixels"`
	ReadbackMS     dist          `json:"readback_ms"`
	UserMS         float64       `json:"cpu_user_ms"`
	KernelMS       float64       `json:"cpu_kernel_ms"`
	AllocMB        float64       `json:"alloc_mb"`
	GCCount        uint32        `json:"gc_count"`
}

type idleReport struct {
	Seconds          float64 `json:"seconds"`
	Frames           int     `json:"frames"`
	CPUPercent       float64 `json:"cpu_percent"`
	UserMS           float64 `json:"cpu_user_ms"`
	KernelMS         float64 `json:"cpu_kernel_ms"`
	WorkingSetMB     float64 `json:"working_set_mb"`
	PeakWorkingSetMB float64 `json:"peak_working_set_mb"`
	InputSource      string  `json:"input_source"`
}

type steadyReport struct {
	Seconds      float64       `json:"seconds"`
	RateHz       float64       `json:"rate_hz"`
	FPSMode      string        `json:"fps_mode"`
	Frames       int           `json:"frames"`
	Durations    durationStats `json:"durations"`
	CPUPercent   float64       `json:"cpu_percent"`
	WorkingSetMB float64       `json:"working_set_mb"`
	Note         string        `json:"note"`
}

type report struct {
	Tool            string `json:"tool"`
	Timestamp       string `json:"timestamp"`
	Mode            string `json:"mode"`
	Hidden          bool   `json:"hidden"`
	FPSMode         string `json:"fps_mode"`
	WindowShown     bool   `json:"window_shown"`
	GraphicsLibrary string `json:"graphics_library"`
	InputSource     string `json:"input_source"`

	ForegroundSamples int `json:"foreground_samples"`
	ForegroundOwned   int `json:"foreground_owned_samples"`

	Startup     *startupReport     `json:"startup,omitempty"`
	Replay      *replayReport      `json:"replay,omitempty"`
	Idle        *idleReport        `json:"idle,omitempty"`
	Steady      *steadyReport      `json:"steady,omitempty"`
	Interactive *interactiveReport `json:"interactive,omitempty"`

	Checks []check `json:"checks"`
}

type check struct {
	Name     string `json:"name"`
	Pass     bool   `json:"pass"`
	Measured string `json:"measured"`
	Limit    string `json:"limit"`
}

func (r *report) addCheck(name string, pass bool, measured, limit string) {
	r.Checks = append(r.Checks, check{name, pass, measured, limit})
}

func (r *report) failed() bool {
	for _, c := range r.Checks {
		if !c.Pass {
			return true
		}
	}
	return false
}

func durationMS(d time.Duration) float64 { return float64(d.Nanoseconds()) / 1e6 }

func main() {
	mode := flag.String("mode", "suite", "suite|startup|replay|idle|steady|interactive")
	visible := flag.Bool("visible", false, "show a real window instead of running hidden; needs a desktop slot where a foreground window is acceptable")
	seconds := flag.Float64("seconds", 0, "phase length in seconds for replay (default 10, the fixture length), steady (default 5), and interactive (default 60)")
	idleSeconds := flag.Float64("idle-seconds", 5, "idle observation length in seconds")
	startupFrames := flag.Int("startup-frames", 2, "startup mode: frames to draw before exiting")
	rate := flag.Float64("rate", 60, "steady mode: frame schedule rate in Hz")
	fpsMode := flag.String("fps-mode", "app", "steady mode: app keeps the example's on-demand rendering, vsync restores continuous 60 Hz presentation")
	jsonPath := flag.String("json", "", "write the JSON report to this file")
	flag.Parse()

	defaultSeconds := map[string]float64{"replay": logexplorer.ReplayDuration.Seconds(), "steady": 5, "interactive": 60}
	phaseSeconds := *seconds
	if phaseSeconds == 0 {
		phaseSeconds = defaultSeconds[*mode]
	}
	if *fpsMode != "app" && *fpsMode != "vsync" {
		fmt.Fprintf(os.Stderr, "logbench: -fps-mode must be app or vsync\n")
		os.Exit(2)
	}
	forcedVisible := *mode == "interactive"
	if forcedVisible && !*visible {
		fmt.Fprintln(os.Stderr, "logbench: interactive mode needs a visible window; pass -visible")
		os.Exit(2)
	}
	if *visible {
		fmt.Println("logbench: visible window mode; the window may take the foreground")
	}

	r := &report{
		Tool: tool, Timestamp: time.Now().UTC().Format(time.RFC3339),
		Mode: *mode, Hidden: !*visible && !forcedVisible, FPSMode: *fpsMode,
		InputSource: "synthetic: deterministic fixture worker and Go callbacks; no keyboard or mouse",
	}
	var exitErr error
	switch *mode {
	case "startup":
		exitErr = runStartup(r, *visible, *startupFrames)
	case "replay":
		exitErr = runReplay(r, *visible, phaseSeconds, 0)
	case "idle":
		exitErr = runIdle(r, *visible, *idleSeconds)
	case "steady":
		exitErr = runSteady(r, *visible, phaseSeconds, *rate, *fpsMode)
	case "interactive":
		exitErr = runInteractive(r, phaseSeconds)
	case "suite":
		exitErr = runSuite(r, *visible, *idleSeconds)
	default:
		fmt.Fprintf(os.Stderr, "logbench: unknown mode %q\n", *mode)
		os.Exit(2)
	}
	if exitErr != nil {
		fmt.Fprintf(os.Stderr, "logbench: %v\n", exitErr)
	}
	writeReport(r, *jsonPath)
	if exitErr != nil || r.failed() {
		os.Exit(1)
	}
}

// runWindow starts the instrumented DirectX window and the shared
// samplers, and returns once the window closed.
func runWindow(r *report, b *bench, ctx context.Context, updates <-chan struct{}, hidden bool, phase *phaseControl) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var foregroundSamples, foregroundOwned atomic.Int64
	stopForeground := sampleForeground(&foregroundSamples, &foregroundOwned)
	defer stopForeground()

	graphics := make(chan string, 1)
	err := desktop.RunInstrumented(runCtx, b.viewFn, updates, desktop.InstrumentOptions{Hidden: hidden, ObserveInput: phase.observeInput},
		func(t desktop.FrameTiming, screen *ebiten.Image) bool {
			if t.Frame == 1 {
				var info ebiten.DebugInfo
				ebiten.ReadDebugInfo(&info)
				graphics <- info.GraphicsLibrary.String()
				if phase.fpsModeVsync {
					ebiten.SetFPSMode(ebiten.FPSModeVsyncOn)
				}
				if phase.onStart != nil {
					phase.onStart()
				}
			}
			return b.frame(t, screen, phase)
		})
	close(graphics)
	if library, ok := <-graphics; ok {
		r.GraphicsLibrary = library
	}
	r.WindowShown = !hidden
	r.ForegroundSamples = int(foregroundSamples.Load())
	r.ForegroundOwned = int(foregroundOwned.Load())
	return err
}

func sampleForeground(samples, owned *atomic.Int64) (stop func()) {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				samples.Add(1)
				if foregroundIsOwnWindow() {
					owned.Add(1)
				}
			}
		}
	}()
	return func() { close(done) }
}

func memStats() (allocMB float64, gc uint32) {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return float64(stats.TotalAlloc) / (1 << 20), stats.NumGC
}

func writeReport(r *report, jsonPath string) {
	if jsonPath != "" {
		data, err := json.MarshalIndent(r, "", "  ")
		if err == nil {
			err = os.WriteFile(jsonPath, data, 0o666)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "logbench: writing %s: %v\n", jsonPath, err)
		}
	}
	printReport(r)
	if r.failed() {
		fmt.Println("RESULT: FAIL")
	} else {
		fmt.Println("RESULT: PASS")
	}
}
