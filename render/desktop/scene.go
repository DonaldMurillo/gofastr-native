package desktop

import (
	"fmt"
	"image"
)

type controlState struct {
	kind     nodeKind
	viewport viewport
	split    int
	wheel    float64
	choice   int
	menu     int
	edit     *textBuffer
}

type placedNode struct {
	node  Node
	box   image.Rectangle
	split image.Rectangle // the full split area, or the dialog panel
	state *controlState
	// dialog names the modal layer this node belongs to. It is empty for
	// nodes outside any dialog.
	dialog string
}

type scene struct {
	root     Node
	states   map[string]*controlState
	items    []placedNode
	dialogs  []placedNode
	size     image.Point
	dirty    bool
	focus    string
	restore  map[string]string // dialog key to the focus to bring back on close
	inDialog string
}

// set reconciles control state by key. Reordering a node keeps its state.
// Removing a node or changing its kind retires that state.
func (s *scene) set(root Node) error {
	next := make(map[string]*controlState, len(s.states))
	var visit func(Node) error
	visit = func(node Node) error {
		switch node.kind {
		case splitNode, buttonNode, choiceNode, tableNode, detailNode, inputNode, areaNode, checkboxNode, switchNode, tabsNode, menuNode, dialogNode:
			if node.key == "" {
				return fmt.Errorf("desktop: interactive nodes require a key")
			}
			if _, found := next[node.key]; found {
				return fmt.Errorf("desktop: duplicate node key %q", node.key)
			}
			state := s.states[node.key]
			if state == nil || state.kind != node.kind {
				state = &controlState{kind: node.kind, split: node.ratio}
			}
			next[node.key] = state
		}
		for _, child := range node.children {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(root); err != nil {
		return err
	}
	// A dialog that left the tree gives the focus back it captured on open.
	for key, back := range s.restore {
		if state := next[key]; state == nil || state.kind != dialogNode {
			delete(s.restore, key)
			if back != "" && next[back] != nil {
				s.focus = back
			}
		}
	}
	// A dialog that just appeared remembers where the focus came from.
	for key, state := range next {
		if state.kind == dialogNode && s.states[key] == nil {
			if s.restore == nil {
				s.restore = make(map[string]string)
			}
			s.restore[key] = s.focus
		}
	}
	if next[s.focus] != s.states[s.focus] {
		s.focus = ""
	}
	s.root, s.states, s.dirty = root, next, true
	return nil
}

func (s *scene) layout(size image.Point) {
	if s.size == size && !s.dirty {
		return
	}
	s.size, s.dirty = size, false
	clear(s.items)
	s.items = s.items[:0]
	window := image.Rectangle{Max: size}
	s.place(s.root, window)
	for i := 0; i < len(s.dialogs); i++ {
		item := s.dialogs[i]
		item.box, item.split = window, dialogPanelBox(window, item.node)
		s.items = append(s.items, item)
		s.inDialog = item.node.key
		s.place(item.node.children[1], item.split)
	}
	s.inDialog = ""
	clear(s.dialogs)
	s.dialogs = s.dialogs[:0]
	// While a dialog is open, the focus stays inside it.
	layer := ""
	if dialog, ok := s.topDialog(); ok {
		layer = dialog.node.key
	}
	focused, visible := s.find(s.focus)
	if visible && focused.dialog != layer {
		visible = false
	}
	if !visible {
		s.focus = ""
		for _, item := range s.items {
			if item.node.kind == tableNode && item.focusable() && item.dialog == layer {
				s.focus = item.node.key
				return
			}
		}
		s.tab(false)
	}
}

// topDialog reports the topmost open dialog, the last one placed.
func (s *scene) topDialog() (placedNode, bool) {
	for i := len(s.items) - 1; i >= 0; i-- {
		if s.items[i].node.kind == dialogNode {
			return s.items[i], true
		}
	}
	return placedNode{}, false
}

const dividerWidth = 12

func clampSplit(ratio int) int { return max(20, min(80, ratio)) }

func (s *scene) place(node Node, box image.Rectangle) {
	if node.kind == emptyNode || (box.Empty() && node.kind != dialogNode) {
		return
	}
	if node.width > 0 {
		box.Max.X = min(box.Max.X, box.Min.X+node.width)
	}
	if node.height > 0 {
		box.Max.Y = min(box.Max.Y, box.Min.Y+node.height)
	}
	if node.padding > 0 {
		box = box.Inset(min(node.padding, min(box.Dx(), box.Dy())/2))
	}
	if box.Empty() && node.kind != dialogNode {
		return
	}
	item := placedNode{node: node, box: box, state: s.states[node.key], dialog: s.inDialog}
	switch node.kind {
	case horizontalNode, verticalNode:
		s.placeStack(node, box)
	case splitNode:
		gap := min(dividerWidth, box.Dx())
		x := box.Min.X + (box.Dx()-gap)*item.state.split/100
		item.box, item.split = image.Rect(x, box.Min.Y, x+gap, box.Max.Y), box
		s.place(node.children[0], image.Rect(box.Min.X, box.Min.Y, x, box.Max.Y))
		s.items = append(s.items, item)
		s.place(node.children[1], image.Rect(x+gap, box.Min.Y, box.Max.X, box.Max.Y))
	case panelNode:
		s.items = append(s.items, item)
		s.place(node.children[0], box)
	case tabsNode:
		s.items = append(s.items, item)
		header := tabsHeader(item)
		s.place(node.children[0], image.Rect(box.Min.X, header.Max.Y, box.Max.X, box.Max.Y))
	case dialogNode:
		// Paint the base with its siblings and defer the window-wide overlay
		// until after all content on this layer.
		s.place(node.children[0], box)
		item.dialog = node.key
		s.dialogs = append(s.dialogs, item)
	default:
		if node.kind == tableNode {
			rows, _ := item.tableBounds()
			item.state.viewport.update(node.value.(Table), rows.Dy()/rowHeight)
		}
		if node.kind == inputNode || node.kind == areaNode {
			if item.state.edit == nil {
				item.state.edit = &textBuffer{}
			}
			item.state.edit.sync(node.value.(Field).Value, node.kind == areaNode)
		}
		s.items = append(s.items, item)
	}
}

func (s *scene) placeStack(node Node, box image.Rectangle) {
	horizontal := node.kind == horizontalNode
	total := box.Dy()
	if horizontal {
		total = box.Dx()
	}
	gap := min(node.gap, total/max(1, len(node.children)-1))
	remaining := total - gap*max(0, len(node.children)-1)
	fixed, flexible := 0, 0
	for _, child := range node.children {
		if size := child.request(horizontal); size > 0 {
			fixed += size
		} else {
			flexible++
		}
	}
	flexSpace := max(0, remaining-fixed)
	for _, child := range node.children {
		size := child.request(horizontal)
		if size == 0 {
			size = flexSpace / flexible
			flexSpace -= size
			flexible--
		}
		size = min(size, remaining)
		remaining -= size
		bounds := box
		if horizontal {
			bounds.Max.X = bounds.Min.X + size
			box.Min.X = bounds.Max.X + gap
		} else {
			bounds.Max.Y = bounds.Min.Y + size
			box.Min.Y = bounds.Max.Y + gap
		}
		s.place(child, bounds)
	}
}

func (n Node) request(horizontal bool) int {
	if horizontal {
		if n.width > 0 {
			return n.width
		}
		switch n.kind {
		case dialogNode:
			return n.children[0].request(true)
		case buttonNode:
			return 160
		case choiceNode, menuNode:
			return 134
		}
		return 0
	}
	if n.height > 0 {
		return n.height
	}
	switch n.kind {
	case dialogNode:
		return n.children[0].request(false)
	case headingNode:
		return 44
	case buttonNode, choiceNode, menuNode:
		return 34
	case textNode, statusNode:
		return 28
	case inputNode:
		if n.value.(Field).Error != "" {
			return 80
		}
		return 58
	case checkboxNode, switchNode:
		return 30
	}
	return 0
}

func (p placedNode) tableBounds() (rows, scroll image.Rectangle) {
	r := p.box
	y := min(r.Max.Y, r.Min.Y+30)
	right := max(r.Min.X, r.Max.X-12)
	bottom := y + (r.Max.Y-y)/rowHeight*rowHeight
	return image.Rect(r.Min.X, y, right, bottom), image.Rect(right, y, r.Max.X, r.Max.Y)
}

func (p placedNode) controlBox() image.Rectangle {
	r := p.box
	r.Min.Y += max(0, (r.Dy()-34)/2)
	r.Max.Y = min(r.Max.Y, r.Min.Y+34)
	return r
}

func (p placedNode) optionBox(i int) image.Rectangle {
	r := p.controlBox()
	y := r.Max.Y + 2 + i*30
	return image.Rect(r.Min.X, y, r.Max.X, y+30)
}

func (p placedNode) copyBox() image.Rectangle {
	r := p.box
	x := min(r.Max.X, r.Min.X+14)
	y := max(r.Min.Y, r.Max.Y-46)
	return image.Rect(x, y, max(x, min(r.Max.X-14, x+133)), max(y, r.Max.Y-12))
}

func (p placedNode) focusable() bool {
	switch p.node.kind {
	case buttonNode:
		b := p.node.value.(Button)
		return b.Press != nil && !b.Disabled
	case choiceNode:
		c := p.node.value.(Choice)
		return c.Change != nil && len(c.Options) > 0
	case tableNode:
		t := p.node.value.(Table)
		return t.Select != nil || t.Copy != ""
	case detailNode:
		return p.node.value.(Detail).Copy != ""
	case inputNode, areaNode:
		f := p.node.value.(Field)
		return f.Change != nil && !f.Disabled
	case checkboxNode, switchNode:
		t := p.node.value.(Toggle)
		return t.Change != nil && !t.Disabled
	case tabsNode:
		t := p.node.value.(Tabs)
		return t.Change != nil && len(t.Labels) > 0
	case menuNode:
		return p.node.value.(Menu).focusable()
	}
	return false
}

func (s *scene) find(key string) (placedNode, bool) {
	if key != "" {
		for _, item := range s.items {
			if item.node.key == key {
				return item, true
			}
		}
	}
	return placedNode{}, false
}

func (s *scene) tab(reverse bool) {
	// While a dialog is open, Tab cycles inside it only.
	layer := ""
	if dialog, ok := s.topDialog(); ok {
		layer = dialog.node.key
	}
	first, last, previous := "", "", ""
	found := false
	for _, item := range s.items {
		if !item.focusable() || item.dialog != layer {
			continue
		}
		key := item.node.key
		if first == "" {
			first = key
		}
		last = key
		if found && !reverse {
			s.focus = key
			return
		}
		if key == s.focus {
			if reverse && previous != "" {
				s.focus = previous
				return
			}
			found = true
		}
		previous = key
	}
	s.focus = first
	if reverse {
		s.focus = last
	}
}

func workspaceContent(view View) Node {
	if view.Content.kind != emptyNode {
		return view.Content
	}
	toolbar := make([]Node, 0, 1+len(view.Buttons))
	if len(view.Filter.Options) > 0 {
		toolbar = append(toolbar, SelectBox("filter", view.Filter))
	}
	for i, button := range view.Buttons {
		toolbar = append(toolbar, PushButton(fmt.Sprintf("button-%d", i), button))
	}
	table := view.Table
	if table.Copy == "" {
		table.Copy = view.Detail.Copy
	}
	return VStack(6,
		Heading(view.Title),
		HStack(8, toolbar...).Height(40),
		Split("workspace", 68, VirtualTable("table", table), Details("detail", view.Detail)),
		Status(view.Status),
	).Padding(12)
}
