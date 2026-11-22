//go:build windows

package main

import (
	"context"
	"strconv"
	"time"

	"github.com/DonaldMurillo/gofastr-native/examples/logexplorer"
)

type interactiveLatency struct {
	LatencyMS float64 `json:"latency_ms"`
	Frame     int     `json:"frame"`
}

type interactiveReport struct {
	Seconds   float64              `json:"seconds"`
	Samples   []interactiveLatency `json:"samples"`
	LatencyMS dist                 `json:"latency_ms"`
	Note      string               `json:"note"`
}

func runInteractive(r *report, seconds float64) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds*float64(time.Second)))
	defer cancel()

	replay := logexplorer.NewReplay()
	b := newBench(ctx, replay)
	phase := &phaseControl{stopAtFrame: -1, observeInput: true}
	err := runWindow(r, b, ctx, replay.Updates(), false, phase)

	latencies := make([]float64, 0, len(b.inputSamples))
	for _, sample := range b.inputSamples {
		latencies = append(latencies, sample.LatencyMS)
	}
	r.InputSource = "keyboard and pointer activity received by the focused window"
	r.Interactive = &interactiveReport{
		Seconds: seconds, Samples: b.inputSamples, LatencyMS: distribution(latencies),
		Note: "timing starts at the update that reads input and ends at Draw return; use the arrow keys, click rows, or scroll to produce samples",
	}
	r.addCheck("interactive: graphics library is DirectX", r.GraphicsLibrary == "DirectX", r.GraphicsLibrary, "DirectX")
	r.addCheck("interactive: window received input", len(latencies) > 0, strconv.Itoa(len(latencies)), "at least one frame with input")
	return err
}
