package chordharp

import (
	"encoding/binary"
	"encoding/json"
	"testing"
)

func decode(t *testing.T) state {
	t.Helper()
	var s state
	if err := json.Unmarshal([]byte(State()), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func loudest(pcm []byte) int {
	peak := 0
	for i := 0; i+1 < len(pcm); i += 2 {
		v := int(int16(binary.LittleEndian.Uint16(pcm[i:])))
		peak = max(peak, v, -v)
	}
	return peak
}

func TestPlaying(t *testing.T) {
	if got := len(Fill(64)); got != 128 {
		t.Fatalf("Fill(64) gave %d bytes", got)
	}
	Open(48000)
	if loudest(Fill(4800)) != 0 {
		t.Fatal("sound before anything was played")
	}
	Hold(1, 6) // A minor
	s := decode(t)
	if !s.Held || s.Chord != "A minor" || s.Tones != "A C E" || s.Short != "Am" || s.Zones[0] != "A3" {
		t.Fatalf("after Hold: %+v", s)
	}
	Pluck(0, 0)
	if loudest(Fill(4800)) < 1000 {
		t.Fatal("pluck is silent")
	}
	Release()
	if decode(t).Held {
		t.Fatal("still held")
	}
	Hold(5, 0)
	Hold(0, 99)
	Pluck(12, 0)
	if s := decode(t); s.Held || s.Chord != "A minor" {
		t.Fatalf("out-of-range input changed state: %+v", s)
	}
}

func TestRhythmControls(t *testing.T) {
	Open(44100)
	NextPattern(-1)
	if s := decode(t); s.Pattern != 5 || s.Steps != 12 || s.PerBeat != 3 {
		t.Fatalf("pattern wrap: %+v", s)
	}
	NextPattern(1)
	for range 100 {
		NudgeTempo(2)
	}
	if decode(t).Tempo != 200 {
		t.Fatal("tempo above 200")
	}
	for range 100 {
		NudgeTempo(-2)
	}
	if decode(t).Tempo != 50 {
		t.Fatal("tempo below 50")
	}
	Toggle()
	Fill(64)
	if !decode(t).Playing || Step() < 0 {
		t.Fatal("rhythm did not start")
	}
	// A new engine keeps the rhythm and the latch.
	SetLatch(true)
	Open(22050)
	Fill(64)
	if s := decode(t); !s.Playing || !s.Latch || Step() < 0 {
		t.Fatalf("Open lost state: %+v", s)
	}
	Toggle()
	if Step() != -1 {
		t.Fatal("rhythm did not stop")
	}
}

func TestScreenRenders(t *testing.T) {
	if err := Start(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer Stop()
	out, err := Render(Root(), "{}")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Screen struct {
			Title      string                  `json:"title"`
			Components []struct{ Name string } `json:"components"`
		} `json:"screen"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	screen := envelope.Screen
	if screen.Title != "Chord Harp" || len(screen.Components) != 1 || screen.Components[0].Name != "dev.gofastr.chordharp/v1" {
		t.Fatalf("screen %s", out)
	}
}
