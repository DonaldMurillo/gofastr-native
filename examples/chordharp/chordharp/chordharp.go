// Package chordharp is the gomobile bind surface of the chord harp app.
// The native host calls Start, Stop, Root, Render, Dispatch and Command.
// The instrument view calls the rest from its touch handlers and audio
// thread, so playing never waits on a screen render.
package chordharp

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
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

// Start builds the app. dataDir is unused: the instrument keeps nothing.
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
// the held chord and the running rhythm.
var play = struct {
	sync.Mutex
	eng    *harp.Engine
	params harp.Params
	chord  harp.Chord
	column int
	held   bool
	latch  bool
}{params: harp.NewEngine(44100).Params(), chord: harp.Chord{Root: 0, Kind: harp.Major}, column: 3}

const (
	minTempo, maxTempo = 50, 200
)

// Open starts an engine at the device's output rate, keeping the mix,
// rhythm and chord of any earlier one.
func Open(rate int) {
	play.Lock()
	defer play.Unlock()
	eng := harp.NewEngine(float64(min(max(rate, 8000), 192000)))
	eng.Set(play.params)
	eng.SetChord(play.chord, play.held)
	play.eng = eng
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
		play.eng.Pluck(harp.StrumNotes(play.chord)[zone], min(max(delay, 0), 1))
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
}

// State returns what the view draws, as JSON.
func State() string {
	play.Lock()
	defer play.Unlock()
	pattern := harp.Patterns[play.params.Pattern]
	s := state{
		Held: play.held, Kind: int(play.chord.Kind), Column: play.column, Latch: play.latch,
		Chord: play.chord.Long(), Short: play.chord.String(), Tones: play.chord.Tones(),
		Playing: play.params.Playing, Pattern: play.params.Pattern, Tempo: int(math.Round(play.params.Tempo)),
		Steps: pattern.Steps, PerBeat: pattern.PerBeat,
	}
	for _, n := range harp.StrumNotes(play.chord) {
		s.Zones = append(s.Zones, harp.NoteName(n))
	}
	out, _ := json.Marshal(s)
	return string(out)
}
