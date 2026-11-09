package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-native/examples/chordharp/harp"

	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
)

func (s *screen) press(code input.KeyCode, mods input.Mods) {
	s.now = s.now.Add(repeatWindow + time.Millisecond)
	s.send(input.Key{Code: code, Mods: mods})
}

func (s *screen) ctrl(r rune) {
	s.now = s.now.Add(repeatWindow + time.Millisecond)
	s.send(input.Key{Code: input.KeyRune, Rune: r, Mods: input.ModCtrl})
}

func TestTabCyclesThePages(t *testing.T) {
	s := newScreen(t, 120, 40)
	for _, want := range []page{pageSound, pageTake, pageMix} {
		s.press(input.KeyTab, 0)
		if s.m.page != want || !s.shows(pageNames[want]+" · ") {
			t.Fatalf("page %d, want %d:\n%s", s.m.page, want, s.text())
		}
	}
	s.press(input.KeyTab, input.ModShift)
	if s.m.page != pageTake {
		t.Fatalf("shift+tab went to page %d", s.m.page)
	}
	if !s.shows("Rec") || !s.shows("No take yet") {
		t.Fatalf("take page:\n%s", s.text())
	}
}

func TestSoundPageChangesAndKeepsTheSound(t *testing.T) {
	s := newScreen(t, 120, 40)
	s.m.settings = filepath.Join(t.TempDir(), "chordharp", "settings.json")
	s.press(input.KeyTab, 0)
	if !s.shows("Bright") || !s.shows("Strumplate G4 to E8") {
		t.Fatalf("sound page:\n%s", s.text())
	}
	s.press(input.KeyRight, 0) // Preset: Bright to High
	if got := s.m.eng.Params().Sound; got != harp.Presets[2].Sound {
		t.Fatalf("engine sound %+v after choosing High", got)
	}
	s.press(input.KeyDown, 0)
	s.press(input.KeyDown, 0)
	s.press(input.KeyLeft, 0) // Tone down a step
	want := harp.Sound{Octave: 2, Tone: 50 - toneStep}
	if got := s.m.eng.Params().Sound; got != want {
		t.Fatalf("engine sound %+v, want %+v", got, want)
	}
	if !s.shows("Custom") {
		t.Fatalf("an edited sound is not shown as Custom:\n%s", s.text())
	}
	if saved, err := harp.LoadSound(s.m.settings); err != nil || saved != want {
		t.Fatalf("saved %+v, %v", saved, err)
	}
	// Arrows on the Sound page leave the mix alone.
	if s.m.selected != ctlVolume || s.m.values[ctlVolume] != 80 {
		t.Fatalf("mix moved: %v %v", s.m.selected, s.m.values[ctlVolume])
	}
}

func TestRecordPlayAndSave(t *testing.T) {
	s := newScreen(t, 120, 40)
	dir := filepath.Join(t.TempDir(), "takes")
	s.m.takesDir = dir
	s.m.run = func(f func()) { f() }

	s.ctrl('p')
	if lines, _ := s.m.note.get(); lines[0] != "Nothing recorded yet." {
		t.Fatalf("play with no take: %q", lines)
	}
	s.ctrl('r')
	if !s.m.eng.Status().Recording {
		t.Fatal("ctrl+r did not record")
	}
	s.keys("r")
	render(s.m.eng, 0.6)
	s.keys("5")
	render(s.m.eng, 0.6)
	s.frame()
	if !s.shows("● REC 0:01") {
		t.Fatalf("header while recording:\n%s", s.text())
	}
	s.ctrl('r')
	st := s.m.eng.Status()
	if st.Recording || !st.HasTake || st.Length < 0.9 {
		t.Fatalf("after ctrl+r: %+v", st)
	}

	s.keys("a") // E flat minor, so playback has a chord to change back from
	s.ctrl('p')
	render(s.m.eng, 0.2)
	s.frame()
	if !s.m.eng.Status().Playback || !s.shows("▶ take 0:00 / 0:01") || !s.shows("C major") {
		t.Fatalf("header while playing:\n%s", s.text())
	}
	s.ctrl('p')
	if s.m.eng.Status().Playback || !s.shows("Eb minor") {
		t.Fatalf("ctrl+p did not stop playback:\n%s", s.text())
	}

	s.ctrl('e')
	name := "chordharp-" + s.now.Format("20060102-150405")
	for _, ext := range []string{".mid", ".wav"} {
		info, err := os.Stat(filepath.Join(dir, name+ext))
		if err != nil || info.Size() < 100 {
			t.Fatalf("%s: %v", ext, err)
		}
	}
	s.press(input.KeyTab, 0)
	s.press(input.KeyTab, 0)
	if !s.shows("Saved "+name) || !s.shows("Take 0:01") {
		t.Fatalf("take page after saving:\n%s", s.text())
	}
}

func TestKeysStopPlayback(t *testing.T) {
	s := newScreen(t, 120, 40)
	s.ctrl('r')
	s.keys("r")
	render(s.m.eng, 0.3)
	s.ctrl('r')
	for _, stop := range []func(){
		func() { s.keys("a") },
		func() { s.keys("1") },
		func() { s.keys(" ") },
		func() { s.press(input.KeyEscape, 0) },
	} {
		s.ctrl('p')
		if !s.m.eng.Status().Playback {
			t.Fatal("ctrl+p did not play")
		}
		s.now = s.now.Add(repeatWindow + time.Millisecond)
		stop()
		if s.m.eng.Status().Playback {
			t.Fatalf("still playing after input:\n%s", s.text())
		}
	}
}

// Every page fits wherever the instrument does, at the same height as Mix.
func TestEveryPageFitsEverySupportedSize(t *testing.T) {
	keys := [pages][]string{
		pageMix:   {specs[0].name, specs[controls-1].name},
		pageSound: soundNames[:],
		pageTake:  {"record", "play", "save"},
	}
	for _, size := range [][2]int{{60, 17}, {60, 24}, {80, 18}, {80, 24}, {120, 40}} {
		w, h := size[0], size[1]
		s := newScreen(t, w, h)
		mix := s.find("start")
		for p := range pages {
			s.m.page = p
			s.frame()
			screen := layout.Rect{W: w, H: h}
			for _, key := range keys[p] {
				if r := s.find(key); r.Empty() || r.Intersect(screen) != r {
					t.Fatalf("%dx%d page %s: %s at %+v\n%s", w, h, pageNames[p], key, r, s.text())
				}
			}
			if got := s.find("start"); got != mix {
				t.Fatalf("%dx%d page %s moved the rhythm panel from %+v to %+v", w, h, pageNames[p], mix, got)
			}
			if !strings.Contains(s.text(), pageNames[p]) {
				t.Fatalf("%dx%d: no %s title:\n%s", w, h, pageNames[p], s.text())
			}
		}
	}
}
