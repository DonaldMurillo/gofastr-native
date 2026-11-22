package desktop

import (
	"image"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

var fieldError = color.RGBA{250, 156, 147, 255}

func (w *gpuWindow) drawField(p gpuPainter, item placedNode, focused bool) {
	field := item.node.value.(Field)
	edit, clip := item.editBox(), item.textClip()
	b := item.state.edit
	outline, ink := border, textColor
	switch {
	case field.Error != "":
		outline = fieldError
	case focused && item.focusable():
		outline = accent
	}
	if !item.focusable() {
		ink = disabled
	}
	if field.Label != "" {
		p.label(image.Rect(item.box.Min.X, item.box.Min.Y, item.box.Max.X, edit.Min.Y), field.Label, w.paintTheme.heading, ink, 0)
	}
	if field.Error != "" {
		p.label(image.Rect(item.box.Min.X, edit.Max.Y, item.box.Max.X, item.box.Max.Y), field.Error, w.paintTheme.body, fieldError, 0)
	}
	p.fill(edit, outline)
	p.fill(edit.Inset(1), panel)
	if clip.Empty() {
		return
	}
	composing := w.text.key == item.node.key && w.text.preedit != ""
	if b.value == "" && !composing && field.Placeholder != "" {
		p.drawText(clip, field.Placeholder, w.paintTheme.mono, muted, float64(clip.Min.X), float64(clip.Min.Y))
	}
	start, end := b.selection()
	x := clip.Min.X - b.scrollX
	caretLine := b.line(b.cursor)
	for line := b.scrollLine; line < min(len(b.lines), b.scrollLine+visibleTextLines(item)+1); line++ {
		lo, hi := b.lineBounds(line)
		y := clip.Min.Y + (line-b.scrollLine)*textLineHeight
		value := b.value[lo:hi]
		drawCaret := focused && item.focusable() && caretLine == line
		caretX := x
		if drawCaret {
			caretX += int(text.Advance(b.value[lo:b.cursor], w.theme.mono))
		}
		var preeditX, clauseLeft, clauseRight int
		if composing && drawCaret {
			// The preedit takes the place of the selection's slice of this
			// line, so a selection that spans lines still shows the
			// composition on the caret's line.
			var at int
			value, at = composedLine(value, w.text.preedit, start, end, lo, hi)
			preeditX = x + int(text.Advance(value[:at], w.theme.mono))
			caretX = preeditX + int(text.Advance(w.text.preedit, w.theme.mono))
			p.fill(image.Rect(preeditX, y+18, caretX, y+20).Intersect(clip), accent)
			// Highlight the clause the IME is currently converting.
			clauseLeft = preeditX + int(text.Advance(w.text.preedit[:w.text.preeditStart], w.theme.mono))
			clauseRight = preeditX + int(text.Advance(w.text.preedit[:w.text.preeditEnd], w.theme.mono))
			if clauseRight > clauseLeft {
				p.fill(image.Rect(clauseLeft, y+2, clauseRight, y+18).Intersect(clip), selected)
			}
		} else if focused && start < end && start <= hi && end > lo {
			from, to := max(lo, start), min(hi, end)
			left := x + int(text.Advance(b.value[lo:from], w.theme.mono))
			right := x + int(text.Advance(b.value[lo:to], w.theme.mono))
			if end > hi {
				right += 6
			}
			p.fill(image.Rect(left, y, right, y+textLineHeight).Intersect(clip), selected)
		}
		p.drawText(clip, value, w.paintTheme.mono, ink, float64(x), float64(y))
		if drawCaret {
			p.fill(image.Rect(caretX, y+2, caretX+1, y+18).Intersect(clip), accent)
		}
	}
}

// composedLine returns the text to draw on one line while a preedit is in
// progress, and the byte offset within that text where the preedit begins.
// lo and hi are the line's bounds in the buffer holding the selection
// [start, end); the preedit replaces the selection's slice of the line.
func composedLine(line, preedit string, start, end, lo, hi int) (string, int) {
	from := min(hi, max(start, lo))
	to := max(from, min(end, hi))
	return line[:from-lo] + preedit + line[to-lo:], from - lo
}

func (w *gpuWindow) drawToggle(p gpuPainter, item placedNode, focused bool) {
	toggle := item.node.value.(Toggle)
	box := item.controlBox()
	ink, outline := textColor, border
	if !item.focusable() {
		ink = disabled
	}
	if focused && item.focusable() {
		outline = accent
	}
	y := box.Min.Y + (box.Dy()-18)/2
	width := 18
	if item.node.kind == switchNode {
		width = 32
	}
	check := image.Rect(box.Min.X, y, box.Min.X+width, y+18).Intersect(box)
	p.fill(check, outline)
	p.fill(check.Inset(1), control)
	if !check.Empty() {
		if item.node.kind == switchNode {
			x := check.Min.X + 4
			if toggle.Checked {
				x = check.Max.X - 14
			}
			p.fill(image.Rect(x, y+4, x+10, y+14).Intersect(check), ink)
		} else if toggle.Checked {
			p.fill(check.Inset(4), accent)
		}
	}
	box.Min.X += width + 8
	p.label(box, toggle.Label, w.paintTheme.body, ink, 0)
}
