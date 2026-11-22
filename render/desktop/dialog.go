package desktop

import "image"

// Dialog is a modal layer over the whole window. Content is composed from
// ordinary nodes and renders inside a centered panel with Title. Confirm and
// Cancel add action buttons; OnConfirm runs on the confirm button and
// OnCancel on the cancel button, Escape, or a click outside the panel.
// Removing the DialogLayer from the tree closes it and restores the focus
// the window had before the dialog opened.
type Dialog struct {
	Title     string
	Content   Node
	Confirm   string // confirm button label; empty omits the button
	Cancel    string // cancel button label; empty omits the button
	Width     int    // panel width in logical pixels; 0 uses 380
	OnConfirm func()
	OnCancel  func()
}

// DialogLayer overlays content with a modal dialog. Keep the dialog in the
// tree exactly while it is open; the application opens and closes it by
// rebuilding the view.
func DialogLayer(key string, dialog Dialog, content Node) Node {
	panel := make([]Node, 0, 3)
	if dialog.Title != "" {
		panel = append(panel, Heading(dialog.Title))
	}
	panel = append(panel, dialog.Content)
	actions := make([]Node, 0, 3)
	if dialog.Cancel != "" {
		actions = append(actions, PushButton(key+"-cancel", Button{Label: dialog.Cancel, Press: dialog.OnCancel}))
	}
	if dialog.Confirm != "" {
		actions = append(actions, PushButton(key+"-confirm", Button{Label: dialog.Confirm, Press: dialog.OnConfirm}))
	}
	if len(actions) > 0 {
		// The leading empty node is a flexible spacer, so the buttons align
		// to the right edge and focus order starts at the cancel button.
		actions = append([]Node{{}}, actions...)
		panel = append(panel, HStack(8, actions...).Height(34))
	}
	return Node{
		kind: dialogNode, key: key, value: dialog,
		children: []Node{content, VStack(10, panel...).Padding(18)},
	}
}

// dialogPanelBox centers the dialog panel inside the window box, clamped to
// leave a margin around it even in very small windows.
func dialogPanelBox(box image.Rectangle, node Node) image.Rectangle {
	dialog := node.value.(Dialog)
	width := dialog.Width
	if width <= 0 {
		width = 380
	}
	width = min(width, max(0, box.Dx()-48))
	height := min(max(120, naturalHeight(node.children[1])), max(0, box.Dy()-48))
	return image.Rect(box.Min.X+(box.Dx()-width)/2, box.Min.Y+(box.Dy()-height)/2,
		box.Min.X+(box.Dx()+width)/2, box.Min.Y+(box.Dy()+height)/2)
}

// naturalHeight reports the height a stack subtree asks for. Children that
// ask for flexible height count as zero, so content with flexible parts
// should set an explicit Height.
func naturalHeight(node Node) int {
	total := 0
	switch node.kind {
	case verticalNode:
		for _, child := range node.children {
			total += naturalHeight(child)
		}
		total += node.gap * max(0, len(node.children)-1)
	case horizontalNode:
		for _, child := range node.children {
			total = max(total, naturalHeight(child))
		}
	default:
		total = node.request(false)
	}
	return total + 2*node.padding
}
