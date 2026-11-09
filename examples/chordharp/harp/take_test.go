package harp

import (
	"bytes"
	"encoding/binary"
	"math"
	"slices"
	"testing"
)

// logged attaches a note log to an engine, as an export does.
func logged(e *Engine) *noteLog {
	e.log = &noteLog{open: map[[2]int]int{}}
	return e.log
}

// since lists a log's notes as part, note and start, relative to from.
func since(l *noteLog, from int64) [][3]int64 {
	var out [][3]int64
	for _, n := range l.notes {
		if n.on >= from {
			out = append(out, [3]int64{int64(n.part), int64(n.note), n.on - from})
		}
	}
	slices.SortFunc(out, func(a, b [3]int64) int {
		for i := range a {
			if a[i] != b[i] {
				return int(a[i] - b[i])
			}
		}
		return 0
	})
	return out
}

// perform records a session that starts with the rhythm mid-bar and uses
// every kind of input. It returns the engine and when recording began.
func perform(t *testing.T) (*Engine, *noteLog, int64) {
	t.Helper()
	e := classicEngine()
	p := e.Params()
	p.Tempo, p.Pattern, p.Playing = 132, 0, true
	e.Set(p)
	e.SetChord(Chord{Root: 0, Kind: Major}, true)
	render(e, 0.37)
	l := logged(e)
	start := e.Clock()
	e.Record()
	render(e, 0.4)
	e.Pluck(2, 0)
	e.Pluck(3, 0.05)
	render(e, 0.3)
	e.SetChord(Chord{Root: 9, Kind: Minor}, true)
	render(e, 0.5)
	p.Pattern = 5
	e.Set(p)
	render(e, 0.6)
	e.Restart()
	render(e, 0.3)
	p.Playing = false
	e.Set(p)
	e.SetChord(Chord{Root: 9, Kind: Minor}, false)
	render(e, 0.2)
	e.StopRecording()
	return e, l, start
}

func TestTakeRecordsEveryInput(t *testing.T) {
	e, _, _ := perform(t)
	take := e.Take()
	if take == nil {
		t.Fatal("no take")
	}
	if got := take.Seconds(); math.Abs(got-2.3) > 0.02 {
		t.Fatalf("take is %.3f s, want 2.3", got)
	}
	if !take.Start.Playing || take.Start.Step == 0 && take.Start.StepPos == 0 {
		t.Fatalf("take start %+v does not keep the rhythm's place", take.Start)
	}
	var kinds []EventKind
	for i, ev := range take.Events {
		kinds = append(kinds, ev.Kind)
		if i > 0 && ev.At < take.Events[i-1].At {
			t.Fatalf("events out of order: %+v", take.Events)
		}
	}
	want := []EventKind{EvPluck, EvPluck, EvChord, EvRhythm, EvRestart, EvRhythm, EvChord}
	if !slices.Equal(kinds, want) {
		t.Fatalf("event kinds %v, want %v", kinds, want)
	}
	if d := take.Events[1].At - take.Events[0].At; d != int64(0.05*rate) {
		t.Fatalf("delayed pluck stamped %d samples after the first, want %d", d, int64(0.05*rate))
	}
}

func TestPlaybackRepeatsTheTake(t *testing.T) {
	rec, recLog, recStart := perform(t)
	want := since(recLog, recStart)

	// A fresh engine at another rate, as on a phone after the audio
	// restarts, plays the same notes at the same times.
	for _, r := range []float64{rate, 48000} {
		e := NewEngine(r)
		p := e.Params()
		p.Sound = Classic
		e.Set(p)
		e.LoadTake(rec.Take())
		l := logged(e)
		start := e.Clock()
		if !e.Play() {
			t.Fatal("Play found no take")
		}
		s := make([]float32, int(2.6*r))
		for i := 0; i < len(s); i += 256 {
			e.Render(s[i:min(i+256, len(s))])
		}
		got := since(l, start)
		if len(got) != len(want) {
			t.Fatalf("rate %v: playback played %d notes, the take %d", r, len(got), len(want))
		}
		for i := range got {
			at := float64(got[i][2]) / r
			wantAt := float64(want[i][2]) / rate
			if got[i][0] != want[i][0] || got[i][1] != want[i][1] || math.Abs(at-wantAt) > 2.0/rate+1.0/r {
				t.Fatalf("rate %v: note %d is %v at %.5f s, take has %v at %.5f s", r, i, got[i][:2], at, want[i][:2], wantAt)
			}
		}
		if e.Status().Playback {
			t.Fatalf("rate %v: still playing back after the take ended", r)
		}
	}
}

func TestPlaybackFollowsTheCurrentSound(t *testing.T) {
	rec, _, _ := perform(t)
	e := NewEngine(rate)
	p := e.Params()
	p.Sound = Sound{Octave: 1, Tone: 30}
	e.Set(p)
	e.LoadTake(rec.Take())
	l := logged(e)
	e.Play()
	render(e, 0.8)
	for _, n := range l.notes {
		if n.part == partHarp {
			if n.note != 64+12 && n.note != 67+12 {
				t.Fatalf("plucked %s, want E5 or G5", NoteName(n.note))
			}
			return
		}
	}
	t.Fatal("no pluck played back")
}

func TestInputStopsPlaybackAndRestoresTheInstrument(t *testing.T) {
	e := classicEngine()
	live := Chord{Root: 9, Kind: Minor}
	e.SetChord(Chord{Root: 0, Kind: Major}, true)
	e.Record()
	render(e, 0.5)
	e.StopRecording()
	e.SetChord(live, true)

	e.Play()
	render(e, 0.1)
	if s := e.Status(); !s.Playback || s.Chord != (Chord{Root: 0, Kind: Major}) {
		t.Fatalf("playback status %+v", s)
	}
	// A screen re-sending the live state, or a mix change, keeps playing.
	e.SetChord(live, true)
	p := e.Params()
	p.Master = 0.5
	p.Sound = Sound{Octave: 1, Tone: 80}
	e.Set(p)
	if !e.Status().Playback {
		t.Fatal("a mix change stopped playback")
	}
	if e.Params().Master != 0.5 {
		t.Fatal("the mix change was not applied")
	}
	// Strumming stops it and puts the live chord back.
	e.Pluck(0, 0)
	if s := e.Status(); s.Playback || s.Chord != live || !s.Held {
		t.Fatalf("after a pluck: %+v", s)
	}
	if got := e.Params().Sound.StrumNotes(live)[0]; got != (Sound{Octave: 1}).StrumNotes(live)[0] {
		t.Fatalf("the pluck played %s in the old sound", NoteName(got))
	}

	// Playback that runs out restores the instrument too.
	e.Play()
	render(e, 0.6)
	if s := e.Status(); s.Playback || s.Chord != live {
		t.Fatalf("after the take ended: %+v", s)
	}
	// Starting the rhythm stops playback.
	e.Play()
	render(e, 0.1)
	p = e.Params()
	p.Playing = true
	e.Set(p)
	if s := e.Status(); s.Playback || !s.Rhythm {
		t.Fatalf("after starting the rhythm: %+v", s)
	}
}

func TestRecordingStopsAtTheLimit(t *testing.T) {
	e := NewEngine(1000)
	e.Record()
	s := make([]float32, 1000)
	for range MaxTakeSeconds + 2 {
		e.Render(s)
	}
	st := e.Status()
	if st.Recording || !st.HasTake || st.Length != MaxTakeSeconds {
		t.Fatalf("status %+v", st)
	}
}

func TestStatusReportsPlucksAndTransport(t *testing.T) {
	e := NewEngine(rate)
	if s := e.Status(); s.HasTake || !math.IsInf(s.Since[3], 1) {
		t.Fatalf("fresh status %+v", s)
	}
	e.Record()
	e.Pluck(3, 0)
	render(e, 0.25)
	s := e.Status()
	if !s.Recording || math.Abs(s.Position-0.25) > 0.01 || math.Abs(s.Since[3]-0.25) > 0.01 {
		t.Fatalf("recording status %+v", s)
	}
	if e.Play(); !e.Status().Playback || e.Status().Length != 0.25 {
		t.Fatalf("Play did not end the recording and play it: %+v", e.Status())
	}
}

// track is one parsed MTrk chunk.
type track struct {
	name           string
	program        int
	ons, offs      int
	tempos         []uint32
	maxNote        int
	unmatchedNotes int
}

func parseMIDI(t *testing.T, b []byte) (format, division int, tracks []track) {
	t.Helper()
	if string(b[:4]) != "MThd" || binary.BigEndian.Uint32(b[4:]) != 6 {
		t.Fatalf("no MThd header: % x", b[:14])
	}
	format = int(binary.BigEndian.Uint16(b[8:]))
	n := int(binary.BigEndian.Uint16(b[10:]))
	division = int(binary.BigEndian.Uint16(b[12:]))
	b = b[14:]
	vlq := func(b []byte) (uint32, []byte) {
		var v uint32
		for {
			c := b[0]
			b = b[1:]
			v = v<<7 | uint32(c&0x7f)
			if c&0x80 == 0 {
				return v, b
			}
		}
	}
	for range n {
		if string(b[:4]) != "MTrk" {
			t.Fatalf("expected MTrk, got %q", b[:4])
		}
		size := binary.BigEndian.Uint32(b[4:])
		body := b[8 : 8+size]
		b = b[8+size:]
		tr := track{program: -1}
		sounding := map[int]int{}
		ended := false
		for len(body) > 0 {
			_, body = vlq(body)
			status := body[0]
			switch {
			case status == 0xff:
				kind := body[1]
				var l uint32
				l, body = vlq(body[2:])
				data := body[:l]
				body = body[l:]
				switch kind {
				case 0x03:
					tr.name = string(data)
				case 0x51:
					tr.tempos = append(tr.tempos, uint32(data[0])<<16|uint32(data[1])<<8|uint32(data[2]))
				case 0x2f:
					ended = true
				}
			case status&0xf0 == 0xc0:
				tr.program = int(body[1])
				body = body[2:]
			case status&0xf0 == 0x90:
				tr.ons++
				tr.maxNote = max(tr.maxNote, int(body[1]))
				if sounding[int(body[1])] > 0 {
					t.Fatalf("%s: note %d struck while sounding", tr.name, body[1])
				}
				sounding[int(body[1])]++
				body = body[3:]
			case status&0xf0 == 0x80:
				tr.offs++
				sounding[int(body[1])]--
				body = body[3:]
			default:
				t.Fatalf("unexpected status %#x", status)
			}
		}
		if !ended {
			t.Fatalf("%s has no end-of-track", tr.name)
		}
		for _, c := range sounding {
			if c != 0 {
				tr.unmatchedNotes++
			}
		}
		tracks = append(tracks, tr)
	}
	return format, division, tracks
}

func TestExportWritesMIDIAndWAV(t *testing.T) {
	rec, _, _ := perform(t)
	take := rec.Take()
	var wav, mid bytes.Buffer
	p := rec.Params()
	p.Sound = Classic
	if err := take.Export(p, &wav, &mid); err != nil {
		t.Fatal(err)
	}

	w := wav.Bytes()
	if string(w[:4]) != "RIFF" || string(w[8:16]) != "WAVEfmt " || string(w[36:40]) != "data" {
		t.Fatalf("bad WAV header % x", w[:44])
	}
	frames := int64(math.Round(take.Seconds()*ExportRate)) + exportTail*ExportRate
	if got := binary.LittleEndian.Uint32(w[40:]); int64(got) != frames*2 || len(w) != 44+int(got) {
		t.Fatalf("WAV data size %d, file %d bytes, want %d frames", got, len(w), frames)
	}
	if binary.LittleEndian.Uint32(w[24:]) != ExportRate || binary.LittleEndian.Uint16(w[22:]) != 1 {
		t.Fatal("WAV is not mono at the export rate")
	}
	var loud bool
	for i := 44; i < len(w); i += 2 {
		if int16(binary.LittleEndian.Uint16(w[i:])) > 3000 {
			loud = true
			break
		}
	}
	if !loud {
		t.Fatal("WAV is silent")
	}

	format, division, tracks := parseMIDI(t, mid.Bytes())
	if format != 1 || division != ppq || len(tracks) != parts+1 {
		t.Fatalf("format %d, division %d, %d tracks", format, division, len(tracks))
	}
	if !slices.Equal(tracks[0].tempos, []uint32{uint32(math.Round(60e6 / 132))}) {
		t.Fatalf("tempo track %v", tracks[0].tempos)
	}
	names := []string{"Harp", "Chord", "Bass", "Comp", "Drums"}
	for i, tr := range tracks[1:] {
		if tr.name != names[i] {
			t.Fatalf("track %d is %q, want %q", i+1, tr.name, names[i])
		}
		if tr.ons != tr.offs || tr.unmatchedNotes != 0 {
			t.Fatalf("%s: %d note-ons, %d note-offs", tr.name, tr.ons, tr.offs)
		}
	}
	if tracks[1].ons != 2 || tracks[1].program != 46 {
		t.Fatalf("harp track: %d notes, program %d", tracks[1].ons, tracks[1].program)
	}
	// The pad only sounds while the rhythm is stopped: A minor at the end.
	if tracks[2].ons != 3 {
		t.Fatalf("chord track has %d notes, want 3", tracks[2].ons)
	}
	if tracks[3].ons == 0 || tracks[4].ons == 0 || tracks[5].ons < 10 || tracks[5].program != -1 {
		t.Fatalf("accompaniment tracks: bass %d, comp %d, drums %d", tracks[3].ons, tracks[4].ons, tracks[5].ons)
	}
}

func TestMIDITempoMap(t *testing.T) {
	tempos := []tempoChange{{0, 120}, {rate, 60}}
	// One second at 120 bpm is two beats; the next second at 60 is one.
	if got := ticks(tempos, rate, rate); got != 2*ppq {
		t.Fatalf("1 s is %d ticks", got)
	}
	if got := ticks(tempos, 2*rate, rate); got != 3*ppq {
		t.Fatalf("2 s is %d ticks", got)
	}
	if got := appendVLQ(nil, 0x0fffffff); !bytes.Equal(got, []byte{0xff, 0xff, 0xff, 0x7f}) {
		t.Fatalf("VLQ % x", got)
	}
}
