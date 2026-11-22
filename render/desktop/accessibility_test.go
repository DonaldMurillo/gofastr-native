package desktop

import (
	"image"
	"testing"
)

// accScene lays out a Notes-style workspace and returns its snapshot.
func accScene(t *testing.T, view View, size image.Point) accSnapshot {
	t.Helper()
	var s scene
	if err := s.set(workspaceContent(view)); err != nil {
		t.Fatalf("scene set: %v", err)
	}
	s.layout(size)
	return s.accSnapshot()
}

func accFind(t *testing.T, snap accSnapshot, key string, row int) accElement {
	t.Helper()
	i := snap.accFindByKey(key, row)
	if i < 0 {
		t.Fatalf("no accessible element for key %q row %d", key, row)
	}
	return snap.Elements[i]
}

func accFindByRole(snap accSnapshot, role accRole) accElement {
	for _, e := range snap.Elements {
		if e.Role == role {
			return e
		}
	}
	panic("no element with role")
}

func TestAccSnapshotHidesControlsBehindModal(t *testing.T) {
	view := View{Content: DialogLayer("discard", Dialog{
		Title: "Discard draft?", Content: Text("Discard the unsaved edits."),
		Cancel: "Keep editing", OnCancel: func() {},
		Confirm: "Discard", OnConfirm: func() {},
	}, PushButton("save", Button{Label: "Save", Press: func() {}}))}
	snap := accScene(t, view, image.Pt(900, 600))
	if snap.accFindByKey("save", -1) >= 0 {
		t.Fatal("the save button was exposed behind the modal")
	}
	cancel := accFind(t, snap, "discard-cancel", -1)
	if !cancel.Focused || cancel.Name != "Keep editing" {
		t.Fatalf("modal should expose its safe default with focus: %+v", cancel)
	}
}

func TestAccSnapshotTracksTabSelectionAndOpenMenu(t *testing.T) {
	var s scene
	content := VStack(8,
		MenuButton("actions", Menu{Label: "Note actions", Items: []MenuItem{
			{Label: "Discard draft", Action: func() {}},
			{},
			{Label: "Restore", Disabled: true, Action: func() {}},
		}}),
		TabStrip("note-tabs", Tabs{Labels: []string{"Write", "Details"}, Selected: 1, Change: func(int) {}}, Text("Editor")),
	)
	if err := s.set(content); err != nil {
		t.Fatal(err)
	}
	s.layout(image.Pt(900, 600))
	s.focus = "note-tabs"
	closed := s.accSnapshotWithMenu("")
	tab := accFind(t, closed, "note-tabs", 1)
	if tab.Role != accTabItemRole || !tab.Selected || !tab.Focused || tab.Parent != closed.accFindByKey("note-tabs", -1) {
		t.Fatalf("second tab was not the focused selection: %+v", tab)
	}
	if closed.accFindByKey("actions", 0) >= 0 {
		t.Fatal("a closed menu exposed its items")
	}
	s.focus = "actions"
	open := s.accSnapshotWithMenu("actions")
	trigger := accFind(t, open, "actions", -1)
	item := accFind(t, open, "actions", 0)
	if !trigger.Expanded || trigger.Focused || item.Role != accMenuItemRole || !item.Focused || item.Parent != open.accFindByKey("actions", -2) {
		t.Fatalf("the open menu did not transfer focus to its first item: %+v, %+v", trigger, item)
	}
	if disabled := accFind(t, open, "actions", 2); disabled.Enabled || disabled.Focusable || open.accFindByKey("actions", 1) >= 0 {
		t.Fatal("a disabled entry was focusable or the separator was exposed as an action")
	}
}

func TestAccSnapshotWorkspaceControls(t *testing.T) {
	view := View{
		Title: "Notes",
		Buttons: []Button{
			{Label: "New note", Press: func() {}},
			{Label: "Save", Disabled: true, Press: func() {}},
		},
		Filter: Choice{Options: []string{"All notes", "Pinned"}, Selected: 1, Change: func(int) {}},
		Table: Table{
			Key:     "rows",
			Columns: []Column{{Title: "When"}, {Title: "Text"}},
			Count:   40,
			Cell:    func(row, column int) string { return []string{"12:0" + string(rune('0'+row%10)), "line"}[column] },
			Select:  func(int) {},
		},
		Detail: Detail{Title: "Note", Text: "body", Copy: "body"},
		Status: "3 notes",
	}
	snap := accScene(t, view, image.Pt(1120, 700))

	if got := accFind(t, snap, "button-0", -1); got.Name != "New note" || !got.Enabled || !got.Focusable {
		t.Errorf("button-0: name=%q enabled=%v focusable=%v", got.Name, got.Enabled, got.Focusable)
	}
	if got := accFind(t, snap, "button-1", -1); got.Name != "Save" || got.Enabled || got.Focused {
		t.Errorf("disabled button: name=%q enabled=%v focused=%v", got.Name, got.Enabled, got.Focused)
	}
	filter := accFind(t, snap, "filter", -1)
	if filter.Role != accComboRole || filter.Name != "Pinned" || filter.Value != "Pinned" {
		t.Errorf("filter: role=%v name=%q value=%q", filter.Role, filter.Name, filter.Value)
	}
	if got := accFind(t, snap, "table", -1); got.Role != accListRole || got.Name != "When, Text" || !got.Focusable {
		t.Errorf("table: role=%v name=%q focusable=%v", got.Role, got.Name, got.Focusable)
	}
	detail := accFind(t, snap, "detail", -1)
	if detail.Role != accTextRole || detail.Value != "body" {
		t.Errorf("detail: role=%v value=%q", detail.Role, detail.Value)
	}
	if got := accFindByRole(snap, accStatusRole); got.Name != "3 notes" {
		t.Errorf("status: name=%q", got.Name)
	}
	if got := accFind(t, snap, "detail", 0); got.Role != accButtonRole || got.Name != "Copy Note" || got.Parent != snap.accFindByKey("detail", -1) {
		t.Errorf("copy child: %+v", got)
	}

	// Focus starts on the table, which the layout picks for keyboard users.
	if snap.Focus < 0 || snap.Elements[snap.Focus].Key != "table" {
		t.Errorf("focus = %d, want the table", snap.Focus)
	}
	if !snap.Elements[snap.Focus].Focused {
		t.Errorf("focused flag not set on the table")
	}

	// Rows: visible page only, parented at the table, named from cells.
	table := snap.accFindByKey("table", -1)
	rows := 0
	for _, e := range snap.Elements {
		if e.Role == accRowRole && e.Key == "table" {
			if e.Parent != table {
				t.Errorf("row %d parent = %d, want %d", e.Row, e.Parent, table)
			}
			if e.Box.Dy() != rowHeight {
				t.Errorf("row %d height = %d, want %d", e.Row, e.Box.Dy(), rowHeight)
			}
			rows++
		}
	}
	if rows == 0 || rows >= 40 {
		t.Errorf("visible rows = %d, want the visible page of 40", rows)
	}
	first := accFind(t, snap, "table", 0)
	if want := "12:00 | line"; first.Name != want {
		t.Errorf("row 0 name = %q, want %q", first.Name, want)
	}
}

func TestAccSnapshotTableSelectionAndScroll(t *testing.T) {
	var s scene
	err := s.set(workspaceContent(View{
		Title: "log",
		Table: Table{
			Key:      "rows",
			Columns:  []Column{{Title: "Line"}},
			Count:    500,
			Cell:     func(row, column int) string { return "entry" },
			Select:   func(int) {},
			Selected: 3,
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	s.layout(image.Pt(800, 600))
	snap := s.accSnapshot()
	if got := accFind(t, snap, "table", 3); !got.Selected {
		t.Errorf("row 3 not marked selected")
	}
	if got := accFind(t, snap, "table", 0); got.Selected {
		t.Errorf("row 0 marked selected")
	}

	// Scrolling the viewport changes which rows are exposed.
	table, _ := s.find("table")
	table.state.viewport.scroll(10)
	s.layout(image.Pt(800, 600))
	snap = s.accSnapshot()
	if i := snap.accFindByKey("table", 3); i >= 0 {
		t.Errorf("row 3 still exposed after scrolling away")
	}
	if got := accFind(t, snap, "table", 10); got.Selected {
		t.Errorf("row 10 should be exposed but not selected")
	}
}

func TestAccSnapshotFieldsAndToggles(t *testing.T) {
	content := VStack(8,
		Heading("New"),
		TextInput("title", Field{Label: "Title", Value: "hello", Change: func(string) {}}),
		TextInput("draft", Field{Placeholder: "Name", Error: "required", Change: func(string) {}}),
		TextArea("body", Field{Label: "Note", Value: "line", Disabled: true, Change: func(string) {}}),
		Checkbox("pin", Toggle{Label: "Pin", Checked: true, Change: func(bool) {}}),
		Switch("auto", Toggle{Label: "Auto", Disabled: true, Change: func(bool) {}}),
		Text("plain"),
		Status("saved"),
	)
	var s scene
	if err := s.set(content); err != nil {
		t.Fatal(err)
	}
	s.layout(image.Pt(400, 700))
	snap := s.accSnapshot()

	title := accFind(t, snap, "title", -1)
	if title.Role != accInputRole || title.Name != "Title" || title.Value != "hello" || !title.Enabled || !title.Focusable {
		t.Errorf("title field: %+v", title)
	}
	draft := accFind(t, snap, "draft", -1)
	if draft.Name != "Name" || draft.Help != "required" {
		t.Errorf("draft field name/help = %q/%q", draft.Name, draft.Help)
	}
	body := accFind(t, snap, "body", -1)
	if body.Role != accAreaRole || body.Enabled || body.Focused {
		t.Errorf("disabled area: %+v", body)
	}
	pin := accFind(t, snap, "pin", -1)
	if pin.Role != accToggleRole || !pin.Checked || !pin.Enabled {
		t.Errorf("pin toggle: %+v", pin)
	}
	auto := accFind(t, snap, "auto", -1)
	if auto.Checked || auto.Enabled {
		t.Errorf("disabled switch: %+v", auto)
	}
	if got := accFindByRole(snap, accStatusRole); got.Name != "saved" {
		t.Errorf("status: name=%q", got.Name)
	}
}

func TestAccSnapshotFingerprints(t *testing.T) {
	base := View{
		Title:   "t",
		Buttons: []Button{{Label: "Go", Press: func() {}}},
		Table:   Table{Key: "rows", Columns: []Column{{Title: "c"}}, Count: 3, Cell: func(int, int) string { return "x" }, Select: func(int) {}},
	}
	first := accScene(t, base, image.Pt(900, 600))
	same := accScene(t, base, image.Pt(900, 600))
	if first.Structure != same.Structure || first.State != same.State {
		t.Errorf("identical scenes produced different fingerprints")
	}

	resized := accScene(t, base, image.Pt(800, 600))
	if resized.Structure != first.Structure {
		t.Errorf("resize changed the structure fingerprint")
	}
	if resized.State == first.State {
		t.Errorf("resize left the state fingerprint unchanged")
	}

	base.Buttons[0].Disabled = true
	changed := accScene(t, base, image.Pt(900, 600))
	if changed.Structure != first.Structure {
		t.Errorf("disabling a button changed the structure fingerprint")
	}
	if changed.State == first.State {
		t.Errorf("disabling a button left the state fingerprint unchanged")
	}
}

func TestAccIDStable(t *testing.T) {
	if accID("pin", -1) != accID("pin", -1) || accID("pin", -1) == accID("pin", 2) || accID("pin", -1) == accID("pin", 0) {
		t.Errorf("accID not stable or not row-sensitive")
	}
	if accID("a", -1) == accID("b", -1) {
		t.Errorf("distinct keys collided")
	}
}
