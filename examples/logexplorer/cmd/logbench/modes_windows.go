//go:build windows

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/DonaldMurillo/gofastr-native/examples/logexplorer"
	"github.com/DonaldMurillo/gofastr-native/render/desktop"
)

func megaBytes(bytes uint64) float64 { return float64(bytes) / (1 << 20) }

// scheduleFrame asks the game loop for one more frame from any goroutine.
func scheduleFrame() { ebiten.ScheduleFrame() }

func replayFinalRows() int {
	return logexplorer.InitialEntries + logexplorer.ReplayRate*int(logexplorer.ReplayDuration/time.Second)
}

// newBench wires the real example window into the bench observer.
func newBench(ctx context.Context, replay *logexplorer.Replay) *bench {
	window := logexplorer.NewWindow(ctx, replay)
	b := &bench{finalRows: replayFinalRows(), replayFirstIdx: -1, replayDoneIdx: -1}
	b.viewFn = func() desktop.View {
		if event := b.eventAt.Swap(nil); event != nil && b.drawnEvent == nil {
			b.drawnEvent = event
		}
		view := window.View()
		b.noteView(view)
		return view
	}
	return b
}

// idleData is collected by the watchdog goroutine while the window sits
// without events.
type idleData struct {
	seconds     float64
	frames      int
	userMS      float64
	kernelMS    float64
	cpuPercent  float64
	workingMB   float64
	peakWorking float64
	allocMB     float64
	gcCount     uint32
	truncated   bool
}

// waitReplayDone blocks until the fixture finished rendering or the
// truncation deadline passed.
func waitReplayDone(b *bench, seconds float64) (done bool) {
	deadline := time.Now().Add(time.Duration(seconds*float64(time.Second)) + 2*time.Second)
	for {
		if b.replayDone.Load() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func measureIdle(b *bench, seconds float64, framesBefore int, truncated bool) *idleData {
	cpu0 := sampleCPU()
	alloc0, gc0 := memStats()
	start := time.Now()
	time.Sleep(time.Duration(seconds * float64(time.Second)))
	cpu1, mem1 := sampleCPU(), sampleMemory()
	alloc1, gc1 := memStats()
	elapsedMS := durationMS(time.Since(start))
	totalMS := cpu1.totalMS() - cpu0.totalMS()
	frames := int(b.framesDrawn.Load()) - framesBefore
	percent := 0.0
	if elapsedMS > 0 {
		percent = 100 * totalMS / elapsedMS
	}
	return &idleData{
		seconds: seconds, frames: frames,
		userMS: cpu1.UserMS - cpu0.UserMS, kernelMS: cpu1.KernelMS - cpu0.KernelMS,
		cpuPercent: percent, workingMB: megaBytes(mem1.WorkingSetBytes),
		peakWorking: megaBytes(mem1.PeakWorkingSetBytes),
		allocMB:     alloc1 - alloc0, gcCount: gc1 - gc0, truncated: truncated,
	}
}

func fillReplayReport(b *bench, seconds float64, idle *idleData) *replayReport {
	end := b.replayDoneIdx
	if end < 0 {
		end = len(b.frames) - 1
	}
	startIdx := b.replayFirstIdx
	if startIdx < 0 {
		startIdx = 0
	}
	frames := b.frames[startIdx : end+1]
	catchup := 0
	for _, f := range frames {
		catchup = max(catchup, f.updates)
	}
	report := &replayReport{
		Seconds: seconds, Events: b.eventsTotal.Load(), Frames: len(frames),
		RowsFinal: b.servedRows, MaxCatchup: catchup,
		InputSource:    "synthetic: deterministic fixture worker",
		Durations:      statsFor(frames, b.eventLatencyMS),
		ReadbackFrames: b.readbackFrames, ReadbackPixels: b.readbackPixels,
		ReadbackMS: distribution(b.readbackMS),
	}
	if idle != nil {
		report.UserMS = idle.userMS
		report.KernelMS = idle.kernelMS
	}
	return report
}

func runStartup(r *report, visible bool, frames int) error {
	ctx := context.Background()
	b := newBench(ctx, logexplorer.NewReplay())
	phase := &phaseControl{stopAtFrame: frames - 1}
	err := runWindow(r, b, ctx, nil, !visible, phase)

	createdMS, ok := processCreated()
	startup := &startupReport{
		FirstUpdateMS: b.firstUpdateMS, FirstDrawMS: b.firstDrawMS,
		WorkingSetMB: megaBytes(b.firstFrameMem.WorkingSetBytes),
		Note:         "fresh process; the first run after a boot also pays the cold file cache for the binary, Direct3D DLLs, and fonts",
	}
	if ok {
		created := time.UnixMilli(int64(createdMS))
		startup.ProcessCreateToMainMS = durationMS(mainEntry.Sub(created))
	}
	if !b.firstFrameWall.IsZero() {
		startup.MainToFirstDrawEndMS = durationMS(b.firstFrameWall.Sub(mainEntry))
	}
	r.Startup = startup

	r.addCheck("startup: graphics library is DirectX", r.GraphicsLibrary == "DirectX", r.GraphicsLibrary, "DirectX")
	if !visible {
		r.addCheck("startup: hidden window never shown", !r.WindowShown, fmt.Sprint(r.WindowShown), "false")
	}
	if b.firstFrameWall.After(mainEntry) {
		limit := 2500.0
		r.addCheck("startup: first frame drawn within 2.5 s of entry",
			startup.MainToFirstDrawEndMS < limit, fmt.Sprintf("%.1f ms", startup.MainToFirstDrawEndMS), "< 2500 ms")
	} else {
		r.addCheck("startup: first frame drawn", false, "no frame observed", "at least one frame")
	}
	return err
}

// runReplayAndIdle drives the real replay worker, then observes the idle
// window. It backs both the replay mode and the suite.
func runReplayAndIdle(r *report, visible bool, seconds, idleSeconds float64, withIdle bool) error {
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	replay := logexplorer.NewReplay()
	b := newBench(runCtx, replay)
	updates := b.forwardEvents(runCtx, replay.Updates())
	phase := &phaseControl{replay: true, samplePixels: true, pixelFrameA: 30, pixelFrameB: -1, stopAtFrame: -1}
	phase.onStart = func() { replay.Start(runCtx) }

	var idle *idleData
	go func() {
		done := waitReplayDone(b, seconds)
		if withIdle {
			idle = measureIdle(b, idleSeconds, int(b.framesDrawn.Load()), !done)
		}
		cancel()
	}()
	err := runWindow(r, b, runCtx, updates, !visible, phase)

	r.Replay = fillReplayReport(b, seconds, idle)
	if idle != nil {
		r.Idle = &idleReport{
			Seconds: idle.seconds, Frames: idle.frames, CPUPercent: idle.cpuPercent,
			UserMS: idle.userMS, KernelMS: idle.kernelMS,
			WorkingSetMB: idle.workingMB, PeakWorkingSetMB: idle.peakWorking,
			InputSource: idleInputSource(visible),
		}
	}

	r.addCheck("replay: graphics library is DirectX", r.GraphicsLibrary == "DirectX", r.GraphicsLibrary, "DirectX")
	full := r.Replay.RowsFinal == replayFinalRows()
	r.addCheck("replay: fixture completed", full,
		fmt.Sprintf("%d rows", r.Replay.RowsFinal), fmt.Sprintf("%d rows", replayFinalRows()))
	if !visible {
		r.addCheck("replay: hidden window never owned the foreground", r.ForegroundOwned == 0,
			fmt.Sprintf("%d of %d samples", r.ForegroundOwned, r.ForegroundSamples), "0")
	}
	limit := 16.7
	r.addCheck("replay: Update+Draw p99 fits a 60 Hz frame",
		r.Replay.Durations.UpdatePlusDraw.P99 < limit,
		fmt.Sprintf("%.2f ms", r.Replay.Durations.UpdatePlusDraw.P99), "< 16.7 ms")
	limit = 80.0
	r.addCheck("replay: fixture event to drawn frame p95 under 80 ms",
		r.Replay.Durations.EventToDrawMS.P95 < limit,
		fmt.Sprintf("%.1f ms", r.Replay.Durations.EventToDrawMS.P95), "< 80 ms")
	minFrames := 300
	r.addCheck("replay: coalesced frame rate kept up with the 50 Hz event stream",
		r.Replay.Frames >= minFrames, fmt.Sprintf("%d frames in %.0f s", r.Replay.Frames, r.Replay.Seconds), fmt.Sprintf(">= %d", minFrames))
	if len(r.Replay.ReadbackPixels) == 2 {
		r.addCheck("replay: back buffer content changed across the replay",
			r.Replay.ReadbackPixels[0] != r.Replay.ReadbackPixels[1],
			fmt.Sprintf("%s vs %s", r.Replay.ReadbackPixels[0], r.Replay.ReadbackPixels[1]), "different pixels")
	}
	if idle != nil {
		r.addCheck("idle: no frames while nothing changes", idle.frames <= 1,
			fmt.Sprintf("%d frames in %.0f s", idle.frames, idle.seconds), "<= 1")
		limit := 2.0
		r.addCheck("idle: process CPU under 2 percent", idle.cpuPercent < limit,
			fmt.Sprintf("%.2f%%", idle.cpuPercent), "< 2%")
	}
	return err
}

func idleInputSource(visible bool) string {
	if visible {
		return "none scheduled; a visible window can still receive ambient desktop input"
	}
	return "none: a hidden window receives no keyboard or mouse input"
}

func runReplay(r *report, visible bool, seconds, idleSeconds float64) error {
	return runReplayAndIdle(r, visible, seconds, idleSeconds, false)
}

func runSuite(r *report, visible bool, idleSeconds float64) error {
	return runReplayAndIdle(r, visible, logexplorer.ReplayDuration.Seconds(), idleSeconds, true)
}

func runIdle(r *report, visible bool, seconds float64) error {
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	b := newBench(runCtx, logexplorer.NewReplay())
	phase := &phaseControl{stopAtFrame: -1}

	var idle *idleData
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		for b.framesDrawn.Load() < 1 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		idle = measureIdle(b, seconds, int(b.framesDrawn.Load()), false)
		cancel()
	}()
	err := runWindow(r, b, runCtx, nil, !visible, phase)

	if idle != nil {
		r.Idle = &idleReport{
			Seconds: idle.seconds, Frames: idle.frames, CPUPercent: idle.cpuPercent,
			UserMS: idle.userMS, KernelMS: idle.kernelMS,
			WorkingSetMB: idle.workingMB, PeakWorkingSetMB: idle.peakWorking,
			InputSource: idleInputSource(visible),
		}
		r.addCheck("idle: no frames while nothing changes", idle.frames <= 1,
			fmt.Sprintf("%d frames in %.0f s", idle.frames, idle.seconds), "<= 1")
		r.addCheck("idle: process CPU under 2 percent", idle.cpuPercent < limit2Percent,
			fmt.Sprintf("%.2f%%", idle.cpuPercent), "< 2%")
	}
	r.addCheck("idle: graphics library is DirectX", r.GraphicsLibrary == "DirectX", r.GraphicsLibrary, "DirectX")
	return err
}

const limit2Percent = 2.0

func runSteady(r *report, visible bool, seconds float64, rate float64, fpsMode string) error {
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	b := newBench(runCtx, logexplorer.NewReplay())
	phase := &phaseControl{stopAtFrame: -1, fpsModeVsync: fpsMode == "vsync"}

	var cpu0, cpu1 cpuSample
	var mem1 memorySample
	phase.onStart = func() {
		cpu0 = sampleCPU()
		interval := time.Duration(float64(time.Second) / rate)
		ticker := time.NewTicker(interval)
		go func() {
			defer ticker.Stop()
			deadline := time.After(time.Duration(seconds * float64(time.Second)))
			for {
				select {
				case <-runCtx.Done():
					return
				case <-ticker.C:
					scheduleFrame()
				case <-deadline:
					cpu1, mem1 = sampleCPU(), sampleMemory()
					cancel()
					return
				}
			}
		}()
	}
	err := runWindow(r, b, runCtx, nil, !visible, phase)

	frames := b.frames[min(1, len(b.frames)):]
	elapsedMS := cpu1.totalMS() - cpu0.totalMS()
	percent := 0.0
	if seconds > 0 {
		percent = 100 * elapsedMS / (seconds * 1000)
	}
	note := "on-demand frames scheduled by the bench; period measures Update+Draw plus submission"
	if fpsMode == "vsync" {
		note = "continuous vsync mode; the frame period beyond Update+Draw approximates the driver present-and-wait"
		if !visible {
			note += " (hidden: frames are not presented; pass -visible to measure presentation)"
		}
	}
	r.Steady = &steadyReport{
		Seconds: seconds, RateHz: rate, FPSMode: fpsMode, Frames: len(frames),
		Durations: statsFor(frames, nil), CPUPercent: percent,
		WorkingSetMB: megaBytes(mem1.WorkingSetBytes), Note: note,
	}
	r.addCheck("steady: graphics library is DirectX", r.GraphicsLibrary == "DirectX", r.GraphicsLibrary, "DirectX")
	limit := 16.7
	r.addCheck("steady: Update+Draw p99 fits a 60 Hz frame",
		r.Steady.Durations.UpdatePlusDraw.P99 < limit,
		fmt.Sprintf("%.2f ms", r.Steady.Durations.UpdatePlusDraw.P99), "< 16.7 ms")
	return err
}
