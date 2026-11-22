package desktop

import (
	"fmt"
	"hash/fnv"
	"image"
	"strings"
)

// Accessibility semantics for the GPU desktop renderer.
//
// The GPU renderer paints application views with DirectX and owns no native
// controls, so without help a screen reader sees an empty client area. The
// functions in this file translate a laid-out scene into a snapshot of
// accessible elements (role, name, state, bounds, focus) that the platform
// adapter in accessibility_windows.go exposes through Windows UI Automation.
//
// This file is platform-neutral: it reads the scene and produces data. The
// renderer builds the snapshot after layout, hands it to the UIA provider,
// and runs pattern actions (invoke, toggle, set text, select row, focus) on
// the UI thread through the same code paths as keyboard input.

// accRole classifies an element for platform accessibility APIs.
type accRole uint8

const (
	accTextRole accRole = iota
	accHeadingRole
	accStatusRole
	accButtonRole
	accToggleRole
	accComboRole
	accInputRole
	accAreaRole
	accListRole
	accRowRole
	accSplitterRole
	accTabRole
	accTabItemRole
	accMenuButtonRole
	accMenuRole
	accMenuItemRole
)

func (r accRole) selectionContainer() bool { return r == accListRole || r == accTabRole }
func (r accRole) selectionItem() bool      { return r == accRowRole || r == accTabItemRole }

// accElement is one accessible element. Bounds are in the window's client
// area, in logical pixels; the platform adapter converts them to physical
// screen coordinates when publishing.
type accElement struct {
	Key       string // scene node key; the owning table's key for rows
	Row       int    // -1 for controls, >= 0 for indexed children
	Role      accRole
	Name      string
	Value     string // committed field text, selected option, or row cells
	Help      string // field error text, exposed as UIA HelpText
	Enabled   bool
	Checked   bool
	Selected  bool
	Expanded  bool
	Focusable bool
	Focused   bool
	Box       image.Rectangle
	Parent    int // index into accSnapshot.Elements, -1 for window children
	static    bool
}

// accSnapshot is the complete accessibility state of one window layout.
type accSnapshot struct {
	Title     string
	Elements  []accElement
	Focus     int // index of the focused element, -1 when none
	Size      image.Point
	Structure uint64 // changes when the element topology changes
	State     uint64 // changes when names, values, or states change
}

type accKey struct {
	key    string
	row    int
	static bool
}

func accID(key string, row int) accKey { return accKey{key: key, row: row} }

func (e accElement) identity() accKey {
	return accKey{key: e.Key, row: e.Row, static: e.static}
}

// accSnapshot describes the scene for assistive technology. Call it after
// layout, on the renderer's UI thread. The caller fills Title from the view;
// the scene does not know the window title.
func (s *scene) accSnapshot() accSnapshot {
	return s.accSnapshotWithMenu("")
}

func (s *scene) accSnapshotWithMenu(openMenu string) accSnapshot {
	snap := accSnapshot{Focus: -1, Size: s.size}
	keyless := 0
	add := func(e accElement) int {
		if e.Key == "" {
			// Keyless static text still needs a distinct identity for the
			// platform adapter. The ordinal is stable while the tree is
			// unchanged; structural changes invalidate the whole tree anyway.
			e.Key = fmt.Sprintf("#%d", keyless)
			e.static = true
			keyless++
		}
		if e.Role != accRowRole && e.Parent < 0 {
			e.Row = -1 // window-level controls have no row
		}
		if e.Row == -1 && e.Focusable && e.Key == s.focus && !e.Expanded {
			e.Focused = true
		}
		if e.Focused {
			snap.Focus = len(snap.Elements)
		}
		snap.Elements = append(snap.Elements, e)
		return len(snap.Elements) - 1
	}
	dialog, modal := s.topDialog()
	for i := range s.items {
		item := &s.items[i]
		if modal && item.dialog != dialog.node.key {
			continue
		}
		switch item.node.kind {
		case buttonNode:
			b := item.node.value.(Button)
			add(accElement{
				Key: item.node.key, Role: accButtonRole, Name: b.Label,
				Enabled:   b.Press != nil && !b.Disabled,
				Focusable: item.focusable(), Box: item.box, Parent: -1,
			})
		case checkboxNode, switchNode:
			t := item.node.value.(Toggle)
			add(accElement{
				Key: item.node.key, Role: accToggleRole, Name: t.Label,
				Enabled: t.Change != nil && !t.Disabled, Checked: t.Checked,
				Focusable: item.focusable(), Box: item.box, Parent: -1,
			})
		case choiceNode:
			c := item.node.value.(Choice)
			if len(c.Options) == 0 {
				continue
			}
			selected := ""
			if c.Selected >= 0 && c.Selected < len(c.Options) {
				selected = c.Options[c.Selected]
			}
			add(accElement{
				Key: item.node.key, Role: accComboRole, Name: selected, Value: selected,
				Enabled:   c.Change != nil,
				Focusable: item.focusable(), Box: item.box, Parent: -1,
			})
		case tabsNode:
			tabs := item.node.value.(Tabs)
			parent := add(accElement{
				Key: item.node.key, Role: accTabRole, Name: "Tabs",
				Enabled: true, Box: tabsHeader(*item), Parent: -1,
			})
			for i, label := range tabs.Labels {
				focusable := item.focusable()
				add(accElement{
					Key: item.node.key, Row: i, Role: accTabItemRole, Name: label,
					Selected: i == tabs.Selected, Enabled: focusable, Focusable: focusable,
					Focused: focusable && i == tabs.Selected && s.focus == item.node.key,
					Box:     tabSegment(*item, i), Parent: parent,
				})
			}
		case menuNode:
			menu := item.node.value.(Menu)
			expanded := openMenu == item.node.key && item.focusable()
			trigger := add(accElement{
				Key: item.node.key, Role: accMenuButtonRole, Name: menu.Label,
				Enabled: item.focusable(), Focusable: item.focusable(), Expanded: expanded,
				Box: item.controlBox(), Parent: -1,
			})
			if expanded {
				parent := add(accElement{
					Key: item.node.key, Row: -2, Role: accMenuRole, Name: menu.Label,
					Enabled: true, Box: item.menuBox(0).Union(item.menuBox(len(menu.Items) - 1)), Parent: trigger,
				})
				for i, entry := range menu.Items {
					if entry.separator() {
						continue
					}
					add(accElement{
						Key: item.node.key, Row: i, Role: accMenuItemRole, Name: entry.Label,
						Enabled: entry.enabled(), Focusable: entry.enabled(),
						Focused: entry.enabled() && item.state.menu == i,
						Box:     item.menuBox(i), Parent: parent,
					})
				}
			}
		case inputNode, areaNode:
			f := item.node.value.(Field)
			role, name := accInputRole, f.Label
			if name == "" {
				name = f.Placeholder
			}
			if item.node.kind == areaNode {
				role = accAreaRole
			}
			add(accElement{
				Key: item.node.key, Role: role, Name: name, Value: f.Value,
				Help: f.Error, Enabled: f.Change != nil && !f.Disabled,
				Focusable: item.focusable(), Box: item.box, Parent: -1,
			})
		case tableNode:
			t := item.node.value.(Table)
			columns := make([]string, len(t.Columns))
			for c := range t.Columns {
				columns[c] = t.Columns[c].Title
			}
			table := add(accElement{
				Key: item.node.key, Role: accListRole,
				Name:      strings.Join(columns, ", "),
				Enabled:   item.focusable(),
				Focusable: item.focusable(), Box: item.box, Parent: -1,
			})
			rows, _ := item.tableBounds()
			visible := rows.Dy() / rowHeight
			for r := 0; r < visible && item.state.viewport.top+r < t.Count; r++ {
				row := item.state.viewport.top + r
				cells := make([]string, len(t.Columns))
				if t.Cell != nil {
					for c := range t.Columns {
						cells[c] = t.Cell(row, c)
					}
				}
				text := strings.Join(cells, " | ")
				add(accElement{
					Key: item.node.key, Row: row, Role: accRowRole,
					Name: text, Value: text,
					Enabled: t.Select != nil, Selected: row == t.Selected,
					Box:    image.Rect(rows.Min.X, rows.Min.Y+r*rowHeight, rows.Max.X, rows.Min.Y+(r+1)*rowHeight),
					Parent: table,
				})
			}
		case detailNode:
			d := item.node.value.(Detail)
			detail := add(accElement{
				Key: item.node.key, Role: accTextRole, Name: d.Title, Value: d.Text,
				Enabled:   true,
				Focusable: item.focusable(), Box: item.box, Parent: -1,
			})
			if d.Copy != "" {
				add(accElement{
					Key: item.node.key, Role: accButtonRole, Name: "Copy " + d.Title,
					Enabled: true, Box: item.copyBox(), Parent: detail,
				})
			}
		case headingNode:
			add(accElement{Role: accHeadingRole, Name: item.node.value.(string), Enabled: true, Box: item.box, Parent: -1})
		case statusNode:
			add(accElement{Role: accStatusRole, Name: item.node.value.(string), Enabled: true, Box: item.box, Parent: -1})
		case textNode:
			add(accElement{Role: accTextRole, Name: item.node.value.(string), Enabled: true, Box: item.box, Parent: -1})
		case splitNode:
			add(accElement{Role: accSplitterRole, Name: "Splitter", Enabled: true, Box: item.box, Parent: -1})
		case dialogNode:
			add(accElement{Role: accHeadingRole, Name: item.node.value.(Dialog).Title, Enabled: true, Box: item.split, Parent: -1})
		}
	}
	snap.hash()
	return snap
}

// hash computes the change fingerprints the platform adapter compares to
// decide which UI Automation events to raise.
func (s *accSnapshot) hash() {
	structure, state := fnv.New64a(), fnv.New64a()
	for i := range s.Elements {
		e := &s.Elements[i]
		fmt.Fprintf(structure, "%d|%s|%d|%d|%d;", i, e.Key, e.Row, e.Role, e.Parent)
		fmt.Fprintf(state, "%d|%s|%d|%q|%q|%q|%t|%t|%t|%t|%t|%t|%d,%d,%d,%d;",
			i, e.Key, e.Role, e.Name, e.Value, e.Help,
			e.Enabled, e.Checked, e.Selected, e.Expanded, e.Focusable, e.Focused,
			e.Box.Min.X, e.Box.Min.Y, e.Box.Max.X, e.Box.Max.Y)
	}
	fmt.Fprintf(state, "focus=%d|size=%d,%d", s.Focus, s.Size.X, s.Size.Y)
	s.Structure, s.State = structure.Sum64(), state.Sum64()
}

// accFindByKey returns the index of the control with the key, or -1.
func (s *accSnapshot) accFindByKey(key string, row int) int {
	for i := range s.Elements {
		e := &s.Elements[i]
		if e.Key == key && e.Row == row {
			return i
		}
	}
	return -1
}

// accFocusKey identifies the focused element by key and row, stable when
// element indexes shift between layouts.
func accFocusKey(s *accSnapshot) string {
	if s.Focus < 0 || s.Focus >= len(s.Elements) {
		return ""
	}
	e := &s.Elements[s.Focus]
	return fmt.Sprintf("%s/%d", e.Key, e.Row)
}
