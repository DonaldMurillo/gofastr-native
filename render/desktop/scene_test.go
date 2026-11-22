package desktop

import (
	"image"
	"testing"
)

func TestSceneReordersKeyedControlsDuringReplay(t *testing.T) {
	var s scene
	table := Table{Key: "all", Count: 1_000_000, Select: func(int) {}}
	left := VirtualTable("left", table)
	right := VirtualTable("right", table)
	size := image.Pt(900, 500)
	if err := s.set(Split("split", 65, left, right)); err != nil {
		t.Fatal(err)
	}
	s.layout(size)
	a, _ := s.find("left")
	b, _ := s.find("right")
	s.states["split"].split = 42
	a.state.viewport.scroll(140)
	b.state.viewport.scroll(30)
	s.focus = "right"

	table.Count += 50_000
	root := Split("split", 65, VirtualTable("right", table), Panel(VirtualTable("left", table)))
	if err := s.set(root); err != nil {
		t.Fatal(err)
	}
	s.layout(size)
	nextA, _ := s.find("left")
	nextB, _ := s.find("right")
	if nextA.box.Min.X <= nextB.box.Min.X || s.states["split"].split != 42 {
		t.Fatal("the new order or the retained split size was lost")
	}
	if nextA.state != a.state || nextB.state != b.state ||
		nextA.state.viewport.top != 140 || nextB.state.viewport.top != 30 ||
		nextA.state.viewport.count != table.Count || s.focus != "right" {
		t.Fatal("the tables lost their independent viewport or focus")
	}
	s.tab(false)
	if s.focus != "left" {
		t.Fatalf("Tab used the old layout order: focus = %q", s.focus)
	}

	if err := s.set(VStack(0, PushButton("left", Button{Label: "Reset", Press: func() {}}))); err != nil {
		t.Fatal(err)
	}
	s.layout(size)
	if s.states["right"] != nil || s.states["split"] != nil || s.states["left"] == a.state {
		t.Fatal("removed or replaced controls still have retained state")
	}
	if s.focus != "left" {
		t.Fatal("focus did not recover to the remaining button")
	}
}

func TestSceneClampsSmallLayoutAndRejectsDuplicateKeys(t *testing.T) {
	var s scene
	if err := s.set(VStack(8,
		HStack(8, PushButton("one", Button{}).Width(120), SelectBox("two", Choice{})).Height(40),
		Panel(Paint(nil)),
	).Padding(4)); err != nil {
		t.Fatal(err)
	}
	for _, size := range []image.Point{image.Pt(720, 400), image.Pt(10, 10), image.Pt(1, 1)} {
		s.layout(size)
		for _, item := range s.items {
			if item.box.Empty() || !item.box.In(image.Rectangle{Max: size}) {
				t.Fatalf("layout for %v placed a node outside the window: %v", size, item.box)
			}
		}
	}
	if err := s.set(HStack(0, PushButton("same", Button{}), VirtualTable("same", Table{}))); err == nil {
		t.Fatal("duplicate interactive keys were accepted")
	}
	if s.states["one"] == nil || s.states["same"] != nil {
		t.Fatal("a rejected tree replaced the retained state")
	}
}
