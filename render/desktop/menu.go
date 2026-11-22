package desktop

import "image"

// MenuItem is one entry of a Menu. A disabled entry ignores the keyboard and
// the mouse. A zero MenuItem renders as a separator.
type MenuItem struct {
	Label    string
	Disabled bool
	Action   func()
}

func (m MenuItem) enabled() bool   { return m.Label != "" && m.Action != nil && !m.Disabled }
func (m MenuItem) separator() bool { return m.Label == "" && m.Action == nil }
func (m Menu) focusable() bool {
	for _, entry := range m.Items {
		if entry.enabled() {
			return true
		}
	}
	return false
}

// firstEnabled reports the first selectable index, or 0 when none is.
func (m Menu) firstEnabled() int {
	for i, entry := range m.Items {
		if entry.enabled() {
			return i
		}
	}
	return 0
}

// step moves from the given index by delta, skipping separators and disabled
// entries, and wraps around both ends.
func (m Menu) step(from, delta int) int {
	if len(m.Items) == 0 {
		return 0
	}
	for n := len(m.Items); n > 0; n-- {
		from = (from + delta + len(m.Items)) % len(m.Items)
		if m.Items[from].enabled() {
			return from
		}
	}
	return from
}

// Menu is a push button that opens a dropdown of actions. Each entry carries
// its own callback; the renderer owns the open state.
type Menu struct {
	Label string
	Items []MenuItem
}

// MenuButton shows a labeled button that opens the menu. Open, highlight,
// Escape and dismissal behavior follows the select box.
func MenuButton(key string, menu Menu) Node {
	return Node{kind: menuNode, key: key, value: menu}
}

func (p placedNode) menuBox(i int) image.Rectangle {
	r := p.controlBox()
	y := r.Max.Y + 2 + i*30
	return image.Rect(r.Min.X, y, r.Max.X, y+30)
}
