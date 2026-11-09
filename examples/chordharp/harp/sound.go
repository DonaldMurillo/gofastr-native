package harp

import (
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
)

// Sound is the instrument's voicing: how high it sits and how bright it is.
type Sound struct {
	// Octave shifts the pad, the strumplate and the accompaniment by whole
	// octaves above Classic, from MinOctave to MaxOctave. The bass follows
	// it between 0 and +1 only, so it stays a bass.
	Octave int `json:"octave"`
	// Tone is how much upper harmonic content the voices have, 0 to 100.
	// Classic is 30.
	Tone int `json:"tone"`
}

// The limits of Sound.Octave.
const MinOctave, MaxOctave = -1, 2

// Preset is a named Sound.
type Preset struct {
	Name  string
	Sound Sound
}

// Presets are the sounds offered by name, in menu order.
var Presets = []Preset{
	{"Classic", Sound{Octave: 0, Tone: 30}},
	{"Bright", Sound{Octave: 1, Tone: 60}},
	{"High", Sound{Octave: 2, Tone: 50}},
}

// Classic is the chord harp's original sound: the pad between F#2 and F3
// and the strumplate from G3.
var Classic = Presets[0].Sound

// DefaultSound is the sound a new engine starts with.
func DefaultSound() Sound { return Presets[1].Sound }

// Clamp keeps both settings in range.
func (s Sound) Clamp() Sound {
	return Sound{Octave: min(max(s.Octave, MinOctave), MaxOctave), Tone: min(max(s.Tone, 0), 100)}
}

// Preset returns the index of the preset s matches, or -1.
func (s Sound) Preset() int {
	for i, p := range Presets {
		if p.Sound == s {
			return i
		}
	}
	return -1
}

// Name is the preset's name, or "Custom".
func (s Sound) Name() string {
	if i := s.Preset(); i >= 0 {
		return Presets[i].Name
	}
	return "Custom"
}

// plateLow is the lowest note the strumplate can play in Classic (G3).
// Every chord's zones start at the first chord tone from here, so the
// plate keeps one register while the chord changes, as a harp's strings
// would.
const plateLow = 55

// StrumNotes returns the MIDI notes of the strumplate's zones for c: the
// chord's tones in ascending order from the plate's lowest note.
func (s Sound) StrumNotes(c Chord) [Zones]int {
	var out [Zones]int
	for i, n := 0, plateLow+12*s.Octave; i < Zones; n++ {
		if c.has(n % 12) {
			out[i] = n
			i++
		}
	}
	return out
}

// PadNotes returns the sustained chord voicing: root position, rooted
// between F#2 and F3 in Classic so it stays under the strumplate.
func (s Sound) PadNotes(c Chord) []int {
	base := 48 + c.Root + 12*s.Octave
	if c.Root > 5 {
		base -= 12
	}
	out := make([]int, 0, 4)
	for _, iv := range intervals[c.Kind] {
		out = append(out, base+iv)
	}
	return out
}

// BassNote returns the accompaniment's bass note for a scale degree of c:
// 'R' root, '3' third, '5' fifth, '6' sixth, '8' octave.
func (s Sound) BassNote(c Chord, degree byte) int {
	root := 36 + c.Root + 12*min(max(s.Octave, 0), 1)
	if c.Root > 4 {
		root -= 12
	}
	third := 4
	if c.Kind == Minor {
		third = 3
	}
	switch degree {
	case '3':
		return root + third
	case '5':
		return root + 7
	case '6':
		return root + 9
	case '8':
		return root + 12
	}
	return root
}

// voicing is a Sound worked out into the synthesizer's numbers. Every
// value is exactly Classic's at Tone 30, which the golden test checks.
type voicing struct {
	// The pad's 2nd, 3rd, 4th and 5th harmonics, and the gain that keeps
	// its level where Classic's is.
	pad     [4]float64
	padNorm float64
	// A pluck's bright harmonics, 2nd to 6th, their starting level, how
	// fast it fades, and the level gain.
	pluck       [5]float64
	pluckBright float64
	pluckFade   float64 // seconds to -60 dB
	pluckNorm   float64
	// The chord accompaniment's 2nd and 3rd harmonics, and its gain.
	comp     [2]float64
	compNorm float64
}

func (s Sound) voicing() voicing {
	d := float64(s.Tone-30) / 100 // -0.3 to 0.7; 0 is Classic
	up := max(d, 0)
	h := math.Pow(2, 2.5*d)
	v := voicing{
		pad:         [4]float64{0.35 * h, 0.18 * h, 0.15 * up, 0.08 * h},
		pluck:       [5]float64{0.5, 0.3, 0.15, 0.25 * up, 0.15 * up},
		pluckBright: 1 + 1.5*d,
		pluckFade:   0.25 * math.Pow(2, 3*d),
		comp:        [2]float64{0.3 * h, 0.12 * h},
	}
	sum := func(w []float64) float64 {
		t := 0.0
		for _, x := range w {
			t += x
		}
		return t
	}
	v.padNorm = (1 + 0.35 + 0.18 + 0.08) / (1 + sum(v.pad[:]))
	v.pluckNorm = (1.4 + 0.95) / (1.4 + v.pluckBright*sum(v.pluck[:]))
	v.compNorm = (1 + 0.3 + 0.12) / (1 + sum(v.comp[:]))
	if d == 0 {
		// Rounding in the sums above must not move Classic at all.
		v.padNorm, v.pluckNorm, v.compNorm = 1, 1, 1
	}
	return v
}

// LoadSound reads a sound saved by SaveSound. A missing file is the
// default sound, not an error.
func LoadSound(path string) (Sound, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return DefaultSound(), nil
	}
	if err != nil {
		return DefaultSound(), err
	}
	s := DefaultSound()
	if err := json.Unmarshal(b, &s); err != nil {
		return DefaultSound(), err
	}
	return s.Clamp(), nil
}

// SaveSound writes s as JSON, creating the directory.
func SaveSound(path string, s Sound) error {
	b, err := json.Marshal(s.Clamp())
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
