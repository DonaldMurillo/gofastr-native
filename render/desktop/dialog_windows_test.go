package desktop

import (
	"image"
	"testing"
)

func TestDialogRoutesDismissConfirmAndContainment(t *testing.T) {
	confirming := true
	confirms, cancels, news := 0, 0, 0
	view := func() View {
		content := VStack(8,
			PushButton("new", Button{Label: "New", Press: func() { news++ }}),
			PushButton("delete", Button{Label: "Delete", Press: func() {}}),
		).Padding(10)
		if confirming {
			content = DialogLayer("confirm", Dialog{
				Title: "Delete note", Content: Text("Delete this note?"),
				Confirm: "Delete", Cancel: "Keep",
				OnConfirm: func() { confirms++; confirming = false },
				OnCancel:  func() { cancels++; confirming = false },
			}, content)
		}
		return View{Title: "Dialog routing", Content: content}
	}
	window, err := newGPUWindow(t.Context(), view)
	if err != nil {
		t.Fatal(err)
	}
	size := image.Pt(720, 400)
	window.scene.layout(size)
	center := func(r image.Rectangle) image.Point { return r.Min.Add(r.Size().Div(2)) }

	if window.scene.focus != "confirm-cancel" {
		t.Fatalf("focus = %q, want the dialog cancel button", window.scene.focus)
	}

	// A click on the blocked base layer dismisses instead of activating.
	button, _ := window.scene.find("new")
	window.click(center(button.controlBox()))
	if news != 0 || cancels != 1 || confirming {
		t.Fatalf("modal click produced news=%d cancels=%d open=%v", news, cancels, confirming)
	}
	if window.scene.focus != "delete" && window.scene.focus != "new" {
		t.Fatalf("focus = %q, want it restored to the base layer", window.scene.focus)
	}
	if _, open := window.scene.topDialog(); open {
		t.Fatal("the dialog survived its cancel callback")
	}

	// Escape runs the same dismissal.
	confirming = true
	window.refresh()
	window.handleKeys(keyEvents{escape: true})
	if cancels != 2 || confirming {
		t.Fatalf("escape produced cancels=%d open=%v", cancels, confirming)
	}

	// Tab stays inside the dialog; Enter confirms and restores focus.
	confirming = true
	window.refresh()
	window.handleKeys(keyEvents{tab: true})
	if window.scene.focus != "confirm-confirm" {
		t.Fatalf("Tab moved to %q, want the confirm button", window.scene.focus)
	}
	window.handleKeys(keyEvents{tab: true})
	if window.scene.focus != "confirm-cancel" {
		t.Fatalf("Tab wrapped to %q, want the cancel button", window.scene.focus)
	}
	window.handleKeys(keyEvents{tab: true, shift: true})
	if window.scene.focus != "confirm-confirm" {
		t.Fatalf("Shift-Tab moved to %q", window.scene.focus)
	}
	window.handleKeys(keyEvents{enter: true})
	if confirms != 1 || confirming {
		t.Fatalf("enter confirmed %d times, open=%v", confirms, confirming)
	}
	if window.scene.focus == "" || window.scene.focus == "confirm-confirm" {
		t.Fatalf("focus = %q, want restoration to the base layer", window.scene.focus)
	}
}

func TestDialogTextInputTakesContainedFocus(t *testing.T) {
	typed := ""
	confirming := true
	view := func() View {
		content := VStack(8, PushButton("delete", Button{Label: "Delete", Press: func() {}})).Padding(10)
		if confirming {
			content = DialogLayer("rename", Dialog{
				Title: "Rename note",
				Content: VStack(6, TextInput("name", Field{
					Label: "Name", Placeholder: "New name",
					Change: func(value string) { typed = value },
				})),
				Confirm: "Rename", Cancel: "Cancel",
				OnConfirm: func() { confirming = false },
				OnCancel:  func() { confirming = false },
			}, content)
		}
		return View{Title: "Dialog field", Content: content}
	}
	window, err := newGPUWindow(t.Context(), view)
	if err != nil {
		t.Fatal(err)
	}
	window.scene.layout(image.Pt(720, 400))
	if window.scene.focus != "name" {
		t.Fatalf("focus = %q, want the dialog text field", window.scene.focus)
	}
	window.handleKeys(keyEvents{tab: true})
	if window.scene.focus != "rename-cancel" {
		t.Fatalf("Tab moved to %q, want the cancel button", window.scene.focus)
	}
	window.handleKeys(keyEvents{escape: true})
	if confirming {
		t.Fatal("Escape did not close the dialog")
	}
	if window.scene.focus != "delete" {
		t.Fatalf("focus = %q, want the delete button restored", window.scene.focus)
	}
	if typed != "" {
		t.Fatal("the field reported text without input")
	}
}

func TestStackDialogBlocksLaterSiblings(t *testing.T) {
	var pressed, dismissed bool
	window, err := newGPUWindow(t.Context(), func() View {
		return View{Content: HStack(8,
			DialogLayer("dialog", Dialog{OnCancel: func() { dismissed = true }}, Text("Background")),
			PushButton("outside", Button{Label: "Outside", Press: func() { pressed = true }}),
		)}
	})
	if err != nil {
		t.Fatal(err)
	}
	size := image.Pt(720, 400)
	window.scene.layout(size)
	dialog, _ := window.scene.topDialog()
	if dialog.box != (image.Rectangle{Max: size}) {
		t.Fatalf("dialog overlay = %v, want the entire window", dialog.box)
	}
	button, _ := window.scene.find("outside")
	box := button.controlBox()
	window.click(box.Min.Add(box.Size().Div(2)))
	if pressed || !dismissed {
		t.Fatalf("outside click pressed=%v, dismissed=%v", pressed, dismissed)
	}
}
