// Package chordharp is the gomobile bind surface of the chord harp app.
// The native host calls Start, Stop, Root, Render, Dispatch and Command.
// The instrument view calls the rest from its touch handlers, its audio
// thread and its export worker, so playing never waits on a screen render.
package chordharp

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/DonaldMurillo/gofastr-native/examples/chordharp/harp"
	"github.com/DonaldMurillo/gofastr-native/nativeui"

	"github.com/DonaldMurillo/gofastr-native/examples/chordharp/screens"
)

var host struct {
	sync.RWMutex
	app     *nativeui.App
	runtime *nativeui.Runtime
}

// Start builds the app. The sound settings are kept in dataDir.
func Start(dataDir string) error {
	host.Lock()
	defer host.Unlock()
	if host.app != nil {
		return errors.New("chordharp: already started")
	}
	app, err := screens.New()
	if err != nil {
		return err
	}
	runtime, err := nativeui.NewRuntime(app, nativeui.RuntimeOptions{})
	if err != nil {
		return err
	}
	loadSettings(dataDir)
	host.app, host.runtime = app, runtime
	return nil
}

// Stop releases the app.
func Stop() error {
	host.Lock()
	defer host.Unlock()
	host.app, host.runtime = nil, nil
	return nil
}

// Root is the first route the renderer opens.
func Root() string { return screens.Root }

// Render returns one screen as JSON.
func Render(route, values string) (string, error) {
	host.RLock()
	defer host.RUnlock()
	if host.app == nil {
		return "", errors.New("chordharp: not started")
	}
	return host.app.RenderJSON(context.Background(), route, values)
}

// Dispatch runs a screen action and returns its result as JSON.
func Dispatch(route, action, values string) (string, error) {
	host.RLock()
	defer host.RUnlock()
	if host.app == nil {
		return "", errors.New("chordharp: not started")
	}
	return host.app.DispatchJSON(context.Background(), route, action, values)
}

// Command runs the renderer's editor and draft protocol.
func Command(payload string) (string, error) {
	host.RLock()
	defer host.RUnlock()
	if host.runtime == nil {
		return "", errors.New("chordharp: not started")
	}
	return host.runtime.CommandJSON(context.Background(), payload)
}

// The instrument's state outlives the view, so a rebuilt view picks up
// the held chord, the running rhythm and the last take.
var play = struct {
	sync.Mutex
	eng      *harp.Engine
	params   harp.Params
	chord    harp.Chord
	column   int
	held     bool
	latch    bool
	take     *harp.Take
	settings string // the settings file; empty when there is nowhere to keep it
}{params: harp.NewEngine(44100).Params(), chord: harp.Chord{Root: 0, Kind: harp.Major}, column: 3}

const (
	minTempo, maxTempo = 50, 200
	toneStep           = 10
)

// loadSettings reads the saved sound from dataDir.
func loadSettings(dataDir string) {
	play.Lock()
	defer play.Unlock()
	play.settings = ""
	if dataDir == "" {
		return
	}
	play.settings = filepath.Join(dataDir, "chordharp", "settings.json")
	sound, _ := harp.LoadSound(play.settings)
	setParams(func(p *harp.Params) { p.Sound = sound })
}

// Open starts an engine at the device's output rate, keeping the mix,
// rhythm, chord and take of any earlier one. A recording in progress
// ends there, and playback stops.
func Open(rate int) {
	play.Lock()
	defer play.Unlock()
	keepTake()
	eng := harp.NewEngine(float64(min(max(rate, 8000), 192000)))
	eng.Set(play.params)
	eng.SetChord(play.chord, play.held)
	eng.LoadTake(play.take)
	play.eng = eng
}

// keepTake ends a recording on the current engine and keeps its take.
func keepTake() {
	if play.eng == nil {
		return
	}
	play.eng.StopRecording()
	if t := play.eng.Take(); t != nil {
		play.take = t
	}
}

// Fill renders frames of mono 16-bit little-endian PCM, silence before
// Open.
func Fill(frames int) []byte {
	play.Lock()
	eng := play.eng
	play.Unlock()
	frames = min(max(frames, 0), 8192)
	out := make([]byte, frames*2)
	if eng == nil {
		return out
	}
	samples := make([]float32, frames)
	eng.Render(samples)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(int16(math.Round(float64(s)*32767))))
	}
	return out
}

// Hold sounds the chord at a grid cell: kind is the row (0 major, 1 minor,
// 2 seventh) and column the root, flat side first.
func Hold(kind, column int) {
	if kind < 0 || kind >= harp.Kinds || column < 0 || column >= len(harp.Roots) {
		return
	}
	play.Lock()
	defer play.Unlock()
	play.chord = harp.Chord{Root: harp.Roots[column], Kind: harp.Kind(kind)}
	play.column, play.held = column, true
	if play.eng != nil {
		play.eng.SetChord(play.chord, true)
	}
}

// Release lets the chord go. The strumplate keeps its notes.
func Release() {
	play.Lock()
	defer play.Unlock()
	play.held = false
	if play.eng != nil {
		play.eng.SetChord(play.chord, false)
	}
}

// SetLatch records whether a tapped chord stays held after the finger
// lifts. The view applies it; Go keeps it so a rebuilt view agrees.
func SetLatch(on bool) {
	play.Lock()
	defer play.Unlock()
	play.latch = on
}

// Pluck plays strumplate zone 0 (lowest) to 11 after delay seconds.
func Pluck(zone int, delay float64) {
	if zone < 0 || zone >= harp.Zones {
		return
	}
	play.Lock()
	defer play.Unlock()
	if play.eng != nil {
		play.eng.Pluck(zone, min(max(delay, 0), 1))
	}
}

func setParams(change func(*harp.Params)) {
	change(&play.params)
	if play.eng != nil {
		play.eng.Set(play.params)
	}
}

// Toggle starts or stops the rhythm.
func Toggle() {
	play.Lock()
	defer play.Unlock()
	setParams(func(p *harp.Params) { p.Playing = !p.Playing })
}

// NextPattern moves through the rhythms by delta, wrapping.
func NextPattern(delta int) {
	play.Lock()
	defer play.Unlock()
	n := len(harp.Patterns)
	setParams(func(p *harp.Params) { p.Pattern = ((p.Pattern+delta)%n + n) % n })
}

// NudgeTempo changes the tempo by delta beats per minute, within 50 to 200.
func NudgeTempo(delta int) {
	play.Lock()
	defer play.Unlock()
	setParams(func(p *harp.Params) { p.Tempo = min(max(p.Tempo+float64(delta), minTempo), maxTempo) })
}

// SetPreset chooses a sound from harp.Presets by index.
func SetPreset(index int) {
	if index < 0 || index >= len(harp.Presets) {
		return
	}
	changeSound(func(s *harp.Sound) { *s = harp.Presets[index].Sound })
}

// NudgeOctave moves the sound up or down by delta octaves.
func NudgeOctave(delta int) { changeSound(func(s *harp.Sound) { s.Octave += delta }) }

// NudgeTone makes the sound brighter (positive delta) or darker, in steps
// of 10%.
func NudgeTone(delta int) { changeSound(func(s *harp.Sound) { s.Tone += delta * toneStep }) }

func changeSound(change func(*harp.Sound)) {
	play.Lock()
	defer play.Unlock()
	setParams(func(p *harp.Params) {
		change(&p.Sound)
		p.Sound = p.Sound.Clamp()
	})
	if play.settings != "" {
		harp.SaveSound(play.settings, play.params.Sound)
	}
}

// Record starts recording a take, or stops the one being recorded.
func Record() {
	play.Lock()
	defer play.Unlock()
	if play.eng == nil {
		return
	}
	if play.eng.Status().Recording {
		keepTake()
		return
	}
	play.eng.Record()
}

// PlayTake plays the last take back, or stops it playing.
func PlayTake() {
	play.Lock()
	defer play.Unlock()
	if play.eng == nil {
		return
	}
	if play.eng.Status().Playback {
		play.eng.StopPlaying()
		return
	}
	keepTake()
	play.eng.Play()
}

// Export writes the last take to midPath as MIDI and to wavPath as WAV,
// with the current mix and sound. A long take takes seconds, so the view
// calls this off the UI thread.
func Export(midPath, wavPath string) error {
	play.Lock()
	keepTake()
	t, params := play.take, play.params
	play.Unlock()
	if t == nil {
		return errors.New("chordharp: nothing recorded")
	}
	mid, err := os.Create(midPath)
	if err != nil {
		return err
	}
	defer mid.Close()
	wav, err := os.Create(wavPath)
	if err != nil {
		return err
	}
	defer wav.Close()
	if err := t.Export(params, wav, mid); err != nil {
		return err
	}
	if err := mid.Close(); err != nil {
		return err
	}
	return wav.Close()
}

// Step returns the rhythm step being played, or -1 when it is stopped.
func Step() int {
	play.Lock()
	eng := play.eng
	play.Unlock()
	if eng == nil {
		return -1
	}
	return eng.Step()
}

type state struct {
	Held    bool     `json:"held"`
	Kind    int      `json:"kind"`
	Column  int      `json:"column"`
	Latch   bool     `json:"latch"`
	Chord   string   `json:"chord"`
	Short   string   `json:"short"`
	Tones   string   `json:"tones"`
	Zones   []string `json:"zones"`
	Playing bool     `json:"playing"`
	Pattern int      `json:"pattern"`
	Tempo   int      `json:"tempo"`
	Steps   int      `json:"steps"`
	PerBeat int      `json:"perBeat"`

	Sound  string `json:"sound"`  // the preset's name, or Custom
	Preset int    `json:"preset"` // index in the payload's presets, -1 for Custom
	Octave int    `json:"octave"`
	Tone   int    `json:"tone"`

	Recording bool      `json:"recording"`
	Playback  bool      `json:"playback"` // a take is playing back
	Position  float64   `json:"position"` // seconds recorded or played
	Length    float64   `json:"length"`   // the take's length, 0 without one
	HasTake   bool      `json:"hasTake"`
	Since     []float64 `json:"since,omitempty"` // during playback, seconds since each zone sounded
}

// State returns what the view draws, as JSON. While a take plays back,
// the chord and the rhythm are the take's.
func State() string {
	play.Lock()
	defer play.Unlock()
	chord, held, params, column := play.chord, play.held, play.params, play.column
	s := state{Latch: play.latch, HasTake: play.take != nil}
	if play.take != nil {
		s.Length = play.take.Seconds()
	}
	if play.eng != nil {
		st := play.eng.Status()
		s.Recording, s.Playback, s.Position = st.Recording, st.Playback, st.Position
		if st.HasTake {
			s.HasTake, s.Length = true, st.Length
		}
		if st.Playback {
			chord, held = st.Chord, st.Held
			params.Tempo, params.Pattern, params.Playing = st.Tempo, st.Pattern, st.Rhythm
			column = slices.Index(harp.Roots[:], chord.Root)
			for _, since := range st.Since {
				s.Since = append(s.Since, min(since, 60))
			}
		}
	}
	pattern := harp.Patterns[params.Pattern]
	s.Held, s.Kind, s.Column = held, int(chord.Kind), column
	s.Chord, s.Short, s.Tones = chord.Long(), chord.String(), chord.Tones()
	s.Playing, s.Pattern, s.Tempo = params.Playing, params.Pattern, int(math.Round(params.Tempo))
	s.Steps, s.PerBeat = pattern.Steps, pattern.PerBeat
	s.Sound, s.Preset, s.Octave, s.Tone = params.Sound.Name(), params.Sound.Preset(), params.Sound.Octave, params.Sound.Tone
	for _, n := range params.Sound.StrumNotes(chord) {
		s.Zones = append(s.Zones, harp.NoteName(n))
	}
	out, _ := json.Marshal(s)
	return string(out)
}
