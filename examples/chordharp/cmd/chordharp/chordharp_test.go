package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-native/examples/chordharp/harp"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/uni"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
)

func TestChordNamesAndKeys(t *testing.T) {
	for _, test := range []struct {
		key  rune
		want string
	}{{'q', "Eb"}, {'r', "C"}, {'o', "B"}, {'a', "Ebm"}, {'h', "Dm"}, {'z', "Eb7"}, {'b', "G7"}, {'.', "B7"}} {
		c, ok := chordForKey(test.key)
		if !ok || c.String() != test.want {
			t.Errorf("key %q gives %v (%v), want %s", test.key, c, ok, test.want)
		}
	}
	if _, ok := chordForKey('p'); ok {
		t.Error("p is a chord key")
	}
}

// screen renders the model after every event, the way the app loop does.
type screen struct {
	t    *testing.T
	m    *model
	tree *view.Tree
	buf  *cell.Buffer
	now  time.Time
}

func newScreen(t *testing.T, w, h int) *screen {
	s := &screen{t: t, tree: view.NewTree(uni.Graphemes), buf: cell.NewBuffer(w, h), now: time.Unix(1000, 0)}
	s.tree.Now = func() time.Time { return s.now }
	s.m = newModel(harp.NewEngine(sampleRate), "test", func() time.Time { return s.now })
	s.frame()
	return s
}

func (s *screen) frame() { s.tree.Frame(s.m.view(), s.buf) }

func (s *screen) send(events ...input.Event) {
	for _, e := range events {
		s.tree.Dispatch(e)
		s.frame()
	}
}

func (s *screen) keys(text string) {
	for _, r := range text {
		s.send(input.Key{Code: input.KeyRune, Rune: r})
	}
}

func (s *screen) text() string {
	var b strings.Builder
	for y := range s.buf.Height() {
		for x := range s.buf.Width() {
			c := s.buf.At(x, y)
			if c.Width > 0 {
				b.WriteString(c.Text)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func (s *screen) shows(text string) bool { return strings.Contains(s.text(), text) }

// find returns the screen rect of the first node with key.
func (s *screen) find(key string) layout.Rect {
	s.t.Helper()
	n := s.tree.Find(func(n *view.Node) bool { return n.Key() == key })
	if n == nil {
		s.t.Fatalf("no node %q:\n%s", key, s.text())
	}
	return n.Rect()
}

func mouse(x, y int, action input.MouseAction) input.Mouse {
	return input.Mouse{X: x, Y: y, Button: input.ButtonLeft, Action: action}
}

func TestLayoutFitsAStandardTerminal(t *testing.T) {
	s := newScreen(t, 80, 24)
	for _, want := range []string{"chordharp", "Chords", "Strumplate", "Rhythm", "Mix", "Eb", "Bm", "B7", "Start", "Swing", "Sustain", "help"} {
		if !s.shows(want) {
			t.Errorf("80x24 lacks %q", want)
		}
	}
	if plate := s.find("plate"); plate.H < 3 {
		t.Errorf("strumplate is %d rows", plate.H)
	}
	if t.Failed() || testing.Verbose() {
		t.Logf("\n%s", s.text())
	}
}

func TestChordKeysHoldAndIgnoreRepeat(t *testing.T) {
	s := newScreen(t, 100, 30)
	if !s.shows("Pick a chord") {
		t.Fatalf("no first-run prompt:\n%s", s.text())
	}
	s.keys("w")
	if !s.m.held || s.m.chord != (harp.Chord{Root: 10, Kind: harp.Major}) || !s.shows("Bb major") || !s.shows("Bb D F") {
		t.Fatalf("w: held %v chord %v\n%s", s.m.held, s.m.chord, s.text())
	}
	s.keys("H") // shifted letters play too
	if s.m.chord != (harp.Chord{Root: 2, Kind: harp.Minor}) {
		t.Fatalf("H gives %v", s.m.chord)
	}
	// A held key repeats; every repeat, and a later press, keeps the chord.
	s.keys("hhhhhh")
	s.now = s.now.Add(2 * time.Second)
	s.keys("h")
	if !s.m.held || s.m.chord != (harp.Chord{Root: 2, Kind: harp.Minor}) {
		t.Fatal("pressing the held chord's key released it")
	}
	s.send(input.Key{Code: input.KeyEscape})
	if s.m.held || !s.shows("no chord held") {
		t.Fatal("Esc did not release the chord")
	}
}

func TestClickingTheHeldPadReleasesIt(t *testing.T) {
	s := newScreen(t, 100, 30)
	r := s.find("C")
	click := func() { s.send(mouse(r.X+1, r.Y, input.MousePress), mouse(r.X+1, r.Y, input.MouseRelease)) }
	click()
	if !s.m.held {
		t.Fatal("click did not hold C")
	}
	click()
	if s.m.held {
		t.Fatal("second click did not release C")
	}
}

func TestToggleKeysIgnoreAutoRepeat(t *testing.T) {
	s := newScreen(t, 100, 30)
	s.keys("     ") // space held down
	if !s.m.playing {
		t.Fatal("holding space toggled the rhythm more than once")
	}
	// A long hold: each repeat is within the window of the one before,
	// though most are far past the first press.
	for range 6 {
		s.now = s.now.Add(500 * time.Millisecond)
		s.keys(" ")
	}
	if !s.m.playing {
		t.Fatal("a long hold of space stopped the rhythm")
	}
	s.now = s.now.Add(time.Second)
	s.keys(" ")
	if s.m.playing {
		t.Fatal("a new press of space did not stop the rhythm")
	}
	s.keys("??")
	if !s.m.help {
		t.Fatal("holding ? closed help")
	}
	s.send(input.Key{Code: input.KeyEscape})
	// Another key between two spaces makes the second one a new press.
	s.now = s.now.Add(time.Second)
	s.keys(" r ")
	if s.m.playing {
		t.Fatal("space, r, space left the rhythm running")
	}
}

func TestClickingAChordPadHoldsIt(t *testing.T) {
	s := newScreen(t, 100, 30)
	r := s.find("G7")
	s.send(mouse(r.X+1, r.Y, input.MousePress), mouse(r.X+1, r.Y, input.MouseRelease))
	if !s.m.held || s.m.chord.String() != "G7" {
		t.Fatalf("click held %v %v", s.m.held, s.m.chord)
	}
}

func TestNumberRowPlucksZones(t *testing.T) {
	s := newScreen(t, 100, 30)
	s.keys("r1=")
	if v := s.m.eng.Voices(); v != 2 {
		t.Fatalf("%d voices after two plucks", v)
	}
	if s.m.plucked[0] != s.now || s.m.plucked[harp.Zones-1] != s.now || !s.m.plucked[5].IsZero() {
		t.Fatalf("lit zones %v", s.m.plucked)
	}
}

func TestDraggingAcrossThePlateStrumsEveryZone(t *testing.T) {
	s := newScreen(t, 120, 30)
	r := s.find("plate")
	y := r.Y + 1
	zoneX := func(z int) int { return r.X + z*r.W/harp.Zones + 1 }
	// One press then a fast drag that skips most cells.
	s.send(mouse(zoneX(1), y, input.MousePress), mouse(zoneX(9), y, input.MouseDrag), mouse(zoneX(4), y, input.MouseDrag), mouse(zoneX(4), y, input.MouseRelease))
	// Zones 1 to 9 up, then 8 down to 4.
	if v, want := s.m.eng.Voices(), 9+5; v != want {
		t.Fatalf("%d plucks, want %d", v, want)
	}
	for z := range harp.Zones {
		lit := !s.m.plucked[z].IsZero()
		if lit != (z >= 1 && z <= 9) {
			t.Errorf("zone %d lit %v", z, lit)
		}
	}
	// The plate draws a lit string where it was plucked.
	mid := r.X + (5*r.W/harp.Zones+6*r.W/harp.Zones)/2
	if c := s.buf.At(mid, y); c.Text != "█" {
		t.Errorf("zone 5 draws %q", c.Text)
	}
	s.now = s.now.Add(glowFor)
	s.frame()
	if c := s.buf.At(mid, y); c.Text != "│" {
		t.Errorf("zone 5 still lit after the glow: %q", c.Text)
	}
}

func TestGlideSchedulesTheWholePlate(t *testing.T) {
	s := newScreen(t, 100, 30)
	s.keys("/")
	if v := s.m.eng.Voices(); v != harp.Zones {
		t.Fatalf("%d notes scheduled", v)
	}
	if got := s.m.plucked[harp.Zones-1].Sub(s.now); got != time.Duration(harp.Zones-1)*glideGap {
		t.Fatalf("last zone lights after %v", got)
	}
}

func TestRhythmControls(t *testing.T) {
	s := newScreen(t, 100, 30)
	s.keys(" ")
	if !s.m.playing || !s.shows("Stop") || !s.shows("rock 112 bpm") {
		t.Fatalf("space did not start the rhythm:\n%s", s.text())
	}
	s.keys("p")
	if s.m.pattern != 1 || !s.shows("waltz") {
		t.Fatal("p did not move to the next pattern")
	}
	r := s.find("Swing")
	s.send(mouse(r.X, r.Y, input.MousePress), mouse(r.X, r.Y, input.MouseRelease))
	if s.m.pattern != len(harp.Patterns)-1 {
		t.Fatalf("clicking Swing chose %d", s.m.pattern)
	}
	s.keys("]]")
	if s.m.values[ctlTempo] != 116 {
		t.Fatalf("tempo %v after ]]", s.m.values[ctlTempo])
	}
	s.send(input.Key{Code: input.KeyBackspace})
	if s.m.playing {
		t.Fatal("Backspace did not stop the rhythm")
	}
}

// Explicit start and stop keys never toggle, so quick presses do what
// they say however close together they come.
func TestTransportKeysStartAndStop(t *testing.T) {
	s := newScreen(t, 100, 30)
	enter := input.Key{Code: input.KeyEnter}
	back := input.Key{Code: input.KeyBackspace}
	s.send(enter)
	if !s.m.playing {
		t.Fatal("Enter did not start the rhythm")
	}
	render(s.m.eng, 1)
	if s.m.eng.Step() == 0 {
		t.Fatal("the rhythm did not advance")
	}
	s.now = s.now.Add(time.Second)
	s.send(enter)
	if !s.m.playing || s.m.eng.Step() != 0 {
		t.Fatalf("Enter while playing: playing %v step %d, want a restart at step 0", s.m.playing, s.m.eng.Step())
	}
	// A second Enter soon after the first still restarts the bar.
	s.now = s.now.Add(150 * time.Millisecond)
	render(s.m.eng, 0.4)
	s.send(enter)
	if s.m.eng.Step() != 0 {
		t.Fatalf("a quick second Enter left the rhythm at step %d", s.m.eng.Step())
	}
	// A repeat the terminal marks does not.
	render(s.m.eng, 0.4)
	step := s.m.eng.Step()
	s.send(input.Key{Code: input.KeyEnter, Action: input.KeyRepeat})
	if s.m.eng.Step() != step {
		t.Fatal("a marked repeat of Enter restarted the bar")
	}
	// Stop and start straight after each other.
	s.send(back, back)
	if s.m.playing {
		t.Fatal("Backspace did not stop the rhythm")
	}
	s.send(enter)
	if !s.m.playing {
		t.Fatal("Enter straight after Backspace did not start the rhythm")
	}
	s.keys(" ")
	s.send(enter)
	if !s.m.playing {
		t.Fatal("space then Enter left the rhythm stopped")
	}
}

// Where the terminal marks repeats, quick taps all count and only held
// keys are skipped.
func TestMarkedRepeats(t *testing.T) {
	s := newScreen(t, 100, 30)
	press := func(r rune) input.Key { return input.Key{Code: input.KeyRune, Rune: r, Action: input.KeyPress} }
	held := func(r rune) input.Key { return input.Key{Code: input.KeyRune, Rune: r, Action: input.KeyRepeat} }
	s.send(press(' '))
	s.now = s.now.Add(150 * time.Millisecond)
	s.send(press(' '))
	if s.m.playing {
		t.Fatal("two quick taps of space left the rhythm running")
	}
	s.send(press(' '), held(' '), held(' '))
	if !s.m.playing {
		t.Fatal("a held space toggled more than once")
	}
	s.send(press('w'), press('1'), held('1'), held('1'), held('/'))
	if got := s.m.eng.Voices(); got != 1 {
		t.Fatalf("a held strum key plucked %d notes, want 1", got)
	}
	s.send(input.Key{Code: input.KeyEscape, Action: input.KeyRepeat})
	if !s.m.held {
		t.Fatal("a marked repeat of Esc released the chord")
	}
	// Keys meant to repeat still do.
	s.send(input.Key{Code: input.KeyRune, Rune: ']', Action: input.KeyRepeat})
	if s.m.values[ctlTempo] != 114 {
		t.Fatalf("held ] left tempo at %v", s.m.values[ctlTempo])
	}
}

// At the smallest size the header still says whether there is sound.
func TestHeaderKeepsAudioState(t *testing.T) {
	for _, audio := range []string{"none", "off (-silent)"} {
		s := newScreen(t, minWidth, minHeight)
		s.m.audio = audio
		s.frame()
		if line := strings.SplitN(s.text(), "\n", 2)[0]; !strings.Contains(line, "audio: "+audio) {
			t.Fatalf("header %q lacks the audio state %q", line, audio)
		}
	}
}

func TestGlideRepeats(t *testing.T) {
	s := newScreen(t, 100, 30)
	s.keys("w//")
	if got := s.m.eng.Voices(); got != 2*harp.Zones {
		t.Fatalf("two quick glides scheduled %d notes, want %d", got, 2*harp.Zones)
	}
}

func TestMixKeysAndMouse(t *testing.T) {
	s := newScreen(t, 100, 30)
	s.send(input.Key{Code: input.KeyDown}, input.Key{Code: input.KeyDown}, input.Key{Code: input.KeyLeft})
	if s.m.selected != ctlHarp || s.m.values[ctlHarp] != 75 {
		t.Fatalf("selected %v harp %v", s.m.selected, s.m.values[ctlHarp])
	}
	s.send(input.Key{Code: input.KeyUp}, input.Key{Code: input.KeyUp}, input.Key{Code: input.KeyUp})
	if s.m.selected != ctlSustain {
		t.Fatalf("up wraps to %v", s.m.selected)
	}
	for range 100 {
		s.send(input.Key{Code: input.KeyRight})
	}
	if s.m.values[ctlSustain] != 5 {
		t.Fatalf("sustain clamps at %v", s.m.values[ctlSustain])
	}
	r := s.find("Volume")
	x, w := slider{}.bar(r.W)
	s.send(mouse(r.X+x, r.Y, input.MousePress), mouse(r.X+x, r.Y, input.MouseRelease))
	s.send(mouse(r.X+x+w-1, r.Y, input.MousePress), mouse(r.X+x+w-1, r.Y, input.MouseRelease))
	if s.m.values[ctlVolume] != 100 || s.m.selected != ctlVolume {
		t.Fatalf("clicking the end of the bar set %v", s.m.values[ctlVolume])
	}
	s.send(mouse(r.X+x, r.Y, input.MousePress), mouse(r.X+x-5, r.Y, input.MouseDrag), mouse(r.X+x-5, r.Y, input.MouseRelease))
	if s.m.values[ctlVolume] != 0 {
		t.Fatalf("dragging left of the bar set %v", s.m.values[ctlVolume])
	}
	s.send(input.Mouse{X: r.X + x, Y: r.Y, Button: input.ButtonWheelUp, Action: input.MousePress})
	if s.m.values[ctlVolume] != 5 {
		t.Fatalf("wheel set %v", s.m.values[ctlVolume])
	}
	if s.m.eng.Params().Master != 0.05 {
		t.Fatalf("engine master %v", s.m.eng.Params().Master)
	}
}

func TestHelpSwallowsInstrumentKeys(t *testing.T) {
	s := newScreen(t, 100, 30)
	s.keys("?")
	if !s.shows("How to play") {
		t.Fatal("no help")
	}
	s.keys("w1")
	if s.m.held || s.m.eng.Voices() != 0 {
		t.Fatal("keys played under the help dialog")
	}
	s.send(input.Key{Code: input.KeyEscape})
	if s.m.help || s.shows("How to play") {
		t.Fatal("Esc did not close help")
	}
}

func TestCtrlKeysAreLeftToTheApp(t *testing.T) {
	s := newScreen(t, 100, 30)
	if s.m.key(input.Key{Code: input.KeyRune, Rune: 'c', Mods: input.ModCtrl}) {
		t.Fatal("ctrl+c was taken by the instrument")
	}
}

func TestSliderLabelAndValueOnlySelect(t *testing.T) {
	s := newScreen(t, 100, 30)
	r := s.find("Volume")
	x, w := slider{}.bar(r.W)
	for _, at := range []int{r.X + 2, r.X + r.W - 2} { // the label, the number
		s.m.selected = ctlSustain
		s.send(mouse(at, r.Y, input.MousePress), mouse(at, r.Y, input.MouseRelease))
		if s.m.values[ctlVolume] != 80 || s.m.selected != ctlVolume {
			t.Fatalf("click at %d: volume %v, selected %v", at-r.X, s.m.values[ctlVolume], s.m.selected)
		}
	}
	// A drag that starts on the label does not grab the bar.
	s.send(mouse(r.X+2, r.Y, input.MousePress), mouse(r.X+x+w-1, r.Y, input.MouseDrag), mouse(r.X+x+w-1, r.Y, input.MouseRelease))
	if s.m.values[ctlVolume] != 80 {
		t.Fatalf("drag from the label set volume %v", s.m.values[ctlVolume])
	}
	// One that starts on the bar keeps setting it past either end.
	s.send(mouse(r.X+x+2, r.Y, input.MousePress), mouse(r.X+r.W+5, r.Y, input.MouseDrag), mouse(r.X+r.W+5, r.Y, input.MouseRelease))
	if s.m.values[ctlVolume] != 100 {
		t.Fatalf("drag past the end set volume %v", s.m.values[ctlVolume])
	}
	// The bar's first cell is 0 and its last is full.
	for _, c := range []struct {
		dx   int
		want float64
	}{{x, 0}, {x + w - 1, 100}} {
		s.send(mouse(r.X+c.dx, r.Y, input.MousePress), mouse(r.X+c.dx, r.Y, input.MouseRelease))
		if s.m.values[ctlVolume] != c.want {
			t.Fatalf("click on bar cell %d set volume %v, want %v", c.dx-x, s.m.values[ctlVolume], c.want)
		}
	}
}

// plateLabels returns the text of the rows under the plate's strings.
func (s *screen) plateRow(y int) string {
	r := s.find("plate")
	var b strings.Builder
	for x := r.X; x < r.X+r.W; x++ {
		if c := s.buf.At(x, y); c.Width > 0 {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

func TestEverySupportedSizeKeepsTheWholeInstrument(t *testing.T) {
	for _, size := range [][2]int{{60, 17}, {60, 20}, {60, 24}, {70, 21}, {80, 18}, {80, 21}, {80, 23}, {80, 24}, {120, 40}, {200, 60}} {
		w, h := size[0], size[1]
		s := newScreen(t, w, h)
		s.keys("w") // B flat major: "1 Bb3" is as wide as a label gets
		fail := func(format string, args ...any) {
			t.Helper()
			t.Fatalf("%dx%d: %s\n%s", w, h, fmt.Sprintf(format, args...), s.text())
		}
		screen := layout.Rect{W: w, H: h}
		visible := func(key string) layout.Rect {
			r := s.find(key)
			if r.Empty() || r.Intersect(screen) != r {
				fail("%s is at %+v", key, r)
			}
			return r
		}
		plate := visible("plate")
		if plate.H < 3 {
			fail("strumplate has %d rows", plate.H)
		}
		// Every zone's key is labeled under the strings.
		var labels string
		for y := plate.Y + 1; y < plate.Y+plate.H; y++ {
			labels += s.plateRow(y)
		}
		for _, k := range strumKeys {
			if !strings.ContainsRune(labels, k) {
				fail("plate lacks the label for %q", k)
			}
		}
		// Labels never run into their neighbors: each row holds one word
		// per zone, or two ("1 Bb3") when key and note share it.
		for y := plate.Y + 1; y < plate.Y+plate.H; y++ {
			row := s.plateRow(y)
			if strings.ContainsAny(row, "│┃█") {
				continue
			}
			if n := len(strings.Fields(row)); n != harp.Zones && n != 2*harp.Zones {
				fail("label row %q has %d words", row, n)
			}
		}
		// Every chord pad shows its name and its key.
		for kind, keys := range keyRows {
			for i, root := range harp.Roots {
				c := harp.Chord{Root: root, Kind: harp.Kind(kind)}
				r := visible(c.String())
				var text string
				for y := r.Y; y < r.Y+r.H; y++ {
					for x := r.X; x < r.X+r.W; x++ {
						text += s.buf.At(x, y).Text
					}
				}
				if !strings.Contains(text, c.String()) || !strings.ContainsRune(text, rune(keys[i])) {
					fail("pad %s reads %q", c, text)
				}
			}
		}
		for _, p := range harp.Patterns {
			if r := visible(p.Name); !strings.Contains(s.text(), p.Name) || r.W < len(p.Name) {
				fail("pattern %s clipped", p.Name)
			}
		}
		for _, c := range specs {
			visible(c.name)
			if !s.shows(c.name) {
				fail("control %s clipped", c.name)
			}
		}
		for _, want := range []string{"Start", "? help", "ctrl+c quit", "start/stop", "tempo"} {
			if !s.shows(want) {
				fail("lacks %q", want)
			}
		}
	}
}

func TestTooSmallSaysSo(t *testing.T) {
	for _, size := range [][2]int{{59, 30}, {100, 16}, {30, 10}} {
		s := newScreen(t, size[0], size[1])
		text := strings.Join(strings.Fields(s.text()), " ")
		if !strings.Contains(text, fmt.Sprintf("%d×%d", size[0], size[1])) || !strings.Contains(text, "ctrl+c quits") {
			t.Fatalf("%v:\n%s", size, s.text())
		}
		s.buf.Resize(80, 24)
		s.frame()
		if !s.shows("Strumplate") {
			t.Fatalf("%v: no instrument after growing", size)
		}
	}
}

// The help body holds focus, so the keyboard scrolls it to the end on a
// short terminal.
func TestHelpScrollsByKeyboard(t *testing.T) {
	for _, k := range []input.KeyCode{input.KeyEnd, input.KeyPageDown, input.KeyDown} {
		s := newScreen(t, minWidth, minHeight)
		s.keys("?")
		if s.shows("Ctrl+C") {
			t.Fatalf("the last entry shows before scrolling:\n%s", s.text())
		}
		if !s.shows("PgDn scroll ·") {
			t.Fatalf("no scroll hint:\n%s", s.text())
		}
		for range 30 {
			s.send(input.Key{Code: k})
		}
		if !s.shows("Ctrl+C") || !s.m.help {
			t.Fatalf("%v did not scroll help to its end:\n%s", k, s.text())
		}
	}
	// No scroll hint when everything fits.
	s := newScreen(t, 100, 40)
	s.keys("?")
	if !s.shows("Ctrl+C") || s.shows("PgDn scroll ·") || !s.shows("Esc or Enter closes") {
		t.Fatalf("tall help:\n%s", s.text())
	}
	s.send(input.Key{Code: input.KeyEnter})
	if s.m.help || s.m.playing {
		t.Fatalf("Enter on help: help %v playing %v; want closed and still stopped", s.m.help, s.m.playing)
	}
	// A marked repeat of the Enter that closed help does nothing, and a
	// new press right after starts the rhythm.
	s.send(input.Key{Code: input.KeyEnter, Action: input.KeyRepeat})
	if s.m.playing {
		t.Fatal("the repeat of the Enter that closed help started the rhythm")
	}
	s.now = s.now.Add(150 * time.Millisecond)
	s.send(input.Key{Code: input.KeyEnter})
	if !s.m.playing {
		t.Fatal("Enter just after closing help did not start the rhythm")
	}
}

func TestCtrlCQuitsUnderHelp(t *testing.T) {
	s := newScreen(t, 100, 30)
	s.keys("?")
	if s.tree.Dispatch(input.Key{Code: input.KeyRune, Rune: 'c', Mods: input.ModCtrl}) {
		t.Fatal("help consumed ctrl+c, so the app cannot quit")
	}
}

func TestClickingCloseShutsHelp(t *testing.T) {
	s := newScreen(t, minWidth, minHeight)
	s.keys("?")
	r := s.find("close-help")
	s.send(mouse(r.X+1, r.Y, input.MousePress), mouse(r.X+1, r.Y, input.MouseRelease))
	if s.m.help {
		t.Fatal("clicking Close left help open")
	}
}

func TestHelpFitsTheSmallestTerminal(t *testing.T) {
	s := newScreen(t, minWidth, minHeight)
	s.keys("?")
	if !s.shows("How to play") || !s.shows("Close") {
		t.Fatalf("help at the minimum size:\n%s", s.text())
	}
	// Long entries wrap inside the dialog instead of running off it.
	if !s.shows("report key") && !s.shows("releases") {
		t.Fatalf("help body clipped:\n%s", s.text())
	}
}

// distinct reports whether fg and bg differ once downgraded to mode. The
// default foreground counts as differing from any background but the
// default.
func distinct(st cell.Style, mode cell.ColorMode) bool {
	fg, bg := st.Fg.Downgrade(mode), st.Bg.Downgrade(mode)
	if fg.IsDefault() || bg.IsDefault() {
		return fg.IsDefault() != bg.IsDefault() || fg.IsDefault()
	}
	return fg != bg
}

func TestPadTextStaysReadableIn16Colors(t *testing.T) {
	for kind := harp.Major; kind <= harp.Seventh; kind++ {
		p := pad{Style: padIdle, Lit: litStyle(kind), HoverStyle: padHover, HintStyle: padHint}
		for _, state := range []struct {
			name        string
			on, hovered bool
		}{{"idle", false, false}, {"hover", false, true}, {"held", true, false}} {
			p.On = state.on
			label, hint := p.styles(state.hovered)
			for _, mode := range []cell.ColorMode{cell.Color16, cell.Color256, cell.ColorTrue} {
				if !distinct(label, mode) || !distinct(hint, mode) {
					t.Errorf("%v %s in mode %d: label %+v hint %+v", kind, state.name, mode, label, hint)
				}
			}
		}
	}
	for name, st := range map[string]cell.Style{"pattern": patternIdle, "pattern on": patternOn, "start": playOff, "stop": playOn} {
		if !distinct(st, cell.Color16) {
			t.Errorf("%s unreadable in 16 colors", name)
		}
	}
}

func TestStateShowsWithoutColor(t *testing.T) {
	strip := func(st cell.Style) cell.Style {
		st.Fg, st.Bg, st.UnderlineColor = cell.Default, cell.Default, cell.Default
		return st
	}
	for kind := harp.Major; kind <= harp.Seventh; kind++ {
		if strip(litStyle(kind)) == strip(padIdle) {
			t.Errorf("held %v pad looks idle without color", kind)
		}
	}
	if strip(padHover) == strip(padIdle) {
		t.Error("hovered pad looks idle without color")
	}
	s := newScreen(t, 100, 30)
	s.keys(" ")
	if !s.shows("◆") || !s.shows("● Rock") || !s.shows("○ Waltz") {
		t.Fatalf("beat and pattern marks missing:\n%s", s.text())
	}
}

// render runs the engine for seconds of audio.
func render(e *harp.Engine, seconds float64) {
	out := make([]float32, 512)
	for range int(seconds * sampleRate / 512) {
		e.Render(out)
	}
}
