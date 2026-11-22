package desktop

import (
	"image"
	"testing"
)

func TestTabStripPlacesHeaderAboveContent(t *testing.T) {
	var s scene
	root := TabStrip("tabs", Tabs{Labels: []string{"One", "Two", "Three"}, Selected: 1,
		Change: func(int) {}},
		PushButton("inside", Button{Label: "OK", Press: func() {}})).Padding(4)
	if err := s.set(root); err != nil {
		t.Fatal(err)
	}
	size := image.Pt(600, 400)
	s.layout(size)
	strip, ok := s.find("tabs")
	if !ok {
		t.Fatal("the tab strip was not placed")
	}
	header := tabsHeader(strip)
	if header.Min != strip.box.Min || header.Dy() != tabsHeaderHeight || header.Dx() != strip.box.Dx() {
		t.Fatalf("unexpected tab header %v inside %v", header, strip.box)
	}
	for i := range 3 {
		segment := tabSegment(strip, i)
		if segment.Dy() != header.Dy() || !segment.In(header) {
			t.Fatalf("segment %d = %v does not fill the header", i, segment)
		}
		if i > 0 && segment.Min.X != tabSegment(strip, i-1).Max.X {
			t.Fatalf("segments %d and %d leave a gap", i-1, i)
		}
	}
	if tabSegment(strip, 2).Max.X != header.Max.X {
		t.Fatal("the last segment does not reach the header edge")
	}
	button, _ := s.find("inside")
	if button.box.Min.Y < header.Max.Y || !button.box.In(strip.box) {
		t.Fatal("tab content overlaps the header or leaves the strip")
	}
}

func TestTabStripWithoutChangeTakesNoFocus(t *testing.T) {
	var s scene
	if err := s.set(TabStrip("tabs", Tabs{Labels: []string{"Only"}}, Text("body"))); err != nil {
		t.Fatal(err)
	}
	s.layout(image.Pt(600, 400))
	item, _ := s.find("tabs")
	if item.focusable() {
		t.Fatal("a strip without Change must not take focus")
	}
	state := s.states["tabs"]
	if err := s.set(VStack(0, Text("first"), TabStrip("tabs", Tabs{Labels: []string{"Only"}, Change: func(int) {}}, Text("body")))); err != nil {
		t.Fatal(err)
	}
	s.layout(image.Pt(600, 400))
	if s.states["tabs"] != state {
		t.Fatal("the strip lost its retained state when it moved")
	}
}
