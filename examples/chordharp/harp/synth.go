package harp

import (
	"math"
	"sync"
	"sync/atomic"
)

// Params are the engine's mix, rhythm and sound settings.
type Params struct {
	Master, Chord, Harp, Rhythm float64 // 0 to 1
	Sustain                     float64 // harp decay to -60 dB, seconds
	Tempo                       float64 // beats per minute
	Pattern                     int
	Playing                     bool // rhythm running
	Sound                       Sound
}

// Engine is the synthesizer: a sustained chord pad, harp plucks for the
// strumplate, and a drum machine with bass and chord accompaniment. It
// also records what is played into a Take and plays a take back. All
// methods are safe from any goroutine; Render runs on the audio thread.
type Engine struct {
	mu   sync.Mutex
	rate float64
	p    Params
	v    voicing

	chord Chord
	held  bool

	pads    []*padVoice
	plucks  []*pluck
	hits    []*hit
	pending []scheduled
	clock   int64

	stepPos float64 // samples into the current step
	step    int
	noise   uint32

	lastPluck [Zones]int64 // clock when each zone last sounded

	rec      *Take // the take being recorded
	recStart int64
	take     *Take // the last finished take
	play     *playback
	log      *noteLog // notes for a MIDI export; nil when live

	shownStep atomic.Int32 // step being played, -1 when stopped
}

type padVoice struct {
	note             int
	freq, phase, vib float64
	env              float64
	on               bool
}

type pluck struct {
	freq, phase, phase2 float64
	amp, decay          float64
	bright, brightDecay float64
	harmonics           [5]float64 // 2nd to 6th, zero above the limit
	norm                float64
	age                 int
}

type hitKind uint8

const (
	hitKick hitKind = iota
	hitSnare
	hitHat
	hitRide
	hitRim
	hitBass
	hitComp
)

type hit struct {
	kind  hitKind
	gain  float64
	freqs []float64
	phase []float64
	t     int
	last  float64 // previous noise sample, for the high-pass
}

type scheduled struct {
	at   int64
	note int
	zone int
}

const maxPlucks = 32

// harmonicLimit is the highest harmonic a voice renders, as a fraction of
// the sample rate. Anything above half the rate would alias.
const harmonicLimit = 0.45

// NewEngine returns an engine that renders at rate samples per second.
func NewEngine(rate float64) *Engine {
	e := &Engine{rate: rate, noise: 0x9e3779b9}
	e.p = Params{Master: 0.8, Chord: 0.5, Harp: 0.8, Rhythm: 0.6, Sustain: 1.5, Tempo: 112, Sound: DefaultSound()}
	e.v = e.p.Sound.voicing()
	for z := range e.lastPluck {
		e.lastPluck[z] = math.MinInt64 / 2
	}
	e.shownStep.Store(-1)
	return e
}

// Set replaces the parameters. Starting the rhythm begins at the top of
// the bar. While a take plays back, a change to the rhythm stops it; a
// change to the mix or the sound does not.
func (e *Engine) Set(p Params) {
	e.mu.Lock()
	defer e.mu.Unlock()
	p.Pattern = min(max(p.Pattern, 0), len(Patterns)-1)
	p.Sound = p.Sound.Clamp()
	if e.play != nil {
		if l := e.play.live; p.Tempo != l.tempo || p.Pattern != l.pattern || p.Playing != l.playing {
			e.stopPlayback()
		} else {
			p.Tempo, p.Pattern, p.Playing = e.p.Tempo, e.p.Pattern, e.p.Playing
		}
	}
	e.setParams(p)
}

func (e *Engine) setParams(p Params) {
	if p.Playing && (!e.p.Playing || p.Pattern != e.p.Pattern) {
		e.step, e.stepPos = 0, 0
		e.playStep(Patterns[p.Pattern], 0, p)
	}
	if !p.Playing {
		e.shownStep.Store(-1)
	}
	if p.Tempo != e.p.Tempo || p.Pattern != e.p.Pattern || p.Playing != e.p.Playing {
		e.record(Event{Kind: EvRhythm, Tempo: p.Tempo, Pattern: p.Pattern, Playing: p.Playing})
	}
	if p.Sound != e.p.Sound {
		// Retune: the held chord's pad fades and starts again in the new
		// voicing.
		e.padsOff()
		e.v = p.Sound.voicing()
	}
	e.p = p
	e.updatePads()
}

// Restart takes a running rhythm back to the top of the bar.
func (e *Engine) Restart() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.play != nil {
		e.stopPlayback()
	}
	e.restart()
}

func (e *Engine) restart() {
	if e.p.Playing {
		e.step, e.stepPos = 0, 0
		e.playStep(Patterns[e.p.Pattern], 0, e.p)
		e.record(Event{Kind: EvRestart})
	}
}

// SetChord changes the held chord. held false releases it: the pad fades
// out and the accompaniment plays drums only. While a take plays back, a
// different chord stops it.
func (e *Engine) SetChord(c Chord, held bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.play != nil {
		if c == e.play.live.chord && held == e.play.live.held {
			return
		}
		e.stopPlayback()
	}
	e.setChord(c, held)
}

func (e *Engine) setChord(c Chord, held bool) {
	if c == e.chord && held == e.held {
		return
	}
	e.chord, e.held = c, held
	e.padsOff()
	e.record(Event{Kind: EvChord, Chord: c, Held: held})
	e.updatePads()
}

// padsOff lets every pad voice fade.
func (e *Engine) padsOff() {
	for _, v := range e.pads {
		if v.on {
			v.on = false
			e.log.off(partPad, v.note, e.clock)
		}
	}
}

// updatePads starts pad voices for the held chord while the rhythm is
// stopped. With the rhythm running the accompaniment plays the chord.
func (e *Engine) updatePads() {
	want := e.held && !e.p.Playing
	active := false
	for _, v := range e.pads {
		if v.on {
			active = true
			if !want {
				v.on = false
				e.log.off(partPad, v.note, e.clock)
			}
		}
	}
	if want && !active {
		for _, n := range e.p.Sound.PadNotes(e.chord) {
			e.pads = append(e.pads, &padVoice{note: n, freq: midiFreq(n), on: true})
			e.log.on(partPad, n, 80, e.clock)
		}
	}
}

// Pluck plays strumplate zone 0 (lowest) to Zones-1 of the current chord
// after delay seconds. It stops a take that is playing back.
func (e *Engine) Pluck(zone int, delay float64) {
	if zone < 0 || zone >= Zones {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.play != nil {
		e.stopPlayback()
	}
	note := e.p.Sound.StrumNotes(e.chord)[zone]
	at := e.clock + int64(delay*e.rate)
	e.pending = append(e.pending, scheduled{at: at, note: note, zone: zone})
	// A take keeps the Classic note, so it plays back in any octave.
	e.recordAt(at, Event{Kind: EvPluck, Zone: zone, Note: note - 12*e.p.Sound.Octave})
}

// Params returns the current parameters. While a take plays back, the
// rhythm fields are the take's.
func (e *Engine) Params() Params {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.p
}

// Clock returns how many samples the engine has rendered.
func (e *Engine) Clock() int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.clock
}

// Rate returns the engine's sample rate.
func (e *Engine) Rate() float64 { return e.rate }

// Step returns the rhythm step being played, or -1 when it is stopped.
func (e *Engine) Step() int { return int(e.shownStep.Load()) }

// Voices returns how many harp notes are sounding or scheduled.
func (e *Engine) Voices() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.plucks) + len(e.pending)
}

func midiFreq(n int) float64 { return 440 * math.Pow(2, float64(n-69)/12) }

func (e *Engine) startPluck(note, zone int) {
	if len(e.plucks) >= maxPlucks {
		e.plucks = e.plucks[1:]
	}
	if zone >= 0 && zone < Zones {
		e.lastPluck[zone] = e.clock
	}
	freq := midiFreq(note)
	p := &pluck{
		freq: freq, amp: 1, decay: math.Pow(0.001, 1/(max(e.p.Sustain, 0.05)*e.rate)),
		bright: e.v.pluckBright, brightDecay: math.Pow(0.001, 1/(e.v.pluckFade*e.rate)),
		harmonics: e.v.pluck, norm: e.v.pluckNorm,
	}
	for k := range p.harmonics {
		if float64(k+2)*freq > harmonicLimit*e.rate {
			p.harmonics[k] = 0
		}
	}
	e.plucks = append(e.plucks, p)
	e.log.note(partHarp, note, 100, e.clock, e.clock+int64(max(e.p.Sustain, 0.05)*e.rate))
}

func (e *Engine) rand() float64 {
	e.noise ^= e.noise << 13
	e.noise ^= e.noise >> 17
	e.noise ^= e.noise << 5
	return float64(e.noise)/float64(math.MaxUint32)*2 - 1
}

func level(c byte) float64 {
	switch c {
	case 'x':
		return 1
	case 'o':
		return 0.45
	}
	return 0
}

func at(track string, i int) byte {
	if i < len(track) {
		return track[i]
	}
	return '.'
}

// drumNotes are the General MIDI drum notes of each hit, for export.
var drumNotes = [...]int{hitKick: 36, hitSnare: 38, hitHat: 42, hitRide: 51, hitRim: 37}

// playStep starts the hits of one step.
func (e *Engine) playStep(pat Pattern, i int, p Params) {
	e.shownStep.Store(int32(i))
	add := func(kind hitKind, gain float64, freqs ...float64) {
		if gain > 0 {
			e.hits = append(e.hits, &hit{kind: kind, gain: gain, freqs: freqs, phase: make([]float64, len(freqs))})
			if kind < hitBass {
				e.log.note(partDrums, drumNotes[kind], int(110*gain), e.clock, e.clock+int64(0.1*e.rate))
			}
		}
	}
	add(hitKick, level(at(pat.Kick, i)))
	add(hitSnare, level(at(pat.Snare, i)))
	add(hitHat, level(at(pat.Hat, i)))
	add(hitRide, level(at(pat.Ride, i)))
	add(hitRim, level(at(pat.Rim, i)))
	if !e.held {
		return
	}
	if d := at(pat.Bass, i); d != '.' {
		n := p.Sound.BassNote(e.chord, d)
		add(hitBass, 1, midiFreq(n))
		e.log.note(partBass, n, 100, e.clock, e.clock+int64(0.5*e.rate))
	}
	if g := level(at(pat.Comp, i)); g > 0 {
		notes := p.Sound.PadNotes(e.chord)
		freqs := make([]float64, len(notes))
		for k, n := range notes {
			freqs[k] = midiFreq(n + 12)
			e.log.note(partComp, n+12, int(90*g), e.clock, e.clock+int64(0.25*e.rate))
		}
		add(hitComp, g, freqs...)
	}
}

// Render fills out with mono samples in [-1, 1].
func (e *Engine) Render(out []float32) {
	e.mu.Lock()
	defer e.mu.Unlock()
	dt := 1 / e.rate
	attack := 1 - math.Exp(-dt/0.04)
	release := 1 - math.Exp(-dt/0.25)
	v := &e.v
	for s := range out {
		e.clock++
		if e.play != nil {
			e.advancePlayback()
		}
		if e.rec != nil && e.clock-e.recStart >= int64(MaxTakeSeconds*e.rate) {
			e.finishRecording()
		}
		if len(e.pending) > 0 {
			kept := e.pending[:0]
			for _, ev := range e.pending {
				if ev.at <= e.clock {
					e.startPluck(ev.note, ev.zone)
				} else {
					kept = append(kept, ev)
				}
			}
			e.pending = kept
		}
		if e.p.Playing {
			pat := Patterns[e.p.Pattern]
			perStep := e.rate * 60 / max(e.p.Tempo, 20) / float64(pat.PerBeat)
			e.stepPos++
			if e.stepPos >= perStep {
				e.stepPos -= perStep
				e.step = (e.step + 1) % pat.Steps
				e.playStep(pat, e.step, e.p)
			}
		}

		var pad float64
		for _, pv := range e.pads {
			target := 0.0
			coef := release
			if pv.on {
				target, coef = 1, attack
			}
			pv.env += (target - pv.env) * coef
			pv.vib += 2 * math.Pi * 5.2 * dt
			f := pv.freq * (1 + 0.0025*math.Sin(pv.vib))
			pv.phase += 2 * math.Pi * f * dt
			if pv.phase > 2*math.Pi {
				pv.phase -= 2 * math.Pi
			}
			ph := pv.phase
			tone := math.Sin(ph) + v.pad[0]*math.Sin(2*ph) + v.pad[1]*math.Sin(3*ph)
			if v.pad[2] != 0 {
				tone += v.pad[2] * math.Sin(4*ph)
			}
			tone += v.pad[3] * math.Sin(5*ph)
			pad += pv.env * tone * v.padNorm
		}
		if s == len(out)-1 {
			kept := e.pads[:0]
			for _, pv := range e.pads {
				if pv.on || pv.env > 1e-4 {
					kept = append(kept, pv)
				}
			}
			e.pads = kept
		}

		var harp float64
		for _, pl := range e.plucks {
			on := min(float64(pl.age)/(0.002*e.rate), 1)
			pl.age++
			pl.phase += 2 * math.Pi * pl.freq * dt
			pl.phase2 += 2 * math.Pi * pl.freq * 1.004 * dt
			if pl.phase > 2*math.Pi {
				pl.phase -= 2 * math.Pi
			}
			if pl.phase2 > 2*math.Pi {
				pl.phase2 -= 2 * math.Pi
			}
			ph := pl.phase
			var upper float64
			for k, w := range pl.harmonics {
				if w != 0 {
					upper += w * math.Sin(float64(k+2)*ph)
				}
			}
			tone := math.Sin(ph) + 0.4*math.Sin(pl.phase2) + pl.bright*upper
			harp += on * pl.amp * tone * pl.norm
			pl.amp *= pl.decay
			pl.bright *= pl.brightDecay
		}
		if s == len(out)-1 {
			kept := e.plucks[:0]
			for _, pl := range e.plucks {
				if pl.amp > 1e-4 {
					kept = append(kept, pl)
				}
			}
			e.plucks = kept
		}

		var drums, accomp float64
		for _, h := range e.hits {
			d, a := e.voice(h, dt)
			drums += d
			accomp += a
			h.t++
		}
		if s == len(out)-1 {
			kept := e.hits[:0]
			for _, h := range e.hits {
				if float64(h.t) < h.life()*e.rate {
					kept = append(kept, h)
				}
			}
			e.hits = kept
		}

		mix := 0.12*pad*e.p.Chord + 0.16*accomp*e.p.Chord + 0.14*harp*e.p.Harp + 0.5*drums*e.p.Rhythm
		out[s] = float32(math.Tanh(mix * e.p.Master))
	}
}

// life is how long a hit sounds, in seconds, before it is dropped: long
// enough that its envelope is below -45 dB.
func (h *hit) life() float64 {
	if h.kind == hitBass {
		return 2.5
	}
	return 1.2
}

// voice renders one sample of a hit, split into drums and accompaniment.
func (e *Engine) voice(h *hit, dt float64) (drums, accomp float64) {
	t := float64(h.t) * dt
	switch h.kind {
	case hitKick:
		f := 48 + 110*math.Exp(-t/0.035)
		h.last += 2 * math.Pi * f * dt
		return h.gain * math.Sin(h.last) * math.Exp(-t/0.2), 0
	case hitSnare:
		tone := math.Sin(2*math.Pi*185*t) * math.Exp(-t/0.04)
		return h.gain * (0.55*e.rand()*math.Exp(-t/0.08) + 0.35*tone), 0
	case hitHat, hitRide:
		n := e.rand()
		hp := n - h.last
		h.last = n
		tail := 0.018
		gain := 0.22
		if h.kind == hitRide {
			tail, gain = 0.2, 0.13
		}
		return h.gain * gain * hp * math.Exp(-t/tail), 0
	case hitRim:
		return h.gain * 0.35 * math.Sin(2*math.Pi*1750*t) * math.Exp(-t/0.012), 0
	case hitBass:
		h.phase[0] += 2 * math.Pi * h.freqs[0] * dt
		on := min(t/0.004, 1)
		ph := h.phase[0]
		return 0, h.gain * on * 1.3 * (math.Sin(ph) + 0.3*math.Sin(2*ph)) * math.Exp(-t/0.45)
	case hitComp:
		var sum float64
		for i, f := range h.freqs {
			h.phase[i] += 2 * math.Pi * f * dt
			ph := h.phase[i]
			sum += math.Sin(ph) + e.v.comp[0]*math.Sin(2*ph) + e.v.comp[1]*math.Sin(3*ph)
		}
		on := min(t/0.005, 1)
		return 0, h.gain * on * 0.55 * sum * e.v.compNorm * math.Exp(-t/0.16)
	}
	return 0, 0
}
