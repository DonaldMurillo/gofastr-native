package harp

import (
	"math"
	"sync"
	"sync/atomic"
)

// Params are the engine's mix and rhythm settings.
type Params struct {
	Master, Chord, Harp, Rhythm float64 // 0 to 1
	Sustain                     float64 // harp decay to -60 dB, seconds
	Tempo                       float64 // beats per minute
	Pattern                     int
	Playing                     bool // rhythm running
}

// Engine is the synthesizer: a sustained chord pad, harp plucks for the
// strumplate, and a drum machine with bass and chord accompaniment. All
// methods are safe from any goroutine; Render runs on the audio thread.
type Engine struct {
	mu   sync.Mutex
	rate float64
	p    Params

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

	shownStep atomic.Int32 // step being played, -1 when stopped
}

type padVoice struct {
	freq, phase, vib float64
	env              float64
	on               bool
}

type pluck struct {
	freq, phase, phase2 float64
	amp, decay          float64
	bright, brightDecay float64
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
}

const maxPlucks = 32

// NewEngine returns an engine that renders at rate samples per second.
func NewEngine(rate float64) *Engine {
	e := &Engine{rate: rate, noise: 0x9e3779b9}
	e.p = Params{Master: 0.8, Chord: 0.5, Harp: 0.8, Rhythm: 0.6, Sustain: 1.5, Tempo: 112}
	e.shownStep.Store(-1)
	return e
}

// Set replaces the parameters. Starting the rhythm begins at the top of
// the bar.
func (e *Engine) Set(p Params) {
	e.mu.Lock()
	defer e.mu.Unlock()
	p.Pattern = min(max(p.Pattern, 0), len(Patterns)-1)
	if p.Playing && (!e.p.Playing || p.Pattern != e.p.Pattern) {
		e.step, e.stepPos = 0, 0
		e.playStep(Patterns[p.Pattern], 0, p)
	}
	if !p.Playing {
		e.shownStep.Store(-1)
	}
	e.p = p
	e.updatePads()
}

// Restart takes a running rhythm back to the top of the bar.
func (e *Engine) Restart() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.p.Playing {
		e.step, e.stepPos = 0, 0
		e.playStep(Patterns[e.p.Pattern], 0, e.p)
	}
}

// SetChord changes the held chord. held false releases it: the pad fades
// out and the accompaniment plays drums only.
func (e *Engine) SetChord(c Chord, held bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if c == e.chord && held == e.held {
		return
	}
	e.chord, e.held = c, held
	for _, v := range e.pads {
		v.on = false
	}
	e.updatePads()
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
			}
		}
	}
	if want && !active {
		for _, n := range PadNotes(e.chord) {
			e.pads = append(e.pads, &padVoice{freq: midiFreq(n), on: true})
		}
	}
}

// Pluck plays a harp note after delay seconds.
func (e *Engine) Pluck(note int, delay float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pending = append(e.pending, scheduled{at: e.clock + int64(delay*e.rate), note: note})
}

// Params returns the current parameters.
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

// Step returns the rhythm step being played, or -1 when it is stopped.
func (e *Engine) Step() int { return int(e.shownStep.Load()) }

// Voices returns how many harp notes are sounding or scheduled.
func (e *Engine) Voices() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.plucks) + len(e.pending)
}

func midiFreq(n int) float64 { return 440 * math.Pow(2, float64(n-69)/12) }

func (e *Engine) startPluck(note int) {
	if len(e.plucks) >= maxPlucks {
		e.plucks = e.plucks[1:]
	}
	decay := math.Pow(0.001, 1/(max(e.p.Sustain, 0.05)*e.rate))
	e.plucks = append(e.plucks, &pluck{
		freq: midiFreq(note), amp: 1, decay: decay,
		bright: 1, brightDecay: math.Pow(0.001, 1/(0.25*e.rate)),
	})
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

// playStep starts the hits of one step.
func (e *Engine) playStep(pat Pattern, i int, p Params) {
	e.shownStep.Store(int32(i))
	add := func(kind hitKind, gain float64, freqs ...float64) {
		if gain > 0 {
			e.hits = append(e.hits, &hit{kind: kind, gain: gain, freqs: freqs, phase: make([]float64, len(freqs))})
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
		add(hitBass, 1, midiFreq(BassNote(e.chord, d)))
	}
	if g := level(at(pat.Comp, i)); g > 0 {
		notes := PadNotes(e.chord)
		freqs := make([]float64, len(notes))
		for k, n := range notes {
			freqs[k] = midiFreq(n + 12)
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
	for s := range out {
		e.clock++
		if len(e.pending) > 0 {
			kept := e.pending[:0]
			for _, ev := range e.pending {
				if ev.at <= e.clock {
					e.startPluck(ev.note)
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
		for _, v := range e.pads {
			target := 0.0
			coef := release
			if v.on {
				target, coef = 1, attack
			}
			v.env += (target - v.env) * coef
			v.vib += 2 * math.Pi * 5.2 * dt
			f := v.freq * (1 + 0.0025*math.Sin(v.vib))
			v.phase += 2 * math.Pi * f * dt
			if v.phase > 2*math.Pi {
				v.phase -= 2 * math.Pi
			}
			ph := v.phase
			pad += v.env * (math.Sin(ph) + 0.35*math.Sin(2*ph) + 0.18*math.Sin(3*ph) + 0.08*math.Sin(5*ph))
		}
		if s == len(out)-1 {
			kept := e.pads[:0]
			for _, v := range e.pads {
				if v.on || v.env > 1e-4 {
					kept = append(kept, v)
				}
			}
			e.pads = kept
		}

		var harp float64
		for _, v := range e.plucks {
			on := min(float64(v.age)/(0.002*e.rate), 1)
			v.age++
			v.phase += 2 * math.Pi * v.freq * dt
			v.phase2 += 2 * math.Pi * v.freq * 1.004 * dt
			if v.phase > 2*math.Pi {
				v.phase -= 2 * math.Pi
			}
			if v.phase2 > 2*math.Pi {
				v.phase2 -= 2 * math.Pi
			}
			ph := v.phase
			tone := math.Sin(ph) + 0.4*math.Sin(v.phase2) +
				v.bright*(0.5*math.Sin(2*ph)+0.3*math.Sin(3*ph)+0.15*math.Sin(4*ph))
			harp += on * v.amp * tone
			v.amp *= v.decay
			v.bright *= v.brightDecay
		}
		if s == len(out)-1 {
			kept := e.plucks[:0]
			for _, v := range e.plucks {
				if v.amp > 1e-4 {
					kept = append(kept, v)
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
			sum += math.Sin(ph) + 0.3*math.Sin(2*ph) + 0.12*math.Sin(3*ph)
		}
		on := min(t/0.005, 1)
		return 0, h.gain * on * 0.55 * sum * math.Exp(-t/0.16)
	}
	return 0, 0
}
