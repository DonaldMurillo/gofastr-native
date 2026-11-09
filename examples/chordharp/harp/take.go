package harp

import (
	"bufio"
	"cmp"
	"encoding/binary"
	"io"
	"math"
	"slices"
)

// MaxTakeSeconds is the longest take. Recording stops itself there. A take
// this long exports as about 26 MB of WAV.
const MaxTakeSeconds = 5 * 60

// EventKind is what an Event changed.
type EventKind uint8

const (
	EvChord   EventKind = iota // Chord and Held
	EvPluck                    // Zone and Note
	EvRhythm                   // Tempo, Pattern and Playing
	EvRestart                  // the rhythm went back to beat 1
)

// Event is one input recorded in a take.
type Event struct {
	At      int64 // samples from the take's start, at the take's rate
	Kind    EventKind
	Chord   Chord
	Held    bool
	Zone    int
	Note    int // the plucked note in Classic's octave
	Tempo   float64
	Pattern int
	Playing bool
}

// TakeStart is the instrument's state when recording began.
type TakeStart struct {
	Chord   Chord
	Held    bool
	Tempo   float64
	Pattern int
	Playing bool    // rhythm running
	Step    int     // the rhythm's step, when it was running
	StepPos float64 // samples into that step
}

// Take is a recorded performance: what was played, not how it was mixed.
// Playback and export use the mix, sustain and sound they are given.
type Take struct {
	Rate   float64
	Length int64 // samples
	Start  TakeStart
	Events []Event
}

// Seconds is the take's length.
func (t *Take) Seconds() float64 { return float64(t.Length) / t.Rate }

// liveState is what a take's playback replaces, restored when it ends.
type liveState struct {
	chord   Chord
	held    bool
	tempo   float64
	pattern int
	playing bool
}

type playback struct {
	take  *Take
	start int64
	next  int
	live  liveState
}

// Record starts recording a new take. It stops a take that is playing
// back.
func (e *Engine) Record() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.play != nil {
		e.stopPlayback()
	}
	t := &Take{Rate: e.rate, Start: TakeStart{
		Chord: e.chord, Held: e.held,
		Tempo: e.p.Tempo, Pattern: e.p.Pattern, Playing: e.p.Playing,
	}}
	if e.p.Playing {
		t.Start.Step, t.Start.StepPos = e.step, e.stepPos
	}
	e.rec, e.recStart = t, e.clock
}

// StopRecording ends the take being recorded, which becomes the engine's
// take.
func (e *Engine) StopRecording() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.rec != nil {
		e.finishRecording()
	}
}

func (e *Engine) finishRecording() {
	t := e.rec
	e.rec = nil
	t.Length = e.clock - e.recStart
	// A delayed pluck is stamped when it sounds, so events can be out of
	// order, and one can land after the end.
	slices.SortStableFunc(t.Events, func(a, b Event) int { return cmp.Compare(a.At, b.At) })
	for len(t.Events) > 0 && t.Events[len(t.Events)-1].At > t.Length {
		t.Events = t.Events[:len(t.Events)-1]
	}
	e.take = t
}

func (e *Engine) record(ev Event) { e.recordAt(e.clock, ev) }

func (e *Engine) recordAt(at int64, ev Event) {
	if e.rec == nil {
		return
	}
	ev.At = at - e.recStart
	e.rec.Events = append(e.rec.Events, ev)
}

// Take returns the last finished take, or nil.
func (e *Engine) Take() *Take {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.take
}

// LoadTake makes t the engine's take, as when a new engine replaces one
// that recorded it.
func (e *Engine) LoadTake(t *Take) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.take = t
}

// Play plays the take back from the start, ending a recording first. It
// reports false when there is no take.
func (e *Engine) Play() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.rec != nil {
		e.finishRecording()
	}
	if e.take == nil {
		return false
	}
	if e.play != nil {
		e.stopPlayback()
	}
	t := e.take
	e.play = &playback{take: t, start: e.clock, live: liveState{
		chord: e.chord, held: e.held, tempo: e.p.Tempo, pattern: e.p.Pattern, playing: e.p.Playing,
	}}
	s := t.Start
	e.p.Tempo, e.p.Pattern, e.p.Playing = s.Tempo, min(max(s.Pattern, 0), len(Patterns)-1), s.Playing
	if s.Playing {
		e.step = s.Step % Patterns[e.p.Pattern].Steps
		e.stepPos = s.StepPos * e.rate / t.Rate
		e.shownStep.Store(int32(e.step))
	} else {
		e.shownStep.Store(-1)
	}
	e.setChord(s.Chord, s.Held)
	e.updatePads()
	return true
}

// StopPlaying ends playback and puts back what was playing before it.
func (e *Engine) StopPlaying() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.play != nil {
		e.stopPlayback()
	}
}

func (e *Engine) stopPlayback() {
	live := e.play.live
	e.play = nil
	e.setChord(live.chord, live.held)
	p := e.p
	p.Tempo, p.Pattern, p.Playing = live.tempo, live.pattern, live.playing
	e.setParams(p)
}

// at converts a time in the take to this engine's clock.
func (pb *playback) at(samples int64, rate float64) int64 {
	if pb.take.Rate == rate {
		return pb.start + samples
	}
	return pb.start + int64(math.Round(float64(samples)*rate/pb.take.Rate))
}

// advancePlayback applies the events due at the current sample, and ends
// playback after the last one.
func (e *Engine) advancePlayback() {
	pb := e.play
	for pb.next < len(pb.take.Events) {
		ev := pb.take.Events[pb.next]
		if pb.at(ev.At, e.rate) > e.clock {
			break
		}
		pb.next++
		switch ev.Kind {
		case EvChord:
			e.setChord(ev.Chord, ev.Held)
		case EvPluck:
			e.startPluck(ev.Note+12*e.p.Sound.Octave, ev.Zone)
		case EvRhythm:
			p := e.p
			p.Tempo, p.Pattern, p.Playing = ev.Tempo, min(max(ev.Pattern, 0), len(Patterns)-1), ev.Playing
			e.setParams(p)
		case EvRestart:
			e.restart()
		}
	}
	if e.clock >= pb.at(pb.take.Length, e.rate) {
		e.stopPlayback()
	}
}

// Status is what a screen draws of the engine. While a take plays back,
// the chord and the rhythm are the take's.
type Status struct {
	Chord   Chord
	Held    bool
	Tempo   float64
	Pattern int
	Rhythm  bool // the rhythm is running

	Recording bool
	Playback  bool    // a take is playing back
	Position  float64 // seconds recorded so far, or played back
	Length    float64 // the take's length in seconds, 0 without one
	HasTake   bool

	// Since is how many seconds ago each strumplate zone sounded, +Inf if
	// it has not.
	Since [Zones]float64
}

// Status returns the engine's state for a screen.
func (e *Engine) Status() Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := Status{
		Chord: e.chord, Held: e.held, Tempo: e.p.Tempo, Pattern: e.p.Pattern, Rhythm: e.p.Playing,
		Recording: e.rec != nil, Playback: e.play != nil, HasTake: e.take != nil,
	}
	switch {
	case e.rec != nil:
		s.Position = float64(e.clock-e.recStart) / e.rate
	case e.play != nil:
		s.Position = float64(e.clock-e.play.start) / e.rate
	}
	if e.play != nil {
		s.Length = e.play.take.Seconds()
	} else if e.take != nil {
		s.Length = e.take.Seconds()
	}
	for z, at := range e.lastPluck {
		s.Since[z] = math.Inf(1)
		if at >= 0 && at <= e.clock {
			s.Since[z] = float64(e.clock-at) / e.rate
		}
	}
	return s
}

// ExportRate is the sample rate of exported audio.
const ExportRate = 44100

// exportTail is how long an export runs past the take's end, so the last
// notes ring out.
const exportTail = 3

// Export renders the take through a fresh engine with p's mix, sustain and
// sound, and writes it as a 16-bit mono WAV to wav and as a Standard MIDI
// File to mid. Either writer may be nil.
func (t *Take) Export(p Params, wav, mid io.Writer) error {
	e := NewEngine(ExportRate)
	idle := p
	idle.Playing = false
	e.Set(idle)
	if mid != nil {
		e.log = &noteLog{open: map[[2]int]int{}}
	}
	e.LoadTake(t)
	e.Play()
	frames := int64(math.Round(t.Seconds()*ExportRate)) + exportTail*ExportRate

	var w *bufio.Writer
	if wav != nil {
		w = bufio.NewWriter(wav)
		if err := writeWAVHeader(w, frames); err != nil {
			return err
		}
	}
	buf := make([]float32, 4096)
	pcm := make([]byte, len(buf)*2)
	for done := int64(0); done < frames; {
		n := int(min(int64(len(buf)), frames-done))
		e.Render(buf[:n])
		done += int64(n)
		if w == nil {
			continue
		}
		for i, s := range buf[:n] {
			binary.LittleEndian.PutUint16(pcm[i*2:], uint16(int16(math.Round(float64(s)*32767))))
		}
		if _, err := w.Write(pcm[:n*2]); err != nil {
			return err
		}
	}
	if w != nil {
		if err := w.Flush(); err != nil {
			return err
		}
	}
	if mid == nil {
		return nil
	}
	e.log.closeAll(e.clock)
	return writeMIDI(mid, e.log.notes, t.tempoMap(ExportRate), ExportRate)
}

// tempoMap lists the take's tempo changes on a clock of the given rate.
func (t *Take) tempoMap(rate float64) []tempoChange {
	scale := rate / t.Rate
	out := []tempoChange{{at: 0, bpm: t.Start.Tempo}}
	for _, ev := range t.Events {
		if ev.Kind == EvRhythm && ev.Tempo != out[len(out)-1].bpm {
			out = append(out, tempoChange{at: int64(math.Round(float64(ev.At) * scale)), bpm: ev.Tempo})
		}
	}
	return out
}

func writeWAVHeader(w io.Writer, frames int64) error {
	data := uint32(frames * 2)
	var h [44]byte
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], 36+data)
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1) // PCM
	binary.LittleEndian.PutUint16(h[22:], 1) // mono
	binary.LittleEndian.PutUint32(h[24:], ExportRate)
	binary.LittleEndian.PutUint32(h[28:], ExportRate*2)
	binary.LittleEndian.PutUint16(h[32:], 2)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], data)
	_, err := w.Write(h[:])
	return err
}
