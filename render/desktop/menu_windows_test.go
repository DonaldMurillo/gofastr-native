package desktop

import (
	"image"
	"testing"
)

func TestMenuRoutesClicksAndKeys(t *testing.T) {
	var log []string
	view := func() View {
		return View{Title: "Menu routing", Content: VStack(8, MenuButton("actions", Menu{
			Label: "Actions",
			Items: []MenuItem{
				{Label: "Rename", Action: func() { log = append(log, "rename") }},
				{}, // separator
				{Label: "Blocked", Disabled: true, Action: func() { log = append(log, "blocked") }},
				{Label: "Delete", Action: func() { log = append(log, "delete") }},
			},
		})).Padding(10)}
	}
	window, err := newGPUWindow(t.Context(), view)
	if err != nil {
		t.Fatal(err)
	}
	window.scene.layout(image.Pt(720, 400))
	center := func(r image.Rectangle) image.Point { return r.Min.Add(r.Size().Div(2)) }
	button, _ := window.scene.find("actions")

	window.click(center(button.controlBox()))
	if window.openMenu != "actions" || window.scene.focus != "actions" {
		t.Fatalf("open menu = %q focus = %q", window.openMenu, window.scene.focus)
	}
	item, _ := window.scene.find("actions")
	if item.state.menu != 0 {
		t.Fatalf("highlight = %d, want the first enabled entry", item.state.menu)
	}
	window.handleKeys(keyEvents{down: true})
	if item.state.menu != 3 {
		t.Fatalf("ArrowDown highlighted %d, want 3 (skip separator and disabled)", item.state.menu)
	}
	window.handleKeys(keyEvents{down: true})
	if item.state.menu != 0 {
		t.Fatalf("wrap highlight = %d, want 0", item.state.menu)
	}
	window.handleKeys(keyEvents{enter: true})
	if len(log) != 1 || log[0] != "rename" || window.openMenu != "" {
		t.Fatalf("enter picked %v with menu %q", log, window.openMenu)
	}

	window.click(center(button.controlBox()))
	window.handleKeys(keyEvents{escape: true})
	if window.openMenu != "" || len(log) != 1 {
		t.Fatal("Escape did not close the menu without an action")
	}

	window.click(center(button.controlBox()))
	window.click(center(button.menuBox(1)))
	if window.openMenu != "" || len(log) != 1 {
		t.Fatal("clicking the separator ran an action")
	}

	window.click(center(button.controlBox()))
	window.click(center(button.menuBox(2)))
	if len(log) != 1 || window.openMenu != "" {
		t.Fatal("the disabled entry ran its action")
	}

	window.handleKeys(keyEvents{enter: true}) // menu closed; button opens it again
	if window.openMenu != "actions" {
		t.Fatal("Enter on the focused menu button did not open it")
	}
	window.click(center(button.menuBox(3)))
	if len(log) != 2 || log[1] != "delete" {
		t.Fatalf("click picked %v", log)
	}
}
