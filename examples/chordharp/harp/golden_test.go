package harp

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/classic.golden")

// classicScenario plays a held chord, plucks, a chord change and the
// rhythm with its bass and chord accompaniment, then returns the RMS of
// every 10 ms of the result.
func classicScenario(e *Engine) []float64 {
	c := Chord{Root: 7, Kind: Seventh}
	e.SetChord(c, true)
	s := render(e, 0.5)
	// G B D F G B, from G3.
	for z := range 6 {
		e.Pluck(z, float64(z)*0.03)
	}
	s = append(s, render(e, 0.7)...)
	e.SetChord(Chord{Root: 9, Kind: Minor}, true)
	p := e.Params()
	p.Tempo, p.Pattern, p.Playing = 126, 5, true
	e.Set(p)
	s = append(s, render(e, 1.4)...)
	e.SetChord(Chord{Root: 9, Kind: Minor}, false)
	s = append(s, render(e, 0.4)...)
	window := rate / 100
	out := make([]float64, 0, len(s)/window)
	for i := 0; i+window <= len(s); i += window {
		out = append(out, rms(s[i:i+window]))
	}
	return out
}

// classicEngine is an engine set to the Classic sound.
func classicEngine() *Engine {
	e := NewEngine(rate)
	p := e.Params()
	p.Sound = Classic
	e.Set(p)
	return e
}

func TestClassicSoundIsUnchanged(t *testing.T) {
	got := classicScenario(classicEngine())
	path := filepath.Join("testdata", "classic.golden")
	if *update {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		w := bufio.NewWriter(f)
		for _, v := range got {
			fmt.Fprintf(w, "%.7f\n", v)
		}
		if err := w.Flush(); err != nil {
			t.Fatal(err)
		}
		f.Close()
		return
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var want []float64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		v, err := strconv.ParseFloat(sc.Text(), 64)
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, v)
	}
	if len(got) != len(want) {
		t.Fatalf("%d windows, golden has %d", len(got), len(want))
	}
	for i := range got {
		// The tolerance absorbs floating-point differences between
		// architectures, such as fused multiply-adds on arm64.
		if d := got[i] - want[i]; d > 1e-4 || d < -1e-4 {
			t.Fatalf("window %d (%d ms): RMS %.7f, golden %.7f", i, i*10, got[i], want[i])
		}
	}
}
