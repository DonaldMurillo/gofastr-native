package desktop

import (
	"image"
	"testing"
)

func dialogTree(confirming *bool) Node {
	base := VStack(8,
		PushButton("new", Button{Label: "New note", Press: func() {}}),
		PushButton("delete", Button{Label: "Delete", Press: func() { *confirming = true }}),
	).Padding(10)
	if !*confirming {
		return base
	}
	return DialogLayer("confirm", Dialog{
		Title: "Delete note", Content: Text("Delete this note?"),
		Confirm: "Delete", Cancel: "Keep",
		OnConfirm: func() { *confirming = false },
		OnCancel:  func() { *confirming = false },
	}, base)
}

func TestDialogContainsAndRestoresFocus(t *testing.T) {
	var s scene
	confirming := false
	size := image.Pt(720, 400)
	if err := s.set(dialogTree(&confirming)); err != nil {
		t.Fatal(err)
	}
	s.layout(size)
	s.focus = "delete"

	confirming = true
	if err := s.set(dialogTree(&confirming)); err != nil {
		t.Fatal(err)
	}
	s.layout(size)
	if _, open := s.topDialog(); !open {
		t.Fatal("the dialog layer was not placed")
	}
	if s.focus != "confirm-cancel" {
		t.Fatalf("focus = %q, want the dialog cancel button", s.focus)
	}
	s.tab(false)
	if s.focus != "confirm-confirm" {
		t.Fatalf("Tab in the dialog moved to %q", s.focus)
	}
	s.tab(false)
	if s.focus != "confirm-cancel" {
		t.Fatalf("Tab did not wrap inside the dialog: %q", s.focus)
	}
	s.tab(true)
	if s.focus != "confirm-confirm" {
		t.Fatalf("Shift-Tab in the dialog moved to %q", s.focus)
	}

	confirming = false
	if err := s.set(dialogTree(&confirming)); err != nil {
		t.Fatal(err)
	}
	if s.focus != "delete" {
		t.Fatalf("focus = %q, want restore to the delete button", s.focus)
	}
	if len(s.restore) != 0 {
		t.Fatal("closing the dialog left restore state behind")
	}
	s.layout(size)
	if _, open := s.topDialog(); open {
		t.Fatal("the dialog stayed open after leaving the tree")
	}
}

func TestDialogKeepsFocusWhenTheTreeIsRebuilt(t *testing.T) {
	var s scene
	confirming := false
	size := image.Pt(720, 400)
	if err := s.set(dialogTree(&confirming)); err != nil {
		t.Fatal(err)
	}
	s.layout(size)
	s.focus = "delete"

	confirming = true
	if err := s.set(dialogTree(&confirming)); err != nil {
		t.Fatal(err)
	}
	s.layout(size)
	s.focus = "confirm-confirm"
	for range 3 {
		if err := s.set(dialogTree(&confirming)); err != nil {
			t.Fatal(err)
		}
		s.layout(size)
		if s.focus != "confirm-confirm" {
			t.Fatalf("rebuilding the dialog moved focus to %q", s.focus)
		}
	}
	if s.restore["confirm"] != "delete" {
		t.Fatalf("rebuilds overwrote the captured focus: %q", s.restore["confirm"])
	}
	confirming = false
	if err := s.set(dialogTree(&confirming)); err != nil {
		t.Fatal(err)
	}
	if s.focus != "delete" {
		t.Fatalf("focus = %q, want delete after the rebuilt dialog closed", s.focus)
	}
}

func TestNestedDialogFocusesItsOwnLayer(t *testing.T) {
	var s scene
	base := Text("background")
	inner := DialogLayer("inner", Dialog{Title: "Inner", Confirm: "OK", OnConfirm: func() {}}, Text("inner base"))
	tree := DialogLayer("outer", Dialog{Title: "Outer", Content: inner}, base)
	if err := s.set(tree); err != nil {
		t.Fatal(err)
	}
	size := image.Pt(720, 400)
	s.layout(size)
	dialog, ok := s.topDialog()
	if !ok || dialog.node.key != "inner" {
		t.Fatalf("topmost dialog = %v, want inner", dialog.node.key)
	}
	if s.focus != "inner-confirm" {
		t.Fatalf("focus = %q, want the inner dialog button", s.focus)
	}
	s.tab(false)
	if s.focus != "inner-confirm" {
		t.Fatalf("Tab left the inner dialog: %q", s.focus)
	}
	if err := s.set(DialogLayer("outer", Dialog{Title: "Outer", Content: Text("no inner")}, base)); err != nil {
		t.Fatal(err)
	}
	s.layout(size)
	if dialog, _ := s.topDialog(); dialog.node.key != "outer" {
		t.Fatalf("topmost dialog = %q, want outer", dialog.node.key)
	}
	if s.focus != "outer-confirm" && s.focus != "" {
		t.Fatalf("focus = %q, want it back in the outer layer", s.focus)
	}
}

func TestDialogPanelBoxCentersAndClamps(t *testing.T) {
	panel := VStack(10,
		Heading("Title"),
		Text("Body"),
		HStack(8, Node{}, PushButton("ok-confirm", Button{Label: "OK"}).Width(120)).Height(34),
	).Padding(18)
	if got := naturalHeight(panel); got != 44+28+34+2*10+2*18 {
		t.Fatalf("naturalHeight = %d", got)
	}
	node := Node{kind: dialogNode, value: Dialog{}, children: []Node{{}, panel}}
	got := dialogPanelBox(image.Rect(0, 0, 800, 600), node)
	if got.Dx() != 380 || got.Min.X != (800-380)/2 {
		t.Fatalf("width %d at %d, want 380 centered", got.Dx(), got.Min.X)
	}
	if got.Dy() != 162 || got.Min.Y != (600-162)/2 {
		t.Fatalf("height %d at %d, want 162 centered", got.Dy(), got.Min.Y)
	}
	small := dialogPanelBox(image.Rect(0, 0, 100, 80), node)
	if small.Dx() != 52 || small.Dy() != 32 {
		t.Fatalf("clamped panel = %v, want 52x32", small)
	}
	if !small.In(image.Rect(0, 0, 100, 80)) {
		t.Fatal("the clamped panel leaves the window")
	}
}

func TestDialogReservesItsButtonKeys(t *testing.T) {
	var s scene
	tree := DialogLayer("x", Dialog{Confirm: "OK", Cancel: "No"},
		PushButton("x-confirm", Button{Label: "Clash", Press: func() {}}))
	if err := s.set(tree); err == nil {
		t.Fatal("a dialog button key collision was accepted")
	}
}
