package main

import (
	"os"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-native/tui/cmd/chordharp/harp"
)

// TestOutputPullsSamples opens the real audio device with nothing playing
// and checks that it asks for samples at the sample rate. It needs a sound
// device, so it runs only with CHORDHARP_AUDIO_TEST=1.
func TestOutputPullsSamples(t *testing.T) {
	if os.Getenv("CHORDHARP_AUDIO_TEST") != "1" {
		t.Skip("set CHORDHARP_AUDIO_TEST=1 to open the sound device")
	}
	e := harp.NewEngine(sampleRate)
	out, err := openAudio(e)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	clock := e.Clock
	// Players take a moment to start; measure the steady rate after.
	time.Sleep(500 * time.Millisecond)
	start, from := time.Now(), clock()
	time.Sleep(time.Second)
	pulled := clock() - from
	elapsed := time.Since(start).Seconds()
	if want := elapsed * sampleRate; float64(pulled) < 0.8*want || float64(pulled) > want+sampleRate/4 {
		t.Fatalf("%s pulled %d samples in %.2f s, want about %.0f", out.Name(), pulled, elapsed, want)
	}
	t.Logf("%s pulled %d samples in %.2f s", out.Name(), pulled, elapsed)
}
