package main

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr-native/examples/chordharp/harp"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
	"github.com/DonaldMurillo/gofastr-native/tui/widget"
)

// control is one row of the mix panel.
type control int

const (
	ctlVolume control = iota
	ctlChord
	ctlHarp
	ctlRhythm
	ctlTempo
	ctlSustain
	controls
)

type controlSpec struct {
	name     string
	min, max float64
	step     float64
	format   func(float64) string
}

var specs = [controls]controlSpec{
	ctlVolume:  {"Volume", 0, 100, 5, percent},
	ctlChord:   {"Chord", 0, 100, 5, percent},
	ctlHarp:    {"Harp", 0, 100, 5, percent},
	ctlRhythm:  {"Rhythm", 0, 100, 5, percent},
	ctlTempo:   {"Tempo", 50, 200, 2, func(v float64) string { return fmt.Sprintf("%3.0f bpm", v) }},
	ctlSustain: {"Sustain", 0.2, 5, 0.1, func(v float64) string { return fmt.Sprintf("%4.1f s", v) }},
}

func percent(v float64) string { return fmt.Sprintf("%3.0f%%", v) }

const (
	// glowFor is how long a plucked zone stays lit.
	glowFor = 450 * time.Millisecond
	// glideGap is the time between zones when / sweeps the whole plate.
	glideGap = 28 * time.Millisecond
	// repeatWindow tells auto-repeat from a new press. Terminals send a
	// held key again and again, with no release, starting 250 to 660 ms
	// after the press on common settings and then every 30 to 90 ms. A key
	// that arrives within this window of the same key is a repeat, and
	// toggles ignore it.
	repeatWindow = 700 * time.Millisecond

	// The smallest terminal the instrument works in.
	minWidth, minHeight = 60, 17
)

// density is how tightly the screen is packed, chosen by height.
type density int

const (
	spacious density = iota // boxes, three-row chord pads: 32 rows or more
	roomy                   // boxes, two-row chord pads: 24 to 31 rows
	compact                 // boxes, one-row chord pads: 21 to 23 rows
	dense                   // section rules in place of boxes: 17 to 20 rows
)

func densityFor(height int) density {
	switch {
	case height >= 32:
		return spacious
	case height >= 24:
		return roomy
	case height >= 21:
		return compact
	}
	return dense
}

type model struct {
	eng   *harp.Engine
	audio string
	now   func() time.Time

	chord    harp.Chord
	held     bool
	played   bool // a chord has been held at least once
	pattern  int
	playing  bool
	values   [controls]float64
	selected control
	plucked  [harp.Zones]time.Time
	help     bool

	lastKey   rune
	lastKeyAt time.Time
}

func newModel(eng *harp.Engine, audio string, now func() time.Time) *model {
	m := &model{eng: eng, audio: audio, now: now, chord: harp.Chord{Root: 0, Kind: harp.Major}}
	m.values = [controls]float64{ctlVolume: 80, ctlChord: 50, ctlHarp: 80, ctlRhythm: 60, ctlTempo: 112, ctlSustain: 1.5}
	m.apply()
	return m
}

// apply sends the settings to the engine.
func (m *model) apply() {
	m.eng.Set(harp.Params{
		Master: m.values[ctlVolume] / 100, Chord: m.values[ctlChord] / 100,
		Harp: m.values[ctlHarp] / 100, Rhythm: m.values[ctlRhythm] / 100,
		Tempo: m.values[ctlTempo], Sustain: m.values[ctlSustain],
		Pattern: m.pattern, Playing: m.playing,
	})
	m.eng.SetChord(m.chord, m.held)
}

// hold holds a chord. A key that is held down repeats, so the keyboard
// only ever selects; Esc releases.
func (m *model) hold(c harp.Chord) {
	m.chord, m.held, m.played = c, true, true
	m.apply()
}

// click holds a chord from its pad. A click does not repeat, so clicking
// the held chord again releases it.
func (m *model) click(c harp.Chord) {
	if m.held && c == m.chord {
		m.release()
		return
	}
	m.hold(c)
}

func (m *model) release() {
	m.held = false
	m.apply()
}

// pluck sounds one zone of the strumplate after delay.
func (m *model) pluck(zone int, delay time.Duration) {
	m.eng.Pluck(harp.StrumNotes(m.chord)[zone], delay.Seconds())
	m.plucked[zone] = m.now().Add(delay)
}

// glide sweeps the whole plate, low to high.
func (m *model) glide() {
	for z := range harp.Zones {
		m.pluck(z, time.Duration(z)*glideGap)
	}
}

func (m *model) set(c control, v float64) {
	s := specs[c]
	v = min(max(v, s.min), s.max)
	// Snap to the step so the keyboard and the mouse agree.
	v = s.min + float64(int((v-s.min)/s.step+0.5))*s.step
	m.values[c] = min(v, s.max)
	m.apply()
}

func (m *model) nudge(c control, dir int) {
	m.set(c, m.values[c]+float64(dir)*specs[c].step)
}

func (m *model) setFraction(c control, f float64) {
	s := specs[c]
	m.set(c, s.min+f*(s.max-s.min))
}

func (m *model) fraction(c control) float64 {
	s := specs[c]
	return (m.values[c] - s.min) / (s.max - s.min)
}

func (m *model) toggleRhythm() {
	m.playing = !m.playing
	m.apply()
}

// startRhythm starts the rhythm, or takes it back to the downbeat when it
// is already running.
func (m *model) startRhythm() {
	if m.playing {
		m.eng.Restart()
		return
	}
	m.playing = true
	m.apply()
}

func (m *model) stopRhythm() {
	m.playing = false
	m.apply()
}

func (m *model) choosePattern(i int) {
	m.pattern = (i + len(harp.Patterns)) % len(harp.Patterns)
	m.apply()
}

// chordForKey maps a key of the three letter rows to its chord.
func chordForKey(r rune) (harp.Chord, bool) {
	for kind, row := range keyRows {
		if i := strings.IndexRune(row, r); i >= 0 {
			return harp.Chord{Root: harp.Roots[i], Kind: harp.Kind(kind)}, true
		}
	}
	return harp.Chord{}, false
}

// Runes standing in for keys without one in the repeat check.
const (
	runeEnter     = -1
	runeEscape    = -2
	runeBackspace = -3
)

// repeated records a key and reports whether it is auto-repeat of a held
// key. A terminal with the kitty protocol's event types says so. Legacy
// input cannot, so there a key within repeatWindow of the same key counts
// as a repeat; the window slides, so a long hold stays one press.
func (m *model) repeated(k input.Key, r rune) bool {
	now := m.now()
	rep := r == m.lastKey && now.Sub(m.lastKeyAt) < repeatWindow
	m.lastKey, m.lastKeyAt = r, now
	switch k.Action {
	case input.KeyRepeat:
		return true
	case input.KeyPress:
		return false
	}
	return rep
}

// key handles every key; the instrument has no focusable widgets.
func (m *model) key(k input.Key) bool {
	if k.Mods&(input.ModCtrl|input.ModAlt|input.ModSuper) != 0 {
		return false
	}
	r := k.Rune
	switch k.Code {
	case input.KeyRune:
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
	case input.KeyEnter:
		r = runeEnter
	case input.KeyEscape:
		r = runeEscape
	case input.KeyBackspace:
		r = runeBackspace
	default:
		r = 0
	}
	// The help dialog is modal and takes every key while it is open, so
	// nothing here runs under it.
	rep := m.repeated(k, r)
	// A key the terminal marks as held acts once, except the ones meant to
	// repeat: arrows, tempo, patterns, and chords, which only select.
	once := k.Action == input.KeyRepeat
	switch k.Code {
	case input.KeyEscape:
		if !once {
			m.release()
		}
		return true
	case input.KeyBackspace:
		m.stopRhythm()
		return true
	case input.KeyUp:
		m.selected = (m.selected + controls - 1) % controls
		return true
	case input.KeyDown:
		m.selected = (m.selected + 1) % controls
		return true
	case input.KeyLeft:
		m.nudge(m.selected, -1)
		return true
	case input.KeyRight:
		m.nudge(m.selected, 1)
		return true
	case input.KeyEnter:
		// Unguarded on legacy terminals: a held Enter keeps the rhythm at
		// beat 1, and a quick second press always restarts it.
		if !once {
			m.startRhythm()
		}
		return true
	case input.KeyRune:
	default:
		return false
	}
	if c, ok := chordForKey(r); ok {
		m.hold(c)
		return true
	}
	if z := strings.IndexRune(strumKeys, r); z >= 0 {
		if !once {
			m.pluck(z, 0)
		}
		return true
	}
	switch r {
	case ' ':
		// Space is the one toggle, so a legacy repeat must not flip it.
		if !rep {
			m.toggleRhythm()
		}
	case '/':
		if !once {
			m.glide()
		}
	case 'p':
		m.choosePattern(m.pattern + 1)
	case '[':
		m.nudge(ctlTempo, -1)
	case ']':
		m.nudge(ctlTempo, 1)
	case '?':
		m.help = true
	default:
		return false
	}
	return true
}

var (
	styleBadge   = cell.Style{Fg: cell.Black, Bg: cell.Magenta, Attrs: cell.Bold}
	styleChord   = cell.Style{Attrs: cell.Bold}
	styleMuted   = cell.Style{Fg: cell.BrightBlack}
	styleAccent  = cell.Style{Fg: cell.Cyan}
	padIdle      = cell.Style{Fg: cell.White, Bg: cell.Indexed(237)}
	padHover     = cell.Style{Fg: cell.BrightWhite, Bg: cell.Indexed(240), Underline: cell.UnderlineSingle}
	padHint      = cell.Style{Fg: cell.Indexed(245)}
	rowStyles    = [...]cell.Style{harp.Major: {Fg: cell.Black, Bg: cell.BrightYellow}, harp.Minor: {Fg: cell.Black, Bg: cell.BrightCyan}, harp.Seventh: {Fg: cell.Black, Bg: cell.BrightMagenta}}
	patternIdle  = cell.Style{Fg: cell.White}
	patternOn    = cell.Style{Fg: cell.Black, Bg: cell.Cyan, Attrs: cell.Bold}
	playOn       = cell.Style{Fg: cell.Black, Bg: cell.Green, Attrs: cell.Bold}
	playOff      = cell.Style{Fg: cell.White, Bg: cell.Indexed(237), Attrs: cell.Bold}
	beatOn       = cell.Style{Fg: cell.BrightGreen, Attrs: cell.Bold}
	beatDownbeat = cell.Style{Fg: cell.BrightYellow, Attrs: cell.Bold}
	beatOff      = cell.Style{Fg: cell.BrightBlack}
)

// litStyle is a held chord's pad: its row's color, plus bold and an
// underline so it still stands out with NO_COLOR.
func litStyle(k harp.Kind) cell.Style {
	s := rowStyles[k]
	s.Attrs |= cell.Bold
	s.Underline = cell.UnderlineSingle
	return s
}

func (m *model) view() view.Element {
	return widget.OnKey(m.key, widget.Layers(view.E(sized{build: m.screen}), view.If(m.help, view.E(sized{build: m.helpDialog}))))
}

// screen lays the instrument out for a w by h terminal.
func (m *model) screen(w, h int) view.Element {
	if w < minWidth || h < minHeight {
		return m.tooSmall(w, h)
	}
	d := densityFor(h)
	plateTitle := "Strumplate · drag across it or press 1 to = · / glides"
	mixTitle := "Mix · ↑↓ choose · ←→ change"
	if w < 72 {
		mixTitle = "Mix · ↑↓ ←→"
	}
	if d == dense {
		return widget.Column(
			m.header(),
			widget.Column(view.E(rule{"Chords"}), m.chordGrid(d)),
			widget.Grow(1, widget.Column(view.E(rule{plateTitle}), widget.Grow(1, m.plate()))),
			view.E(widget.Flex{Dir: widget.Horizontal, Gap: 2},
				widget.Grow(1, widget.Column(view.E(rule{"Rhythm"}), m.rhythmPanel(w/2))),
				widget.Grow(1, widget.Column(view.E(rule{mixTitle}), m.mixPanel())),
			),
			m.footer(w),
		)
	}
	return widget.Column(
		m.header(),
		widget.Panel("Chords", m.chordGrid(d)),
		widget.Grow(1, widget.Panel(plateTitle, m.plate())),
		widget.Row(
			widget.Grow(1, widget.Panel("Rhythm", m.rhythmPanel(w/2-2))),
			widget.Grow(1, widget.Panel(mixTitle, m.mixPanel())),
		),
		m.footer(w),
	)
}

func (m *model) tooSmall(w, h int) view.Element {
	text := func(s string, st cell.Style) view.Element {
		return view.E(widget.Text{Spans: []widget.Span{{Text: s, Style: st}}, Wrap: true, Align: layout.Center})
	}
	return widget.Center(widget.Column(
		text(" chordharp ", styleBadge),
		text("", cell.Style{}),
		text(fmt.Sprintf("This terminal is %d×%d. The instrument needs at least %d×%d.", w, h, minWidth, minHeight), cell.Style{}),
		text("Make the window bigger to play.", cell.Style{}),
		text("", cell.Style{}),
		text("ctrl+c quits", styleMuted),
	))
}

func (m *model) header() view.Element {
	spans := []widget.Span{{Text: " chordharp ", Style: styleBadge}, {Text: "  "}}
	switch {
	case m.held:
		spans = append(spans, widget.Span{Text: m.chord.Long(), Style: styleChord},
			widget.Span{Text: "  " + m.chord.Tones(), Style: styleMuted})
	case !m.played:
		spans = append(spans, widget.Span{Text: "Pick a chord, then strum", Style: styleAccent})
	default:
		spans = append(spans, widget.Span{Text: "no chord held · plate tuned to " + m.chord.String(), Style: styleMuted})
	}
	rhythm := "rhythm off"
	if m.playing {
		rhythm = fmt.Sprintf("%s %0.f bpm", strings.ToLower(harp.Patterns[m.pattern].Name), m.values[ctlTempo])
	}
	spans = append(spans, widget.Span{Text: " · " + rhythm})
	// The audio state stays whole at the right, so a silent instrument
	// always says why; the text before it is cut first.
	audio := widget.Span{Text: "  audio: " + m.audio + " ", Style: styleMuted}
	return widget.Row(widget.Grow(1, view.E(widget.Text{Spans: spans, Ellipsis: true})), view.E(widget.Text{Spans: []widget.Span{audio}}))
}

var rowLabels = [...][2]string{harp.Major: {" major", " maj"}, harp.Minor: {" minor", " min"}, harp.Seventh: {" 7th", " 7th"}}

// chordGrid draws the 27 pads. When the screen is short each pad's key
// goes before its name on one row instead of under it.
func (m *model) chordGrid(d density) view.Element {
	inline := d >= compact
	labelWidth, short, height := 6, 0, 0
	if inline {
		labelWidth, short = 4, 1
	}
	if d == spacious {
		height = 3
	}
	rows := make([]view.Element, 0, len(keyRows))
	for kind, keys := range keyRows {
		label := view.E(widget.Text{Spans: []widget.Span{{Text: rowLabels[kind][short], Style: styleMuted}}})
		if height > 0 {
			label = widget.Column(widget.Label(""), label)
		}
		cells := []view.Element{widget.Fixed(labelWidth, label)}
		for i, root := range harp.Roots {
			c := harp.Chord{Root: root, Kind: harp.Kind(kind)}
			cells = append(cells, widget.Grow(1, view.E(pad{
				Label: c.String(), Hint: string(keys[i]), Inline: inline, Height: height,
				Style: padIdle, Lit: litStyle(harp.Kind(kind)), HoverStyle: padHover, HintStyle: padHint,
				On:      m.held && m.chord == c,
				OnPress: func() { m.click(c) },
			}).WithKey(c.String())))
		}
		rows = append(rows, view.E(widget.Flex{Dir: widget.Horizontal, Gap: 1}, cells...))
	}
	return widget.Column(rows...)
}

func (m *model) plate() view.Element {
	now := m.now()
	p := plate{Notes: harp.StrumNotes(m.chord), OnZone: func(z int) { m.pluck(z, 0) }}
	for z, at := range m.plucked {
		if at.IsZero() {
			continue
		}
		if age := now.Sub(at); age >= 0 && age < glowFor {
			p.Glow[z] = 1 - float64(age)/float64(glowFor)
		}
	}
	return view.E(p).WithKey("plate")
}

func (m *model) rhythmPanel(width int) view.Element {
	label, style := " ▶ Start ", playOff
	if m.playing {
		label, style = " ■ Stop ", playOn
	}
	start := view.E(pad{Label: label, Style: style, Lit: style, HoverStyle: playOn, OnPress: m.toggleRhythm}).WithKey("start")

	// The beat in play is a diamond, the others dots, so the count reads
	// without color too.
	pat := harp.Patterns[m.pattern]
	beat := -1
	if step := m.eng.Step(); step >= 0 {
		beat = step / pat.PerBeat
	}
	beats := []widget.Span{{Text: "  "}}
	for b := range pat.Beats() {
		text, st := "·", beatOff
		if b == beat {
			text, st = "◆", beatOn
			if b == 0 {
				st = beatDownbeat
			}
		}
		beats = append(beats, widget.Span{Text: text + " ", Style: st})
	}
	top := widget.Row(start, view.E(widget.Text{Spans: beats}))

	// Patterns are radio buttons: the mark shows the choice without color.
	var names []view.Element
	for i, p := range harp.Patterns {
		st, mark := patternIdle, "○ "
		if i == m.pattern {
			st, mark = patternOn, "● "
		}
		names = append(names, view.E(pad{Label: mark + p.Name, Style: st, Lit: st, HoverStyle: padHover, OnPress: func() { m.choosePattern(i) }}).WithKey(p.Name))
	}
	// Three to a row when they fit, two otherwise.
	perRow := 3
	if width < 34 {
		perRow = 2
	}

	rows := []view.Element{top}
	for i := 0; i < len(names); i += perRow {
		rows = append(rows, view.E(widget.Flex{Dir: widget.Horizontal, Gap: 1}, names[i:min(i+perRow, len(names))]...))
	}
	// The panel is as tall as the mix panel beside it. The Enter hint goes
	// in when the rows left hold it, and a spacer when there is one more.
	room := int(controls) - len(rows)
	all := [][2]string{{"space", "start/stop"}, {"enter", "from\u00a0beat\u00a01"}, {"p", "next\u00a0pattern"}, {"[\u00a0]", "tempo"}}
	chosen := slices.Delete(slices.Clone(all), 1, 2)
	if hintRows(all, width) <= room {
		chosen = all
	}
	// No-break spaces keep each hint whole when the line wraps, and only
	// the gaps between hints can break.
	var keys []widget.Span
	for i, h := range chosen {
		if i > 0 {
			keys = append(keys, widget.Span{Text: "  "})
		}
		keys = append(keys, widget.Span{Text: h[0], Style: widget.Theme.Accent}, widget.Span{Text: "\u00a0" + h[1], Style: widget.Theme.Muted})
	}
	hints := view.E(widget.Text{Spans: keys, Wrap: true})

	if hintRows(chosen, width) < room {
		rows = append(rows, widget.Label(""))
	}
	return widget.Column(append(rows, hints)...)
}

// hintRows is how many rows hints take at width when only the gaps
// between them break.
func hintRows(hints [][2]string, width int) int {
	rows, used := 1, 0
	for _, h := range hints {
		n := len([]rune(h[0])) + 1 + len([]rune(h[1]))
		switch {
		case used == 0:
			used = n
		case used+2+n <= width:
			used += 2 + n
		default:
			rows, used = rows+1, n
		}
	}
	return rows
}

func (m *model) mixPanel() view.Element {
	rows := make([]view.Element, 0, controls)
	for c := range controls {
		rows = append(rows, view.E(slider{
			Label: specs[c].name, Value: m.fraction(c), Text: specs[c].format(m.values[c]),
			Selected: c == m.selected,
			OnSelect: func() { m.selected = c },
			OnSet:    func(f float64) { m.setFraction(c, f) },
			OnNudge:  func(dir int) { m.nudge(c, dir) },
		}).WithKey(specs[c].name))
	}
	return widget.Column(rows...)
}

// spansWidth is the width of hint text, which is all ASCII.
func spansWidth(spans []widget.Span) int {
	n := 0
	for _, sp := range spans {
		n += len(sp.Text)
	}
	return n
}

func hintSpans(pairs ...[2]string) []widget.Span {
	var spans []widget.Span
	for _, h := range pairs {
		spans = append(spans, widget.Span{Text: " " + h[0], Style: widget.Theme.Accent}, widget.Span{Text: " " + h[1] + " ", Style: widget.Theme.Muted})
	}
	return spans
}

// footer keeps help and quit at the right and adds whole hints on the
// left while they fit, so none is cut in half.
func (m *model) footer(width int) view.Element {
	right := hintSpans([2]string{"?", "help"}, [2]string{"ctrl+c", "quit"})
	room := width - spansWidth(right)
	var left []widget.Span
	for _, h := range [][2]string{{"q-o a-l z-.", "chords"}, {"1-=", "strum"}, {"esc", "release"}, {"space", "rhythm"}, {"/", "glide"}} {
		spans := hintSpans(h)
		if spansWidth(left)+spansWidth(spans) > room {
			break
		}
		left = append(left, spans...)
	}
	return widget.Row(widget.Grow(1, view.E(widget.Text{Spans: left})), view.E(widget.Text{Spans: right}))
}

// helpEntries are the help dialog's rows: keys, then what they do.
var helpEntries = [][2]string{
	{"q w e r t y u i o", "Hold a major chord, E flat to B in fifths"},
	{"a s d f g h j k l", "Hold a minor chord"},
	{"z x c v b n m , .", "Hold a seventh chord"},
	{"Esc", "Release the chord. A chord stays held until you pick another, because terminals do not report key releases. Clicking the held chord's pad also releases it."},
	{"1 to 0, - and =", "Pluck the strumplate's zones, low to high"},
	{"/", "Sweep the whole strumplate"},
	{"Mouse on the plate", "Press to pluck, drag across to strum"},
	{"Space", "Start or stop the rhythm. A held key counts once. Terminals without the kitty keyboard protocol also skip a second tap within 0.7 s; Enter and Backspace never skip."},
	{"Enter", "Start the rhythm, or restart it from beat 1"},
	{"Backspace", "Stop the rhythm"},
	{"p", "Next rhythm pattern; or click one"},
	{"[ and ]", "Slower and faster"},
	{"↑ ↓ and ← →", "Choose a mix control, then change it. Click a name to choose it; click, drag or scroll its bar to set it."},
	{"?", "This help. ↑ ↓ and PgUp PgDn scroll it; Esc or Enter closes it."},
	{"Ctrl+C", "Quit"},
}

// wrappedLines counts the lines text takes when wrapped at width.
func wrappedLines(text string, width int) int {
	lines, used := 1, 0
	for _, word := range strings.Fields(text) {
		n := len([]rune(word))
		switch {
		case used == 0:
			used = n
		case used+1+n <= width:
			used += 1 + n
		default:
			lines, used = lines+1, n
		}
	}
	return lines
}

const (
	helpWidth = 84
	helpKeys  = 19
)

// helpDialog lists the keys. Its body scrolls when the terminal is too
// short for it, and is only as tall as its text otherwise. The body holds
// focus, so arrow and page keys reach the scroller; there is no button to
// take focus away from it.
func (m *model) helpDialog(w, h int) view.Element {
	closeHelp := func() { m.help = false }
	// Border, padding and the gap beside the keys.
	descWidth := min(w, helpWidth) - 4 - helpKeys - 2
	rows := make([]view.Element, 0, len(helpEntries))
	lines := 0
	for _, e := range helpEntries {
		rows = append(rows, view.E(widget.Flex{Dir: widget.Horizontal, Gap: 2, Align: layout.Start},
			widget.Fixed(helpKeys, widget.Styled(e[0], styleAccent)),
			widget.Grow(1, widget.Paragraph(e[1])),
		))
		lines += wrappedLines(e[1], descWidth)
	}
	scrolls := lines+helpChrome > h
	hint := "Esc or Enter closes"
	if scrolls {
		hint = "↑↓ PgUp PgDn scroll · Esc closes"
	}
	closeRow := widget.Row(
		view.E(pad{Label: " Close ", Style: playOff, Lit: playOff, HoverStyle: playOn, OnPress: closeHelp}).WithKey("close-help"),
		widget.Grow(1, view.E(widget.Text{Spans: []widget.Span{{Text: "  " + hint, Style: widget.Theme.Muted}}, Wrap: true})),
	)
	body := widget.Limit(0, max(min(lines, h-helpChrome), 1), widget.Scrolling(view.E(helpBody{onClose: closeHelp}, widget.Column(rows...))))
	box := view.E(surface{widget.Box{Border: widget.BorderRounded, Title: "How to play", Padding: layout.Symmetric(0, 1), Style: widget.Theme.Text}},
		widget.Column(body, widget.Label(""), closeRow))
	return view.E(widget.Modal{OnClose: closeHelp}, widget.Limit(helpWidth, 0, box))
}

// helpChrome is the help dialog's height besides its entries: the border,
// the gap above the close row and the close row.
const helpChrome = 4

// surface is a box that clears what is under it, for a dialog.
type surface struct{ widget.Box }

func (b surface) Draw(n *view.Node, c *view.Canvas) {
	c.Fill(layout.Rect{W: c.Size().W, H: c.Size().H}, b.Style)
	b.Box.Draw(n, c)
}

// helpBody holds focus inside the help dialog. Enter closes it; the
// navigation keys bubble to the scroller around it.
type helpBody struct{ onClose func() }

func (helpBody) Layout(n *view.Node, c layout.Constraints) layout.Size {
	return n.LayoutChild(0, c)
}

func (helpBody) Draw(*view.Node, *view.Canvas) {}
func (helpBody) Focusable() bool               { return true }
func (helpBody) AutoFocus() bool               { return true }

func (b helpBody) Handle(_ *view.Node, e view.Event) bool {
	k, ok := e.(view.KeyEvent)
	if !ok || k.Mods != 0 {
		return false
	}
	if k.Code == input.KeyEnter {
		b.onClose()
		return true
	}
	return false
}
