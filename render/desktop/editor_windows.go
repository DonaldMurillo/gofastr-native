package desktop

import (
	"image"
	"slices"
	"sort"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/exp/textinput"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

type gpuTextInput struct {
	key                      string
	preedit                  string
	preeditStart, preeditEnd int // the IME's target-clause selection within preedit, in bytes
	composer                 textinput.Composer
}

func (p placedNode) isText() bool { return p.node.kind == inputNode || p.node.kind == areaNode }

func (p placedNode) editBox() image.Rectangle {
	box := p.box
	if p.node.value.(Field).Label != "" {
		box.Min.Y = min(box.Max.Y, box.Min.Y+21)
	}
	if p.node.value.(Field).Error != "" {
		box.Max.Y = max(box.Min.Y, box.Max.Y-21)
	}
	return box
}

func (p placedNode) textClip() image.Rectangle {
	box := p.editBox()
	return image.Rectangle{
		Min: box.Min.Add(image.Pt(8, 4)),
		Max: box.Max.Sub(image.Pt(8, 4)),
	}
}

func visibleTextLines(item placedNode) int { return max(1, item.textClip().Dy()/textLineHeight) }

func (w *gpuWindow) caret(item placedNode) image.Point {
	b := item.state.edit
	clip := item.textClip()
	line := b.line(b.cursor)
	x := int(text.Advance(b.value[b.lines[line]:b.cursor], w.theme.mono))
	return image.Pt(clip.Min.X+x-b.scrollX, clip.Min.Y+(line-b.scrollLine)*textLineHeight)
}

func (w *gpuWindow) revealCaret(item placedNode) {
	b := item.state.edit
	line := b.line(b.cursor)
	b.scrollLine = max(0, min(line, max(b.scrollLine, line-visibleTextLines(item)+1)))
	x := int(text.Advance(b.value[b.lines[line]:b.cursor], w.theme.mono))
	width := max(1, item.textClip().Dx()-2)
	b.scrollX = max(0, min(x, max(b.scrollX, x-width)))
}

// revealPreedit keeps the composition visible: the caret an IME user sees sits
// at the end of the preedit, which grows past the committed text's caret.
func (w *gpuWindow) revealPreedit(item placedNode) {
	b := item.state.edit
	line := b.line(b.cursor)
	b.scrollLine = max(0, min(line, max(b.scrollLine, line-visibleTextLines(item)+1)))
	start, _ := b.selection()
	lo, _ := b.lineBounds(line)
	from := max(start, lo)
	x := int(text.Advance(b.value[lo:from]+w.text.preedit, w.theme.mono))
	width := max(1, item.textClip().Dx()-2)
	b.scrollX = max(0, min(x, max(b.scrollX, x-width)))
}

func (w *gpuWindow) textPosition(item placedNode, point image.Point) int {
	b := item.state.edit
	clip := item.textClip()
	line := max(0, min(len(b.lines)-1, b.scrollLine+(point.Y-clip.Min.Y)/textLineHeight))
	start, end := b.lineBounds(line)
	left, _ := slices.BinarySearch(b.stops, start)
	right, _ := slices.BinarySearch(b.stops, end)
	x := float64(point.X - clip.Min.X + b.scrollX)
	i := sort.Search(right-left, func(i int) bool {
		a := text.Advance(b.value[start:b.stops[left+i]], w.theme.mono)
		z := text.Advance(b.value[start:b.stops[left+i+1]], w.theme.mono)
		return (a+z)/2 >= x
	})
	return b.stops[left+i]
}

func (w *gpuWindow) confirmInput() {
	if w.text.key != "" {
		w.text.composer.Confirm()
		w.text.key, w.text.preedit = "", ""
	}
}

func (w *gpuWindow) startInput(key string) {
	w.text.key = key
	w.text.composer = textinput.Composer{
		OnNewSession: func() *textinput.SessionOptions {
			item, ok := w.scene.find(key)
			if !ok || !item.isText() || !item.focusable() {
				return nil
			}
			b := item.state.edit
			start, end := b.selection()
			caret := w.caret(item)
			clip := item.textClip()
			caret.X = max(clip.Min.X, min(caret.X, clip.Max.X))
			caret.Y = max(clip.Min.Y, min(caret.Y, clip.Max.Y))
			return &textinput.SessionOptions{
				CaretBounds:     w.deviceScale.rect(image.Rectangle{Min: caret, Max: caret.Add(image.Pt(2, textLineHeight))}),
				TextBeforeCaret: b.value[:start], TextAfterCaret: b.value[end:],
			}
		},
		OnComposition: func(c *textinput.Composition) {
			start, end := c.SelectionRangeInBytes()
			w.preeditChanged(key, c.Text(), start, end)
		},
		OnCommit: func(c *textinput.Commit) {
			before, after := c.IsSurroundingTextReplaced()
			prefix, suffix := c.SurroundingText()
			w.commitInput(key, c.Text(), before || after, prefix, suffix)
		},
	}
}

// preeditChanged mirrors one IME composition update: a non-empty preedit
// carrying the IME's target-clause selection, or an empty one when the IME
// cancelled. The clause range is clamped because IME offsets are untrusted.
func (w *gpuWindow) preeditChanged(key, text string, selStart, selEnd int) {
	selStart, selEnd = max(0, min(selStart, len(text))), max(0, min(selEnd, len(text)))
	if selStart > selEnd {
		selStart, selEnd = selEnd, selStart
	}
	w.text.preedit, w.text.preeditStart, w.text.preeditEnd = text, selStart, selEnd
	if text != "" {
		if item, ok := w.scene.find(key); ok && item.isText() {
			w.revealPreedit(item)
		}
	}
}

// commitInput applies one IME commit. Typically the committed text replaces
// the current selection; when the IME replaced surrounding text, the commit's
// prefix and suffix carry what remains of the text the session started with,
// and the replaced range is derived from their lengths. The offsets are
// checked against the current buffer so a stale session cannot corrupt it.
func (w *gpuWindow) commitInput(key, text string, replaced bool, prefix, suffix string) {
	item, ok := w.scene.find(key)
	if !ok || !item.isText() || !item.focusable() {
		return
	}
	b := item.state.edit
	start, end := b.selection()
	if replaced {
		start, end = len(prefix), len(b.value)-len(suffix)
	}
	if start >= 0 && end >= start && end <= len(b.value) {
		w.replaceInput(item, start, end, text)
	}
}

func (w *gpuWindow) replaceInput(item placedNode, start, end int, value string) {
	b := item.state.edit
	value = cleanInput(value, item.node.kind == areaNode)
	if b.replace(start, end, value) {
		item.node.value.(Field).Change(b.value)
		w.refresh()
	}
	if next, ok := w.scene.find(item.node.key); ok && next.state == item.state {
		w.revealCaret(next)
	}
}

func (w *gpuWindow) inputKeys() bool {
	item, ok := w.scene.find(w.scene.focus)
	if !ok || !item.isText() || !item.focusable() || !ebiten.IsFocused() {
		w.confirmInput()
		return false
	}
	if w.drag == item.node.key {
		return true
	}
	if w.text.key != item.node.key {
		w.confirmInput()
		w.revealCaret(item)
		w.startInput(item.node.key)
	}
	handled, err := w.text.composer.Update()
	if err != nil {
		w.err = err
		return true
	}
	if handled {
		return true
	}
	item, ok = w.scene.find(w.scene.focus)
	if !ok || !item.isText() || !item.focusable() {
		return false
	}
	just := inpututil.IsKeyJustPressed
	ctrl, shift := ebiten.IsKeyPressed(ebiten.KeyControl), ebiten.IsKeyPressed(ebiten.KeyShift)
	b := item.state.edit
	start, end := b.selection()
	restart := func() { w.confirmInput() }
	move := func(pos int) {
		restart()
		b.move(pos, shift)
		w.revealCaret(item)
	}
	switch {
	case ctrl && just(ebiten.KeyA):
		restart()
		b.anchor, b.cursor = 0, len(b.value)
		w.revealCaret(item)
	case ctrl && just(ebiten.KeyC):
		w.copyMessage(b.value[start:end])
	case ctrl && just(ebiten.KeyX):
		if start != end && w.copyText(b.value[start:end]) {
			restart()
			w.replaceInput(item, start, end, "")
		}
	case ctrl && just(ebiten.KeyV):
		if value, ok := readClipboard(); ok {
			restart()
			w.replaceInput(item, start, end, value)
		}
	case just(ebiten.KeyArrowLeft):
		pos := b.step(b.cursor, -1)
		if !shift && start != end {
			pos = start
		}
		move(pos)
	case just(ebiten.KeyArrowRight):
		pos := b.step(b.cursor, 1)
		if !shift && start != end {
			pos = end
		}
		move(pos)
	case just(ebiten.KeyArrowUp) && item.node.kind == areaNode:
		move(b.vertical(-1))
	case just(ebiten.KeyArrowDown) && item.node.kind == areaNode:
		move(b.vertical(1))
	case just(ebiten.KeyHome):
		move(b.lines[b.line(b.cursor)])
	case just(ebiten.KeyEnd):
		_, pos := b.lineBounds(b.line(b.cursor))
		move(pos)
	case just(ebiten.KeyBackspace):
		restart()
		if start == end {
			start = b.step(start, -1)
		}
		w.replaceInput(item, start, end, "")
	case just(ebiten.KeyDelete):
		restart()
		if start == end {
			end = b.step(end, 1)
		}
		w.replaceInput(item, start, end, "")
	case just(ebiten.KeyEnter):
		restart()
		field := item.node.value.(Field)
		if item.node.kind == areaNode && !ctrl {
			w.replaceInput(item, start, end, "\n")
		} else if field.Submit != nil {
			field.Submit()
			w.refresh()
		}
	default:
		return false
	}
	// Restart now so the very next character goes to the new caret session.
	if next, ok := w.scene.find(w.scene.focus); w.err == nil && w.text.key == "" && ok && next.isText() && next.focusable() {
		w.startInput(next.node.key)
		_, w.err = w.text.composer.Update()
	}
	return true
}
