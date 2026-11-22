package desktop

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-text/typesetting/segmenter"
)

const textLineHeight = 22

type textBuffer struct {
	value               string
	stops, lines        []int
	cursor, anchor      int
	scrollX, scrollLine int
}

func cleanInput(value string, multiline bool) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			if multiline {
				return r
			}
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
}

func (b *textBuffer) sync(value string, multiline bool) {
	if b.stops != nil && b.value == value {
		return
	}
	value = cleanInput(value, multiline)
	if b.stops == nil {
		b.cursor, b.anchor = len(value), len(value)
	}
	b.index(value)
}

func (b *textBuffer) index(value string) {
	b.value = value
	b.stops = append(b.stops[:0], 0)
	b.lines = append(b.lines[:0], 0)
	if value != "" {
		var seg segmenter.Segmenter
		_ = seg.InitWithString(value)
		iter := seg.GraphemeIterator()
		offset := 0
		for iter.Next() {
			for _, r := range iter.Grapheme().Text {
				offset += utf8.RuneLen(r)
				if r == '\n' {
					b.lines = append(b.lines, offset)
				}
			}
			b.stops = append(b.stops, offset)
		}
	}
	b.cursor, b.anchor = b.boundary(b.cursor), b.boundary(b.anchor)
	b.scrollLine = min(b.scrollLine, len(b.lines)-1)
}

func (b *textBuffer) boundary(pos int) int {
	index, exact := slices.BinarySearch(b.stops, max(0, min(pos, len(b.value))))
	if !exact {
		index--
	}
	return b.stops[index]
}

func (b *textBuffer) step(pos, delta int) int {
	index, _ := slices.BinarySearch(b.stops, pos)
	return b.stops[max(0, min(len(b.stops)-1, index+delta))]
}

func (b *textBuffer) selection() (int, int) {
	return min(b.anchor, b.cursor), max(b.anchor, b.cursor)
}

func (b *textBuffer) move(pos int, extend bool) {
	b.cursor = b.boundary(pos)
	if !extend {
		b.anchor = b.cursor
	}
}

func (b *textBuffer) replace(start, end int, value string) bool {
	next := b.value[:start] + value + b.value[end:]
	b.cursor = start + len(value)
	b.anchor = b.cursor
	if next == b.value {
		return false
	}
	b.index(next)
	// A combining character may join following text. Keep the caret past it.
	if b.cursor < start+len(value) {
		b.move(b.step(b.cursor, 1), false)
	}
	return true
}

func (b *textBuffer) line(pos int) int {
	i, ok := slices.BinarySearch(b.lines, pos)
	if !ok {
		i--
	}
	return i
}

func (b *textBuffer) lineBounds(line int) (int, int) {
	start, end := b.lines[line], len(b.value)
	if line+1 < len(b.lines) {
		end = b.lines[line+1] - 1
	}
	return start, end
}

func (b *textBuffer) vertical(delta int) int {
	current := b.line(b.cursor)
	next := max(0, min(len(b.lines)-1, current+delta))
	from, _ := slices.BinarySearch(b.stops, b.lines[current])
	cursor, _ := slices.BinarySearch(b.stops, b.cursor)
	start, end := b.lineBounds(next)
	nextFrom, _ := slices.BinarySearch(b.stops, start)
	return min(end, b.stops[min(len(b.stops)-1, nextFrom+cursor-from)])
}
