package main

import (
	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/cmd/chordharp/harp"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
	"github.com/DonaldMurillo/gofastr-native/tui/widget"
)

// sized builds its child once it knows how much space it has, so the
// screen can pick a layout for the terminal's size.
type sized struct{ build func(w, h int) view.Element }

func (sized) BuildsChildren()               {}
func (sized) Draw(*view.Node, *view.Canvas) {}

func (s sized) Layout(n *view.Node, c layout.Constraints) layout.Size {
	n.SetChildren([]view.Element{s.build(c.MaxW, c.MaxH)})
	return n.LayoutChild(0, layout.Tight(c.Max()))
}

// pad is a button that acts on press, the way an instrument key does, and
// never takes focus, so every key reaches the instrument. Its hint (the
// key that plays it) goes below the label, or before it when Inline.
type pad struct {
	Label, Hint string
	Inline      bool
	Width       int
	Height      int // rows; the label and hint are centered in them
	Style, Lit  cell.Style
	HoverStyle  cell.Style
	HintStyle   cell.Style
	On          bool
	OnPress     func()
}

func (p pad) Layout(n *view.Node, c layout.Constraints) layout.Size {
	m := n.Tree().Method
	w, h := p.Width, 1
	if w == 0 {
		w = m.Width(p.Label) + 2
		if p.Inline && p.Hint != "" {
			w += m.Width(p.Hint) + 1
		}
	}
	if p.Hint != "" && !p.Inline {
		h = 2
	}
	h = max(h, p.Height)
	return c.Constrain(layout.Size{W: w, H: h})
}

// styles returns the label and hint styles for the pad's state. The hint
// takes the label's colors when the pad is lit or hovered: a separate
// gray can land on the same palette entry as the background on a
// 16-color terminal.
func (p pad) styles(hovered bool) (label, hint cell.Style) {
	label, hint = p.Style, p.HintStyle
	hint.Bg = label.Bg
	switch {
	case p.On:
		label = p.Lit
		hint = cell.Style{Fg: label.Fg, Bg: label.Bg}
	case hovered && p.HoverStyle != (cell.Style{}):
		label = p.HoverStyle
		hint = cell.Style{Fg: label.Fg, Bg: label.Bg}
	}
	return label, hint
}

func (p pad) Draw(n *view.Node, c *view.Canvas) {
	size := c.Size()
	style, hint := p.styles(n.Hovered())
	c.Fill(layout.Rect{W: size.W, H: size.H}, style)
	m := c.Method()
	lw, hw := m.Width(p.Label), m.Width(p.Hint)
	switch {
	case p.Hint != "" && p.Inline && hw+1+lw <= size.W:
		x, y := layout.Center.Offset(size.W, hw+1+lw), (size.H-1)/2
		c.Text(x, y, p.Hint, hint)
		c.Text(x+hw+1, y, p.Label, style)
	case p.Hint != "" && !p.Inline && size.H > 1:
		y := (size.H - 2) / 2
		c.TextLimit(layout.Center.Offset(size.W, lw), y, size.W, p.Label, style)
		c.TextLimit(layout.Center.Offset(size.W, hw), y+1, size.W, p.Hint, hint)
	default:
		c.TextLimit(layout.Center.Offset(size.W, lw), (size.H-1)/2, size.W, p.Label, style)
	}
}

func (pad) Hoverable() bool { return true }

func (p pad) Handle(n *view.Node, e view.Event) bool {
	pe, ok := e.(*view.PointerEvent)
	if !ok {
		return false
	}
	switch pe.Kind {
	case view.Press:
		if pe.Button != input.ButtonLeft {
			return false
		}
		if p.OnPress != nil {
			p.OnPress()
		}
		return true
	case view.Release, view.Drag, view.Enter, view.Leave:
		return true
	}
	return false
}

// plate is the strumplate: one zone per note, plucked by pressing or by
// dragging across it. A fast drag skips cells between two mouse reports,
// so every zone it crossed is plucked, in order. Under the strings each
// zone shows its key and note: "1 G3" on one row when zones are wide
// enough, on two rows when not, and only the key when one row is left.
type plate struct {
	Notes  [harp.Zones]int
	Glow   [harp.Zones]float64 // 0 to 1, how recently each zone sounded
	OnZone func(zone int)
}

type plateState struct{ last int }

func (plate) NewState() any { return &plateState{last: -1} }

func (plate) Layout(_ *view.Node, c layout.Constraints) layout.Size {
	return c.Constrain(layout.Size{W: c.MaxW, H: max(c.MinH, 3)})
}

// zoneAt maps a column to a zone; zones share the width evenly.
func zoneAt(x, width int) int {
	if width <= 0 {
		return 0
	}
	return min(max(x*harp.Zones/width, 0), harp.Zones-1)
}

// zoneSpan returns the columns of zone z and the column of its string.
func zoneSpan(z, width int) (x0, x1, mid int) {
	x0, x1 = z*width/harp.Zones, (z+1)*width/harp.Zones
	return x0, x1, (x0 + x1) / 2
}

// combinedLabel is the widest "key note" label, as in "1 C#4".
const combinedLabel = 5

var (
	stringIdle = cell.Style{Fg: cell.Indexed(244)}
	stringFret = cell.Style{Fg: cell.Indexed(238)}
	glowRamp   = []cell.Color{cell.Indexed(117), cell.Indexed(159), cell.Indexed(195), cell.Indexed(231)}
	plateNote  = cell.Style{}
	plateKey   = cell.Style{Fg: cell.Cyan}
	plateLit   = cell.Style{Fg: cell.Indexed(231), Attrs: cell.Bold}
)

// labelRows returns how many rows under the strings hold labels, and
// whether key and note share one row.
func labelRows(width, height int) (rows int, combined bool) {
	// A zone needs a column to spare, or neighbors' labels touch.
	combined = width/harp.Zones > combinedLabel
	switch {
	case combined && height >= 2:
		return 1, true
	case height >= 4:
		return 2, false
	case height >= 2:
		return 1, false
	}
	return 0, false
}

func (p plate) Draw(n *view.Node, c *view.Canvas) {
	size := c.Size()
	if size.W <= 0 || size.H <= 0 {
		return
	}
	labels, combined := labelRows(size.W, size.H)
	strings := size.H - labels
	m := c.Method()
	for z := range harp.Zones {
		x0, x1, mid := zoneSpan(z, size.W)
		style, glyph := stringIdle, "│"
		keySt, noteSt := plateKey, plateNote
		if g := p.Glow[z]; g > 0 {
			i := min(int(g*float64(len(glowRamp))), len(glowRamp)-1)
			style, glyph = cell.Style{Fg: glowRamp[i], Attrs: cell.Bold}, "┃"
			if g > 0.5 {
				glyph = "█"
			}
			keySt, noteSt = plateLit, plateLit
		}
		for y := range strings {
			if x0 > 0 && mid != x0 {
				c.Text(x0, y, "┊", stringFret)
			}
			c.Text(mid, y, glyph, style)
		}
		key, note := strumKeys[z:z+1], harp.NoteName(p.Notes[z])
		w := x1 - x0
		switch {
		case combined:
			x := x0 + layout.Center.Offset(w, m.Width(key)+1+m.Width(note))
			c.Text(x, size.H-1, key, keySt)
			c.Text(x+m.Width(key)+1, size.H-1, note, noteSt)
		case labels == 2:
			c.TextLimit(x0+layout.Center.Offset(w, m.Width(key)), size.H-2, w, key, keySt)
			c.TextLimit(x0+layout.Center.Offset(w, m.Width(note)), size.H-1, w, note, noteSt)
		case labels == 1:
			c.TextLimit(x0+layout.Center.Offset(w, m.Width(key)), size.H-1, w, key, keySt)
		}
	}
}

func (plate) Hoverable() bool { return true }

func (p plate) Handle(n *view.Node, e view.Event) bool {
	pe, ok := e.(*view.PointerEvent)
	if !ok {
		return false
	}
	st := n.State().(*plateState)
	width := n.Rect().W
	switch pe.Kind {
	case view.Press:
		if pe.Button != input.ButtonLeft {
			return false
		}
		st.last = zoneAt(pe.Local.X, width)
		p.zone(st.last)
		return true
	case view.Drag:
		if st.last < 0 {
			return true
		}
		to := zoneAt(pe.Local.X, width)
		step := 1
		if to < st.last {
			step = -1
		}
		for z := st.last; z != to; {
			z += step
			p.zone(z)
		}
		st.last = to
		return true
	case view.Release:
		st.last = -1
		return true
	case view.Enter, view.Leave, view.Move:
		return true
	}
	return false
}

func (p plate) zone(z int) {
	if p.OnZone != nil {
		p.OnZone(z)
	}
}

// slider shows a value as a bar. A press on the bar sets the value and
// drags it; a press on the label or the number only selects the control,
// so a click meant to select it for the arrow keys cannot mute anything.
// The wheel nudges the value.
type slider struct {
	Label    string
	Value    float64 // 0 to 1
	Text     string
	Selected bool
	OnSelect func()
	OnSet    func(v float64)
	OnNudge  func(dir int)
}

type sliderState struct{ dragging bool }

const (
	sliderLabel = 9 // "› Sustain"
	sliderValue = 8 // " 112 bpm"
)

func (slider) NewState() any { return &sliderState{} }

func (slider) Layout(_ *view.Node, c layout.Constraints) layout.Size {
	return c.Constrain(layout.Size{W: c.MaxW, H: 1})
}

var (
	barOn       = cell.Style{Fg: cell.Cyan}
	barOff      = cell.Style{Fg: cell.Indexed(238)}
	barKnob     = cell.Style{Fg: cell.BrightCyan, Attrs: cell.Bold}
	labelStyle  = cell.Style{Fg: cell.BrightBlack}
	selectedLbl = cell.Style{Fg: cell.Cyan, Attrs: cell.Bold}
)

// bar returns the bar's first column and width.
func (slider) bar(width int) (x, w int) {
	return sliderLabel + 1, max(width-sliderLabel-1-sliderValue, 1)
}

func (s slider) Draw(n *view.Node, c *view.Canvas) {
	size := c.Size()
	label, lst := "  "+s.Label, labelStyle
	if s.Selected {
		label, lst = "› "+s.Label, selectedLbl
	}
	c.TextLimit(0, 0, sliderLabel, label, lst)
	x, w := s.bar(size.W)
	filled := int(s.Value*float64(w) + 0.5)
	for i := range w {
		if i < filled {
			c.Text(x+i, 0, "━", barOn)
		} else {
			c.Text(x+i, 0, "─", barOff)
		}
	}
	knob := min(max(filled-1, 0), w-1)
	c.Text(x+knob, 0, "●", barKnob)
	vw := c.Method().Width(s.Text)
	vst := cell.Style{}
	if s.Selected {
		vst.Attrs = cell.Bold
	}
	c.TextLimit(size.W-vw, 0, vw, s.Text, vst)
}

func (slider) Hoverable() bool { return true }

func (s slider) set(localX, width int) {
	if s.OnSet == nil {
		return
	}
	// The first cell is 0 and the last is 1.
	x, w := s.bar(width)
	s.OnSet(min(max(float64(localX-x)/float64(max(w-1, 1)), 0), 1))
}

func (s slider) Handle(n *view.Node, e view.Event) bool {
	pe, ok := e.(*view.PointerEvent)
	if !ok {
		return false
	}
	st := n.State().(*sliderState)
	width := n.Rect().W
	switch pe.Kind {
	case view.Press:
		if pe.Button != input.ButtonLeft {
			return false
		}
		if s.OnSelect != nil {
			s.OnSelect()
		}
		x, w := s.bar(width)
		if pe.Local.X >= x && pe.Local.X < x+w {
			st.dragging = true
			s.set(pe.Local.X, width)
		}
		return true
	case view.Drag:
		if st.dragging {
			s.set(pe.Local.X, width)
		}
		return true
	case view.Release:
		st.dragging = false
		return true
	case view.Wheel:
		if s.OnSelect != nil {
			s.OnSelect()
		}
		if s.OnNudge != nil {
			switch pe.Button {
			case input.ButtonWheelUp, input.ButtonWheelRight:
				s.OnNudge(1)
			case input.ButtonWheelDown, input.ButtonWheelLeft:
				s.OnNudge(-1)
			}
		}
		return true
	case view.Enter, view.Leave:
		return true
	}
	return false
}

// rule is a one-row section heading, used in place of a box's border
// when the terminal is too short for boxes.
type rule struct{ Title string }

func (rule) Layout(_ *view.Node, c layout.Constraints) layout.Size {
	return c.Constrain(layout.Size{W: c.MaxW, H: 1})
}

func (r rule) Draw(n *view.Node, c *view.Canvas) {
	size := c.Size()
	x := c.Text(0, 0, "─ ", widget.Theme.Border)
	x += c.TextLimit(x, 0, size.W-x, r.Title, widget.Theme.Title)
	if x < size.W {
		x += c.Text(x, 0, " ", widget.Theme.Border)
	}
	for ; x < size.W; x++ {
		c.Text(x, 0, "─", widget.Theme.Border)
	}
}
