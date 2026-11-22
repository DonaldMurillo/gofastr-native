package desktop

import (
	"image"
	"strings"
	"testing"
)

// newIMETestWindow builds a window whose view is rebuilt from state so
// commits driven through the IME seams observe the same flow as the game
// loop: Change fires, the view refreshes, and the buffer keeps its selection.
func newIMETestWindow(t *testing.T, build func() Node) (*gpuWindow, func(key string)) {
	t.Helper()
	window, err := newGPUWindow(t.Context(), func() View {
		return View{Title: "IME test", Content: build()}
	})
	if err != nil {
		t.Fatal(err)
	}
	window.scene.layout(image.Pt(640, 480))
	return window, func(key string) {
		window.scene.focus = key
		window.startInput(key)
	}
}

func focusedBuffer(t *testing.T, w *gpuWindow, key string) *textBuffer {
	t.Helper()
	item, ok := w.scene.find(key)
	if !ok || !item.isText() {
		t.Fatalf("field %q is not a text node", key)
	}
	return item.state.edit
}

func TestIMECompositionShowsClauseAndCancels(t *testing.T) {
	value := ""
	w, focus := newIMETestWindow(t, func() Node {
		return TextInput("title", Field{Value: value, Change: func(next string) { value = next }})
	})
	focus("title")

	// "にほんご": the IME reports the first two characters as the clause
	// being converted.
	w.preeditChanged("title", "にほんご", 0, 6)
	if w.text.preedit != "にほんご" || w.text.preeditStart != 0 || w.text.preeditEnd != 6 {
		t.Fatalf("preedit state = %q [%d,%d)", w.text.preedit, w.text.preeditStart, w.text.preeditEnd)
	}
	if value != "" {
		t.Fatalf("composition leaked into the committed value: %q", value)
	}

	// The IME cancelled: an empty composition clears the preedit.
	w.preeditChanged("title", "", 9, 9)
	if w.text.preedit != "" || w.text.preeditStart != 0 || w.text.preeditEnd != 0 {
		t.Fatalf("cancel left preedit state %q [%d,%d)", w.text.preedit, w.text.preeditStart, w.text.preeditEnd)
	}
}

func TestIMECompositionClampsUntrustedClauseRange(t *testing.T) {
	w, focus := newIMETestWindow(t, func() Node { return TextInput("title", Field{}) })
	focus("title")
	w.preeditChanged("title", "にほん", 9, -4) // にほん is nine UTF-8 bytes
	if w.text.preeditStart != 0 || w.text.preeditEnd != 9 {
		t.Fatalf("clause range = [%d,%d), want [0,9)", w.text.preeditStart, w.text.preeditEnd)
	}
	w.preeditChanged("title", "にほん", 8, 2)
	if w.text.preeditStart != 2 || w.text.preeditEnd != 8 {
		t.Fatalf("clause range = [%d,%d), want [2,8)", w.text.preeditStart, w.text.preeditEnd)
	}
}

func TestIMECommitInsertsAtCaret(t *testing.T) {
	value := ""
	w, focus := newIMETestWindow(t, func() Node {
		return TextInput("title", Field{Value: value, Change: func(next string) { value = next }})
	})
	focus("title")
	w.commitInput("title", "日本語", false, "", "")
	b := focusedBuffer(t, w, "title")
	if value != "日本語" || b.value != "日本語" {
		t.Fatalf("committed value = %q", value)
	}
	if start, end := b.selection(); start != end || start != len("日本語") {
		t.Fatalf("caret after commit = [%d,%d)", start, end)
	}
	if w.text.key != "title" || w.text.preedit != "" {
		t.Fatal("commit state not retained for the next session")
	}
}

func TestIMECommitReplacesSelection(t *testing.T) {
	value := "hello world"
	w, focus := newIMETestWindow(t, func() Node {
		return TextInput("title", Field{Value: value, Change: func(next string) { value = next }})
	})
	focus("title")
	b := focusedBuffer(t, w, "title")
	b.move(6, false)
	b.move(11, true)
	w.commitInput("title", "世界", false, "", "")
	if value != "hello 世界" {
		t.Fatalf("value = %q", value)
	}
	if b.cursor != len("hello 世界") || b.anchor != b.cursor {
		t.Fatalf("caret = %d anchor = %d", b.cursor, b.anchor)
	}
}

func TestIMECommitReplacesSurroundingTextRange(t *testing.T) {
	value := "abcdef"
	w, focus := newIMETestWindow(t, func() Node {
		return TextInput("title", Field{Value: value, Change: func(next string) { value = next }})
	})
	focus("title")
	b := focusedBuffer(t, w, "title")
	b.move(3, false) // a session that started with the caret inside the word
	// The IME kept prefix "ab" and suffix "f" and asks to replace between them.
	w.commitInput("title", "X", true, "ab", "f")
	if value != "abXf" {
		t.Fatalf("value = %q, want abXf", value)
	}
	if b.cursor != len("abX") {
		t.Fatalf("cursor = %d", b.cursor)
	}
}

func TestIMECommitDropsStaleSurroundingRange(t *testing.T) {
	value := "ab"
	w, focus := newIMETestWindow(t, func() Node {
		return TextInput("title", Field{Value: value, Change: func(next string) { value = next }})
	})
	focus("title")
	// The buffer shrank after the session started, so the derived range no
	// longer fits and the commit must be dropped instead of corrupting text.
	w.commitInput("title", "X", true, "abcdef", "")
	if value != "ab" {
		t.Fatalf("stale commit changed the value to %q", value)
	}
}

func TestIMECommitIntoMissingFieldIsDropped(t *testing.T) {
	value := "same"
	w, _ := newIMETestWindow(t, func() Node {
		return TextInput("title", Field{Value: value, Change: func(next string) { value = next }})
	})
	w.commitInput("gone", "日本語", false, "", "")
	if value != "same" {
		t.Fatal("a commit for a removed field changed the value")
	}
}

func TestIMECommitCleansControlCharacters(t *testing.T) {
	value := ""
	w, focus := newIMETestWindow(t, func() Node {
		return TextInput("title", Field{Value: value, Change: func(next string) { value = next }})
	})
	focus("title")
	w.commitInput("title", "a\r\nb\tc\x00", false, "", "")
	if value != "a b c" {
		t.Fatalf("single-line commit = %q", value)
	}
}

func TestIMEPreeditFollowsCaretLineOfMultiLineSelection(t *testing.T) {
	for _, test := range []struct {
		name                     string
		line                     string
		start, end, lo, hi, want int
		preedit                  string
	}{
		{name: "caret at line end", line: "hello", start: 5, end: 5, lo: 0, hi: 5, preedit: "に", want: 5},
		{name: "mid-line selection", line: "hello world", start: 2, end: 5, lo: 0, hi: 11, preedit: "世", want: 2},
		{name: "selection spans from an earlier line", line: "abc", start: 0, end: 9, lo: 4, hi: 7, preedit: "世", want: 0},
		{name: "selection starts on this line and continues", line: "abcde", start: 6, end: 9, lo: 4, hi: 9, preedit: "世", want: 2},
		{name: "range past the line is clamped", line: "abc", start: 8, end: 9, lo: 0, hi: 3, preedit: "世", want: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			drawn, at := composedLine(test.line, test.preedit, test.start, test.end, test.lo, test.hi)
			if at != test.want {
				t.Fatalf("preedit offset = %d, want %d", at, test.want)
			}
			if !strings.HasPrefix(drawn[at:], test.preedit) {
				t.Fatalf("drawn line %q does not place the preedit at %d", drawn, at)
			}
		})
	}
}

func TestIMEPreeditRevealsCaret(t *testing.T) {
	value := ""
	w, focus := newIMETestWindow(t, func() Node {
		return VStack(4,
			TextInput("title", Field{Value: value, Change: func(next string) { value = next }}).Width(120),
			TextArea("body", Field{Value: "a\nb\nc\nd\ne", Change: func(string) {}}).Height(100),
		)
	})
	focus("title")
	w.preeditChanged("title", "abcdefghijklmnopqrstuvwxyz", 0, 0) // romaji preedit
	if b := focusedBuffer(t, w, "title"); b.scrollX <= 0 {
		t.Fatalf("horizontal scroll = %d, want the preedit kept visible", b.scrollX)
	}

	focus("body")
	b := focusedBuffer(t, w, "body")
	b.move(len("a\nb\nc\nd\ne"), false) // caret on the last line
	w.preeditChanged("body", "にほん", 0, 6)
	if b.scrollLine < 1 {
		t.Fatalf("vertical scroll = %d, want the composed line scrolled in", b.scrollLine)
	}
}

func TestIMEFocusSwitchRestartsSession(t *testing.T) {
	value := ""
	w, focus := newIMETestWindow(t, func() Node {
		return VStack(4,
			TextInput("first", Field{Value: value, Change: func(next string) { value = next }}),
			TextInput("second", Field{Value: value, Change: func(next string) { value = next }}),
		)
	})
	focus("first")
	w.preeditChanged("first", "にほん", 0, 6)
	w.confirmInput()
	if w.text.key != "" || w.text.preedit != "" {
		t.Fatal("confirm did not reset the input state")
	}
	focus("second")
	if w.text.key != "second" {
		t.Fatalf("session key = %q, want second", w.text.key)
	}
}
