package harp

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestOctaveShiftsEveryPart(t *testing.T) {
	for _, root := range Roots {
		for kind := Major; kind <= Seventh; kind++ {
			c := Chord{Root: root, Kind: kind}
			for oct := MinOctave; oct <= MaxOctave; oct++ {
				s := Sound{Octave: oct, Tone: 30}
				plate, base := s.StrumNotes(c), Classic.StrumNotes(c)
				for z := range plate {
					if plate[z] != base[z]+12*oct {
						t.Fatalf("%v octave %d zone %d: %d, want %d", c, oct, z, plate[z], base[z]+12*oct)
					}
				}
				pad, basePad := s.PadNotes(c), Classic.PadNotes(c)
				for i := range pad {
					if pad[i] != basePad[i]+12*oct {
						t.Fatalf("%v octave %d pad %v, classic %v", c, oct, pad, basePad)
					}
				}
				bassShift := 12 * min(max(oct, 0), 1)
				if got, want := s.BassNote(c, '5'), Classic.BassNote(c, '5')+bassShift; got != want {
					t.Fatalf("%v octave %d bass %d, want %d", c, oct, got, want)
				}
			}
		}
	}
}

func TestPresets(t *testing.T) {
	if DefaultSound().Name() != "Bright" {
		t.Fatalf("default sound is %q", DefaultSound().Name())
	}
	if NewEngine(rate).Params().Sound != DefaultSound() {
		t.Fatal("a new engine does not start with the default sound")
	}
	for i, p := range Presets {
		if p.Sound.Preset() != i || p.Sound.Clamp() != p.Sound {
			t.Errorf("preset %s does not round-trip", p.Name)
		}
	}
	if got := (Sound{Octave: 1, Tone: 65}).Name(); got != "Custom" {
		t.Errorf("an edited sound is named %q", got)
	}
	if got := (Sound{Octave: 9, Tone: -4}).Clamp(); got != (Sound{Octave: MaxOctave, Tone: 0}) {
		t.Errorf("clamped to %+v", got)
	}
	if got := NoteName(124); got != "E9" {
		t.Errorf("NoteName(124) = %q", got)
	}
}

// edge is the share of a signal's energy in its sample-to-sample change,
// which grows with its high-frequency content.
func edge(s []float32) float64 {
	var d, a float64
	for i := 1; i < len(s); i++ {
		x := float64(s[i] - s[i-1])
		d += x * x
		a += float64(s[i]) * float64(s[i])
	}
	return math.Sqrt(d / max(a, 1e-12))
}

func TestToneBrightensPlucks(t *testing.T) {
	var last float64
	for _, tone := range []int{0, 30, 60, 100} {
		e := NewEngine(rate)
		p := e.Params()
		p.Sound = Sound{Octave: 0, Tone: tone}
		e.Set(p)
		e.Pluck(3, 0)
		s := render(e, 0.4)
		checkSamples(t, s)
		got := edge(s)
		if got <= last {
			t.Fatalf("tone %d: brightness %.4f, not above %.4f", tone, got, last)
		}
		last = got
	}
}

func TestToneKeepsLevelNearClassic(t *testing.T) {
	level := func(tone int) float64 {
		e := NewEngine(rate)
		p := e.Params()
		p.Sound = Sound{Octave: 0, Tone: tone}
		e.Set(p)
		e.SetChord(Chord{Root: 0, Kind: Major}, true)
		return rms(render(e, 0.6)[rate/3:])
	}
	classic := level(30)
	for _, tone := range []int{0, 100} {
		if r := level(tone) / classic; r < 0.7 || r > 1.4 {
			t.Errorf("tone %d pad level is %.2f times Classic's", tone, r)
		}
	}
}

func TestNoHarmonicAboveTheLimit(t *testing.T) {
	e := NewEngine(8000)
	e.startPluck(100, 0) // E7, 2637 Hz: even the 2nd harmonic is past 3600 Hz
	for k, w := range e.plucks[0].harmonics {
		if w != 0 {
			t.Fatalf("harmonic %d kept at %v", k+2, w)
		}
	}
	e = NewEngine(rate)
	e.startPluck(60, 0)
	if e.plucks[0].harmonics[0] == 0 {
		t.Fatal("middle C lost its 2nd harmonic")
	}
}

func TestSoundChangeRetunesHeldPad(t *testing.T) {
	e := classicEngine()
	c := Chord{Root: 0, Kind: Major}
	e.SetChord(c, true)
	render(e, 0.1)
	p := e.Params()
	p.Sound = Sound{Octave: 1, Tone: 30}
	e.Set(p)
	var on []int
	for _, v := range e.pads {
		if v.on {
			on = append(on, v.note)
		}
	}
	want := p.Sound.PadNotes(c)
	if len(on) != len(want) {
		t.Fatalf("pads on %v, want %v", on, want)
	}
	for i := range on {
		if on[i] != want[i] {
			t.Fatalf("pads on %v, want %v", on, want)
		}
	}
}

func TestSoundSavesAndLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "settings.json")
	if s, err := LoadSound(path); err != nil || s != DefaultSound() {
		t.Fatalf("missing file: %+v, %v", s, err)
	}
	want := Sound{Octave: -1, Tone: 85}
	if err := SaveSound(path, want); err != nil {
		t.Fatal(err)
	}
	if s, err := LoadSound(path); err != nil || s != want {
		t.Fatalf("loaded %+v, %v", s, err)
	}
	os.WriteFile(path, []byte(`{"octave": 7}`), 0o644)
	if s, _ := LoadSound(path); s != (Sound{Octave: MaxOctave, Tone: DefaultSound().Tone}) {
		t.Fatalf("out-of-range file loaded as %+v", s)
	}
	os.WriteFile(path, []byte(`not json`), 0o644)
	if s, err := LoadSound(path); err == nil || s != DefaultSound() {
		t.Fatalf("bad file: %+v, %v", s, err)
	}
}
