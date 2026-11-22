package desktop

import (
	"image"
	"testing"
)

func TestMenuStepSkipsSeparatorsAndDisabledEntries(t *testing.T) {
	menu := Menu{Label: "Actions", Items: []MenuItem{
		{Label: "Rename", Action: func() {}},
		{}, // separator
		{Label: "Blocked", Disabled: true, Action: func() {}},
		{Label: "Delete", Action: func() {}},
	}}
	if menu.firstEnabled() != 0 {
		t.Fatalf("firstEnabled = %d, want 0", menu.firstEnabled())
	}
	if got := menu.step(0, 1); got != 3 {
		t.Fatalf("step down = %d, want 3 (skip separator and disabled)", got)
	}
	if got := menu.step(3, 1); got != 0 {
		t.Fatalf("step wrap = %d, want 0", got)
	}
	if got := menu.step(0, -1); got != 3 {
		t.Fatalf("step up from first = %d, want 3", got)
	}
	if !menu.focusable() {
		t.Fatal("a menu with enabled entries must be focusable")
	}
	if (Menu{Label: "Empty"}).focusable() ||
		(Menu{Label: "Blocked", Items: []MenuItem{{Label: "No", Disabled: true, Action: func() {}}}}).focusable() {
		t.Fatal("a menu without enabled entries must not be focusable")
	}
}

func TestMenuEntriesLayOutBelowTheButton(t *testing.T) {
	var s scene
	menu := Menu{Label: "Actions", Items: []MenuItem{
		{Label: "Rename", Action: func() {}},
		{},
		{Label: "Delete", Action: func() {}},
	}}
	if err := s.set(VStack(8, MenuButton("actions", menu)).Padding(10)); err != nil {
		t.Fatal(err)
	}
	s.layout(image.Pt(600, 400))
	item, ok := s.find("actions")
	if !ok {
		t.Fatal("the menu button was not placed")
	}
	control := item.controlBox()
	for i := range menu.Items {
		box := item.menuBox(i)
		if box.Min.X != control.Min.X || box.Dx() != control.Dx() || box.Dy() != 30 {
			t.Fatalf("menu entry %d = %v does not line up under %v", i, box, control)
		}
		if i > 0 && box.Min.Y != item.menuBox(i-1).Max.Y {
			t.Fatalf("menu entries %d and %d do not stack", i-1, i)
		}
	}
	if first := item.menuBox(0); first.Min.Y <= control.Max.Y {
		t.Fatal("the first menu entry overlaps the button")
	}
}
