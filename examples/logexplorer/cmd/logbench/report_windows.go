//go:build windows

package main

import (
	"fmt"
	"strings"
)

func printDist(name string, d dist) {
	fmt.Printf("  %-22s n=%-4d mean=%8.3f  p50=%8.3f  p95=%8.3f  p99=%8.3f  max=%8.3f  (ms)\n",
		name, d.N, d.Mean, d.P50, d.P95, d.P99, d.Max)
}

// printReport writes the human-readable summary. Every number carries the
// timing source it came from; see the package comment for what each
// timestamp means.
func printReport(r *report) {
	shown := "hidden (nothing on the desktop)"
	if !r.Hidden {
		shown = "visible window"
	}
	fmt.Printf("logbench %s  mode=%s  %s  fps-mode=%s\n", r.Timestamp, r.Mode, shown, r.FPSMode)
	fmt.Printf("graphics=%s  input=%s\n", r.GraphicsLibrary, r.InputSource)
	if r.ForegroundSamples > 0 {
		fmt.Printf("foreground lock: own window in %d of %d samples\n", r.ForegroundOwned, r.ForegroundSamples)
	}

	if r.Startup != nil {
		s := r.Startup
		fmt.Printf("\nstartup (fresh process, CPU clocks):\n")
		fmt.Printf("  process create -> Go entry : %8.1f ms  (OS loader + runtime init)\n", s.ProcessCreateToMainMS)
		fmt.Printf("  Go entry -> first Draw end : %8.1f ms  (fonts, DirectX device, first scene)\n", s.MainToFirstDrawEndMS)
		fmt.Printf("  first Update %8.2f ms   first Draw %8.2f ms   working set %.1f MB\n",
			s.FirstUpdateMS, s.FirstDrawMS, s.WorkingSetMB)
	}
	if r.Replay != nil {
		p := r.Replay
		fmt.Printf("\nreplay (%.0f s, deterministic fixture, event-driven frames):\n", p.Seconds)
		fmt.Printf("  events=%d  frames drawn=%d  coalescing=%.2f  rows final=%d  max catch-up updates/frame=%d\n",
			p.Events, p.Frames, float64(p.Events)/float64(max(1, p.Frames)), p.RowsFinal, p.MaxCatchup)
		printDist("Update", p.Durations.UpdateMS)
		printDist("Draw", p.Durations.DrawMS)
		printDist("Update+Draw", p.Durations.UpdatePlusDraw)
		printDist("frame period", p.Durations.PeriodMS)
		printDist("event -> Draw end", p.Durations.EventToDrawMS)
		if len(p.ReadbackPixels) > 0 {
			printDist("back-buffer readback", p.ReadbackMS)
			fmt.Printf("  back-buffer hashes: %s\n", strings.Join(p.ReadbackPixels, " | "))
		}
	}
	if r.Idle != nil {
		i := r.Idle
		fmt.Printf("\nidle (%.0f s, no data events):\n", i.Seconds)
		fmt.Printf("  frames=%d  CPU=%.2f%% (user %.1f ms + kernel %.1f ms)  working set=%.1f MB peak=%.1f MB\n",
			i.Frames, i.CPUPercent, i.UserMS, i.KernelMS, i.WorkingSetMB, i.PeakWorkingSetMB)
		fmt.Printf("  input: %s\n", i.InputSource)
	}
	if r.Steady != nil {
		s := r.Steady
		fmt.Printf("\nsteady (%.0f s at %.0f Hz schedule, %s):\n", s.Seconds, s.RateHz, s.FPSMode)
		fmt.Printf("  frames=%d  CPU=%.2f%%  working set=%.1f MB\n", s.Frames, s.CPUPercent, s.WorkingSetMB)
		printDist("Update", s.Durations.UpdateMS)
		printDist("Draw", s.Durations.DrawMS)
		printDist("Update+Draw", s.Durations.UpdatePlusDraw)
		printDist("frame period", s.Durations.PeriodMS)
		fmt.Printf("  %s\n", s.Note)
	}
	if r.Interactive != nil {
		i := r.Interactive
		fmt.Printf("\ninteractive (%.0f s, focused window):\n", i.Seconds)
		fmt.Printf("  frames with input=%d\n", len(i.Samples))
		printDist("input poll -> Draw end", i.LatencyMS)
		for _, sample := range i.Samples {
			fmt.Printf("  frame %-4d %8.2f ms\n", sample.Frame, sample.LatencyMS)
		}
		fmt.Printf("  %s\n", i.Note)
	}

	if len(r.Checks) > 0 {
		fmt.Printf("\nacceptance:\n")
		for _, c := range r.Checks {
			verdict := "PASS"
			if !c.Pass {
				verdict = "FAIL"
			}
			fmt.Printf("  %-4s %-55s measured %-28s limit %s\n", verdict, c.Name, c.Measured, c.Limit)
		}
	}
}
