package harp

import (
	"bufio"
	"cmp"
	"encoding/binary"
	"io"
	"math"
	"slices"
)

// The parts of a MIDI export, one track each.
const (
	partHarp = iota
	partPad
	partBass
	partComp
	partDrums
	parts
)

// partSpecs are each part's track name, channel and General MIDI program.
var partSpecs = [parts]struct {
	name    string
	channel byte
	program byte
}{
	partHarp:  {"Harp", 0, 46},    // orchestral harp
	partPad:   {"Chord", 1, 89},   // pad 2 (warm)
	partBass:  {"Bass", 2, 32},    // acoustic bass
	partComp:  {"Comp", 3, 4},     // electric piano 1
	partDrums: {"Drums", 9, 0xff}, // channel 10 needs no program
}

// ppq is the MIDI file's ticks per quarter note.
const ppq = 480

// midiNote is one note an engine played, on its sample clock. off is -1
// until a pad note is let go.
type midiNote struct {
	part, note, vel int
	on, off         int64
}

// noteLog collects the notes an export plays. Its methods do nothing on a
// nil log, which is the live engine's.
type noteLog struct {
	notes []midiNote
	open  map[[2]int]int // part and note to the index of a held pad note
}

func (l *noteLog) note(part, note, vel int, on, off int64) {
	if l == nil {
		return
	}
	l.notes = append(l.notes, midiNote{part: part, note: note, vel: min(max(vel, 1), 127), on: on, off: off})
}

func (l *noteLog) on(part, note, vel int, at int64) {
	if l == nil {
		return
	}
	l.open[[2]int{part, note}] = len(l.notes)
	l.note(part, note, vel, at, -1)
}

func (l *noteLog) off(part, note int, at int64) {
	if l == nil {
		return
	}
	key := [2]int{part, note}
	if i, ok := l.open[key]; ok {
		l.notes[i].off = at
		delete(l.open, key)
	}
}

func (l *noteLog) closeAll(at int64) {
	for key, i := range l.open {
		l.notes[i].off = at
		delete(l.open, key)
	}
}

type tempoChange struct {
	at  int64 // samples
	bpm float64
}

// ticks converts a sample time to MIDI ticks under the tempo map.
func ticks(tempos []tempoChange, at int64, rate float64) int64 {
	var t float64
	prev := tempos[0]
	for _, c := range tempos[1:] {
		if c.at >= at {
			break
		}
		t += float64(c.at-prev.at) / rate * prev.bpm / 60 * ppq
		prev = c
	}
	t += float64(at-prev.at) / rate * prev.bpm / 60 * ppq
	return int64(math.Round(t))
}

type midiEvent struct {
	tick int64
	off  bool // note-offs sort before note-ons at the same tick
	data []byte
}

// writeMIDI writes a type 1 Standard MIDI File: a tempo track, then one
// track per part.
func writeMIDI(w io.Writer, notes []midiNote, tempos []tempoChange, rate float64) error {
	bw := bufio.NewWriter(w)
	head := []byte("MThd\x00\x00\x00\x06\x00\x01")
	head = binary.BigEndian.AppendUint16(head, parts+1)
	head = binary.BigEndian.AppendUint16(head, ppq)
	bw.Write(head)

	var tempo []midiEvent
	tempo = append(tempo, midiEvent{data: meta(0x03, []byte("Chord harp"))})
	for _, c := range tempos {
		us := uint32(math.Round(60e6 / max(c.bpm, 1)))
		tempo = append(tempo, midiEvent{tick: ticks(tempos, c.at, rate), data: meta(0x51, []byte{byte(us >> 16), byte(us >> 8), byte(us)})})
	}
	writeTrack(bw, tempo)

	for part := range parts {
		spec := partSpecs[part]
		events := []midiEvent{{data: meta(0x03, []byte(spec.name))}}
		if spec.program != 0xff {
			events = append(events, midiEvent{data: []byte{0xc0 | spec.channel, spec.program}})
		}
		// Per pitch, a note ends where the next one starts, so no note-on
		// meets a note that is still sounding.
		var mine []midiNote
		for _, n := range notes {
			if n.part == part {
				mine = append(mine, n)
			}
		}
		slices.SortStableFunc(mine, func(a, b midiNote) int {
			return cmp.Or(cmp.Compare(a.note, b.note), cmp.Compare(a.on, b.on))
		})
		for i, n := range mine {
			on, off := ticks(tempos, n.on, rate), ticks(tempos, n.off, rate)
			if i+1 < len(mine) && mine[i+1].note == n.note {
				next := ticks(tempos, mine[i+1].on, rate)
				if next == on {
					continue // struck twice on one tick: keep one
				}
				off = min(off, next)
			}
			off = max(off, on+1)
			events = append(events,
				midiEvent{tick: on, data: []byte{0x90 | spec.channel, byte(n.note), byte(n.vel)}},
				midiEvent{tick: off, off: true, data: []byte{0x80 | spec.channel, byte(n.note), 0}},
			)
		}
		writeTrack(bw, events)
	}
	return bw.Flush()
}

func meta(kind byte, data []byte) []byte {
	return append(appendVLQ([]byte{0xff, kind}, uint32(len(data))), data...)
}

// writeTrack sorts events by time and writes them as an MTrk chunk.
func writeTrack(w io.Writer, events []midiEvent) {
	slices.SortStableFunc(events, func(a, b midiEvent) int {
		if c := cmp.Compare(a.tick, b.tick); c != 0 {
			return c
		}
		switch {
		case a.off && !b.off:
			return -1
		case b.off && !a.off:
			return 1
		}
		return 0
	})
	var body []byte
	var last int64
	for _, ev := range events {
		body = appendVLQ(body, uint32(ev.tick-last))
		body = append(body, ev.data...)
		last = ev.tick
	}
	body = append(body, 0x00, 0xff, 0x2f, 0x00) // end of track
	chunk := binary.BigEndian.AppendUint32([]byte("MTrk"), uint32(len(body)))
	w.Write(append(chunk, body...))
}

// appendVLQ appends v as a MIDI variable-length quantity.
func appendVLQ(b []byte, v uint32) []byte {
	var tmp [5]byte
	i := len(tmp) - 1
	tmp[i] = byte(v & 0x7f)
	for v >>= 7; v > 0; v >>= 7 {
		i--
		tmp[i] = byte(v&0x7f) | 0x80
	}
	return append(b, tmp[i:]...)
}
