package desktop

import (
	"image"
	"testing"
)

func TestTextBufferMovesAndDeletesGraphemes(t *testing.T) {
	var b textBuffer
	b.sync("e\u0301👩‍💻\r\nsecond", true)
	if len(b.lines) != 2 {
		t.Fatalf("lines = %v", b.lines)
	}
	b.move(0, false)
	b.move(b.step(b.cursor, 1), false)
	if b.cursor != len("e\u0301") {
		t.Fatal("caret stopped inside an accented character")
	}
	b.move(b.step(b.cursor, 1), true)
	start, end := b.selection()
	if b.value[start:end] != "👩‍💻" {
		t.Fatal("selection split an emoji sequence")
	}
	b.replace(start, end, "")
	if b.value != "e\u0301\nsecond" || b.cursor != len("e\u0301") {
		t.Fatalf("after delete: %#v", b)
	}
	b.move(b.vertical(1), false)
	if b.value[:b.cursor] != "e\u0301\ns" {
		t.Fatal("vertical move did not keep the character column")
	}
}

func TestTextInputNormalizesPastedControlCharacters(t *testing.T) {
	const value = "a\r\nb\tc\x00d"
	if got := cleanInput(value, false); got != "a b cd" {
		t.Fatalf("single line = %q", got)
	}
	if got := cleanInput(value, true); got != "a\nb\tcd" {
		t.Fatalf("multiline = %q", got)
	}
}

func TestTextSelectionSurvivesKeyedViewUpdate(t *testing.T) {
	var s scene
	field := Field{Label: "Title", Value: "Draft title", Change: func(string) {}}
	if err := s.set(TextInput("title", field)); err != nil {
		t.Fatal(err)
	}
	s.layout(image.Pt(300, 100))
	first, _ := s.find("title")
	first.state.edit.move(0, false)
	first.state.edit.move(5, true)
	field.Error = "Check the title."
	if err := s.set(VStack(4, Text("Draft"), TextInput("title", field))); err != nil {
		t.Fatal(err)
	}
	s.layout(image.Pt(300, 120))
	next, _ := s.find("title")
	start, end := next.state.edit.selection()
	if next.state.edit != first.state.edit || start != 0 || end != 5 || s.focus != "title" {
		t.Fatal("moving the field or showing validation reset keyboard focus or selection")
	}
}
