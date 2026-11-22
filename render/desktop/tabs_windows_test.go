package desktop

import (
	"image"
	"testing"
)

func TestTabStripRoutesClicksAndKeys(t *testing.T) {
	selected, changes := 1, 0
	view := func() View {
		return View{Title: "Tabs routing", Content: TabStrip("tabs",
			Tabs{Labels: []string{"Inbox", "Drafts", "Archive"}, Selected: selected,
				Change: func(index int) { selected, changes = index, changes+1 }},
			Text("panel")).Padding(10)}
	}
	window, err := newGPUWindow(t.Context(), view)
	if err != nil {
		t.Fatal(err)
	}
	window.scene.layout(image.Pt(720, 400))
	strip, _ := window.scene.find("tabs")
	center := func(r image.Rectangle) image.Point { return r.Min.Add(r.Size().Div(2)) }

	window.click(center(tabSegment(strip, 2)))
	if selected != 2 || changes != 1 || window.scene.focus != "tabs" {
		t.Fatalf("click moved selection to %d with %d changes and focus %q", selected, changes, window.scene.focus)
	}
	strip, _ = window.scene.find("tabs")
	window.click(center(tabSegment(strip, 2)))
	if changes != 1 {
		t.Fatal("re-selecting the active tab fired Change again")
	}

	window.handleKeys(keyEvents{left: true})
	if selected != 1 || changes != 2 {
		t.Fatalf("ArrowLeft selected %d with %d changes", selected, changes)
	}
	window.handleKeys(keyEvents{home: true})
	if selected != 0 {
		t.Fatalf("Home selected %d", selected)
	}
	window.handleKeys(keyEvents{left: true})
	if selected != 0 || changes != 3 {
		t.Fatal("ArrowLeft past the first tab moved the selection")
	}
	window.handleKeys(keyEvents{end: true})
	if selected != 2 {
		t.Fatalf("End selected %d", selected)
	}
	window.handleKeys(keyEvents{enter: true})
	if changes != 4 {
		t.Fatal("Enter on the active tab fired an extra change")
	}
	window.handleKeys(keyEvents{up: true})
	if changes != 4 {
		t.Fatal("an unhandled key changed the selection")
	}
}
