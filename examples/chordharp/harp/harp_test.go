package harp

import (
	"math"
	"testing"
)

const rate = 44100

func TestStrumNotesAreChordTonesAscending(t *testing.T) {
	for _, root := range Roots {
		for kind := Major; kind <= Seventh; kind++ {
			c := Chord{Root: root, Kind: kind}
			notes := Classic.StrumNotes(c)
			for i, n := range notes {
				if !c.has(n % 12) {
					t.Fatalf("%v zone %d: %s is not in the chord", c, i, NoteName(n))
				}
				if i > 0 && n <= notes[i-1] {
					t.Fatalf("%v zones not ascending: %v", c, notes)
				}
			}
			if notes[0] < plateLow || notes[0] >= plateLow+5 {
				t.Fatalf("%v starts at %s, far from the plate's register", c, NoteName(notes[0]))
			}
		}
	}
	// C major from G3: G C E, four times over.
	got := Classic.StrumNotes(Chord{Root: 0, Kind: Major})
	want := [Zones]int{55, 60, 64, 67, 72, 76, 79, 84, 88, 91, 96, 100}
	if got != want {
		t.Fatalf("C major plate %v, want %v", got, want)
	}
}

func TestBassAndTones(t *testing.T) {
	if got := Classic.BassNote(Chord{Root: 9, Kind: Minor}, '3'); got%12 != 0 {
		t.Errorf("A minor third is %s, want C", NoteName(got))
	}
	if got := (Chord{Root: 10, Kind: Major}).Tones(); got != "Bb D F" {
		t.Errorf("Bb major tones %q", got)
	}
}

func TestPatternsHaveOneCharacterPerStep(t *testing.T) {
	for _, p := range Patterns {
		if p.Steps%p.PerBeat != 0 {
			t.Errorf("%s: %d steps do not divide into beats of %d", p.Name, p.Steps, p.PerBeat)
		}
		for name, track := range map[string]string{"kick": p.Kick, "snare": p.Snare, "hat": p.Hat, "ride": p.Ride, "rim": p.Rim, "bass": p.Bass, "comp": p.Comp} {
			if track != "" && len(track) != p.Steps {
				t.Errorf("%s %s has %d steps, want %d", p.Name, name, len(track), p.Steps)
			}
		}
	}
}

func render(e *Engine, seconds float64) []float32 {
	out := make([]float32, int(seconds*rate))
	for i := 0; i < len(out); i += 512 {
		e.Render(out[i:min(i+512, len(out))])
	}
	return out
}

func rms(s []float32) float64 {
	var sum float64
	for _, v := range s {
		sum += float64(v) * float64(v)
	}
	return math.Sqrt(sum / float64(max(len(s), 1)))
}

func checkSamples(t *testing.T, s []float32) {
	t.Helper()
	for i, v := range s {
		if math.IsNaN(float64(v)) || v > 1 || v < -1 {
			t.Fatalf("sample %d is %v", i, v)
		}
	}
}

func TestEngineSilentUntilPlayed(t *testing.T) {
	e := NewEngine(rate)
	if r := rms(render(e, 0.2)); r != 0 {
		t.Fatalf("idle engine RMS %v", r)
	}
}

func TestPadHoldsAndReleases(t *testing.T) {
	e := NewEngine(rate)
	e.SetChord(Chord{Root: 0, Kind: Major}, true)
	held := render(e, 1)
	checkSamples(t, held)
	if r := rms(held[rate/2:]); r < 0.02 {
		t.Fatalf("held chord RMS %v", r)
	}
	e.SetChord(Chord{Root: 0, Kind: Major}, false)
	tail := render(e, 2)
	if r := rms(tail[len(tail)-rate/4:]); r > 1e-3 {
		t.Fatalf("released chord still sounds: RMS %v", r)
	}
}

func TestPluckDecaysWithSustain(t *testing.T) {
	for _, sustain := range []float64{0.3, 2} {
		e := NewEngine(rate)
		p := e.Params()
		p.Sustain = sustain
		e.Set(p)
		e.Pluck(1, 0) // C4 on the C major plate
		// Plucks drop at -80 dB, a third past the -60 dB sustain time.
		s := render(e, sustain*1.4)
		checkSamples(t, s)
		if r := rms(s[:rate/20]); r < 0.02 {
			t.Fatalf("sustain %v: pluck RMS %v", sustain, r)
		}
		if r := rms(s[len(s)-rate/20:]); r > 2e-3 {
			t.Fatalf("sustain %v: still %v after the sustain time", sustain, r)
		}
		if e.Voices() != 0 {
			t.Fatalf("sustain %v: %d voices left", sustain, e.Voices())
		}
	}
}

func TestPluckDelayIsSampleAccurate(t *testing.T) {
	e := NewEngine(rate)
	e.Pluck(4, 0.1)
	s := render(e, 0.2)
	first := -1
	for i, v := range s {
		if v != 0 {
			first = i
			break
		}
	}
	if want := int(0.1 * rate); first < want-1 || first > want+1 {
		t.Fatalf("delayed pluck starts at sample %d, want %d", first, want)
	}
}

func TestRhythmFollowsTempo(t *testing.T) {
	e := NewEngine(rate)
	p := e.Params()
	p.Tempo, p.Pattern, p.Playing = 120, 0, true // rock: 4 steps a beat
	e.Set(p)
	if e.Step() != 0 {
		t.Fatalf("starts on step %d", e.Step())
	}
	// At 120 bpm a step lasts 0.125 s; after 1.01 s it is on step 8.
	s := render(e, 1.01)
	checkSamples(t, s)
	if e.Step() != 8 {
		t.Fatalf("after one second on step %d, want 8", e.Step())
	}
	if rms(s) < 0.01 {
		t.Fatal("drums are silent")
	}
	p.Playing = false
	e.Set(p)
	if e.Step() != -1 {
		t.Fatalf("stopped rhythm reports step %d", e.Step())
	}
}

func TestEngineStaysInRangeUnderLoad(t *testing.T) {
	e := NewEngine(rate)
	p := e.Params()
	p.Master, p.Chord, p.Harp, p.Rhythm, p.Playing, p.Pattern = 1, 1, 1, 1, true, 3
	e.Set(p)
	e.SetChord(Chord{Root: 7, Kind: Seventh}, true)
	for i := range 100 {
		e.Pluck(i%Zones, float64(i)*0.005)
	}
	checkSamples(t, render(e, 1))
	if v := e.Voices(); v > maxPlucks {
		t.Fatalf("%d voices, cap is %d", v, maxPlucks)
	}
}
