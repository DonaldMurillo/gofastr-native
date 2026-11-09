package chordharp

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/examples/chordharp/harp"
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
	if !s.Held || s.Chord != "A minor" || s.Tones != "A C E" || s.Short != "Am" || s.Zones[0] != "A4" || s.Sound != "Bright" {
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

func TestSoundSettingsAreKept(t *testing.T) {
	dir := t.TempDir()
	if err := Start(dir); err != nil {
		t.Fatal(err)
	}
	Open(44100)
	if s := decode(t); s.Sound != "Bright" || s.Preset != 1 || s.Octave != 1 || s.Tone != 60 {
		t.Fatalf("first start: %+v", s)
	}
	SetPreset(0)
	if s := decode(t); s.Sound != "Classic" || s.Zones[0] == "" || s.Octave != 0 {
		t.Fatalf("Classic: %+v", s)
	}
	SetPreset(9)
	for range 5 {
		NudgeOctave(-1)
		NudgeTone(1)
	}
	if s := decode(t); s.Octave != harp.MinOctave || s.Tone != 80 || s.Sound != "Custom" || s.Preset != -1 {
		t.Fatalf("after nudges: %+v", s)
	}
	for range 20 {
		NudgeTone(1)
	}
	if decode(t).Tone != 100 {
		t.Fatal("tone above 100")
	}
	Stop()

	// The next start reads the saved sound.
	play.params.Sound = harp.DefaultSound()
	if err := Start(dir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		Stop()
		play.Lock()
		play.settings = ""
		play.Unlock()
		SetPreset(1)
	}()
	if s := decode(t); s.Octave != harp.MinOctave || s.Tone != 100 {
		t.Fatalf("after restart: %+v", s)
	}
	if _, err := os.Stat(filepath.Join(dir, "chordharp", "settings.json")); err != nil {
		t.Fatal(err)
	}
}

func TestRecordPlayAndExport(t *testing.T) {
	Open(44100)
	Release()
	if s := decode(t); s.Recording || s.Playback {
		t.Fatalf("idle: %+v", s)
	}
	Record()
	Hold(0, 3) // C major
	Fill(4410)
	Pluck(4, 0)
	Fill(4410)
	if s := decode(t); !s.Recording || s.Position < 0.19 {
		t.Fatalf("recording: %+v", s)
	}
	// A new engine, as after a pause, ends the recording and keeps it.
	Open(48000)
	if s := decode(t); s.Recording || !s.HasTake || s.Length < 0.19 {
		t.Fatalf("after Open: %+v", s)
	}

	Hold(1, 6) // A minor, live
	PlayTake()
	Fill(2400)
	s := decode(t)
	if !s.Playback || s.Short != "C" || s.Column != 3 || len(s.Since) != harp.Zones {
		t.Fatalf("playing back: %+v", s)
	}
	Fill(9600)
	if s := decode(t); s.Playback || s.Short != "Am" {
		t.Fatalf("after the take: %+v", s)
	}
	PlayTake()
	PlayTake()
	if decode(t).Playback {
		t.Fatal("PlayTake did not stop playback")
	}

	dir := t.TempDir()
	mid, wav := filepath.Join(dir, "take.mid"), filepath.Join(dir, "take.wav")
	if err := Export(mid, wav); err != nil {
		t.Fatal(err)
	}
	for path, magic := range map[string]string{mid: "MThd", wav: "RIFF"} {
		b, err := os.ReadFile(path)
		if err != nil || len(b) < 100 || string(b[:4]) != magic {
			t.Fatalf("%s: %d bytes, %v", filepath.Base(path), len(b), err)
		}
	}
	Release()
}
