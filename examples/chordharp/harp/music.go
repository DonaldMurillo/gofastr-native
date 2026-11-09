// Package harp is the chord harp's music and synthesizer, shared by the
// terminal app and the Android app: chord spelling, the strumplate's notes,
// accompaniment patterns, and an engine that renders mono samples.
package harp

import (
	"strconv"
	"strings"
)

// Kind is a chord quality: one row of the chord grid.
type Kind int

const (
	Major Kind = iota
	Minor
	Seventh
)

var kindNames = [...]string{Major: "major", Minor: "minor", Seventh: "7th"}

func (k Kind) String() string { return kindNames[k] }

// intervals are semitones above the root.
var intervals = [...][]int{
	Major:   {0, 4, 7},
	Minor:   {0, 3, 7},
	Seventh: {0, 4, 7, 10},
}

// Roots are the grid's columns, in fifths from E flat to B, so neighbors
// are the chords a song most often moves between.
var Roots = [...]int{3, 10, 5, 0, 7, 2, 9, 4, 11}

// Zones is how many notes the strumplate holds.
const Zones = 12

// Kinds is how many chord qualities there are: the chord grid's rows.
const Kinds = 3

var noteNames = [...]string{"C", "C#", "D", "Eb", "E", "F", "F#", "G", "Ab", "A", "Bb", "B"}

// Chord is a root pitch class (0 is C) and a quality.
type Chord struct {
	Root int
	Kind Kind
}

func (c Chord) String() string {
	suffix := [...]string{Major: "", Minor: "m", Seventh: "7"}[c.Kind]
	return noteNames[c.Root] + suffix
}

// Long names the chord in words, for the header.
func (c Chord) Long() string {
	switch c.Kind {
	case Minor:
		return noteNames[c.Root] + " minor"
	case Seventh:
		return noteNames[c.Root] + " seventh"
	}
	return noteNames[c.Root] + " major"
}

// Tones lists the chord's notes, as in "Bb D F".
func (c Chord) Tones() string {
	var names []string
	for _, iv := range intervals[c.Kind] {
		names = append(names, noteNames[(c.Root+iv)%12])
	}
	return strings.Join(names, " ")
}

// has reports whether pitch class pc belongs to the chord.
func (c Chord) has(pc int) bool {
	for _, iv := range intervals[c.Kind] {
		if (c.Root+iv)%12 == pc {
			return true
		}
	}
	return false
}

// NoteName names a MIDI note with its octave, as in "G3".
func NoteName(midi int) string {
	return noteNames[midi%12] + strconv.Itoa(midi/12-1)
}

// Pattern is one accompaniment rhythm. Each track has one character per
// step: 'x' is a full hit, 'o' a soft one, '.' silence. Bass uses scale
// degrees (see BassNote) in place of 'x'.
type Pattern struct {
	Name           string
	Steps, PerBeat int
	Kick, Snare    string
	Hat, Ride, Rim string
	Bass, Comp     string
}

// Beats is the number of beats in one bar.
func (p Pattern) Beats() int { return p.Steps / p.PerBeat }

// Patterns are the accompaniment rhythms, in menu order.
var Patterns = []Pattern{
	{
		Name: "Rock", Steps: 16, PerBeat: 4,
		Kick:  "x.......x.x.....",
		Snare: "....x.......x...",
		Hat:   "x.o.x.o.x.o.x.o.",
		Bass:  "R.......R.5.....",
		Comp:  "....x.......x...",
	},
	{
		Name: "Waltz", Steps: 12, PerBeat: 4,
		Kick: "x...........",
		Hat:  "....o...o...",
		Bass: "R.......5...",
		Comp: "....x...x...",
	},
	{
		Name: "Slow rock", Steps: 12, PerBeat: 3,
		Kick:  "x.....x..x..",
		Snare: "...x.....x..",
		Hat:   "xooxooxooxoo",
		Bass:  "R.....5.....",
		Comp:  "...x.....x..",
	},
	{
		Name: "Latin", Steps: 16, PerBeat: 4,
		Kick: "x..xx..xx..xx..x",
		Rim:  "x..x..x...x.x...",
		Hat:  "o.o.o.o.o.o.o.o.",
		Bass: "R..5....R..5....",
		Comp: "..x..x....x..x..",
	},
	{
		Name: "Foxtrot", Steps: 16, PerBeat: 4,
		Kick:  "x.......x.......",
		Snare: "....o.......o...",
		Ride:  "x...x...x...x...",
		Bass:  "R.......5.......",
		Comp:  "....x.......x...",
	},
	{
		Name: "Swing", Steps: 12, PerBeat: 3,
		Kick: "o.....o.....",
		Hat:  "...x.....x..",
		Ride: "x..x.xx..x.x",
		Bass: "R..3..5..6..",
		Comp: "...x.....x..",
	},
}
