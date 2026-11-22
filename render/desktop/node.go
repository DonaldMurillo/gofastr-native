package desktop

import (
	"image"
	"image/color"
)

type nodeKind uint8

const (
	emptyNode nodeKind = iota
	horizontalNode
	verticalNode
	splitNode
	panelNode
	textNode
	headingNode
	statusNode
	buttonNode
	choiceNode
	tableNode
	detailNode
	canvasNode
	inputNode
	areaNode
	checkboxNode
	switchNode
	tabsNode
	menuNode
	dialogNode
)

// Node is a value that describes part of a desktop view. Build a new tree
// when application state changes. Give interactive nodes distinct keys that
// stay the same when a node moves within the window.
type Node struct {
	kind     nodeKind
	key      string
	width    int
	height   int
	padding  int
	gap      int
	ratio    int
	children []Node
	value    any
}

// HStack lays out children left to right, with a gap in logical pixels.
func HStack(gap int, children ...Node) Node {
	return Node{kind: horizontalNode, gap: max(0, gap), children: children}
}

// VStack lays out children top to bottom, with a gap in logical pixels.
func VStack(gap int, children ...Node) Node {
	return Node{kind: verticalNode, gap: max(0, gap), children: children}
}

// Split lays out two panes with a draggable divider. The initial percentage
// belongs to the first pane. The renderer retains later drag positions by key.
func Split(key string, percent int, first, second Node) Node {
	return Node{
		kind: splitNode, key: key, ratio: clampSplit(percent),
		children: []Node{first, second},
	}
}

func Panel(child Node) Node {
	return Node{kind: panelNode, children: []Node{child}}
}

func Text(value string) Node    { return Node{kind: textNode, value: value} }
func Heading(value string) Node { return Node{kind: headingNode, value: value} }
func Status(value string) Node  { return Node{kind: statusNode, value: value} }

func PushButton(key string, button Button) Node {
	return Node{kind: buttonNode, key: key, value: button}
}

func SelectBox(key string, choice Choice) Node {
	return Node{kind: choiceNode, key: key, value: choice}
}

func VirtualTable(key string, table Table) Node {
	return Node{kind: tableNode, key: key, value: table}
}

func Details(key string, detail Detail) Node {
	return Node{kind: detailNode, key: key, value: detail}
}

// Field is a controlled text field. Change receives committed text. Submit
// runs on Enter in TextInput or Ctrl+Enter in TextArea.
type Field struct {
	Label, Value, Placeholder, Error string
	Disabled                         bool
	Change                           func(string)
	Submit                           func()
}

func TextInput(key string, field Field) Node {
	return Node{kind: inputNode, key: key, value: field}
}

func TextArea(key string, field Field) Node {
	return Node{kind: areaNode, key: key, value: field}
}

type Toggle struct {
	Label             string
	Checked, Disabled bool
	Change            func(bool)
}

func Checkbox(key string, toggle Toggle) Node {
	return Node{kind: checkboxNode, key: key, value: toggle}
}

func Switch(key string, toggle Toggle) Node {
	return Node{kind: switchNode, key: key, value: toggle}
}

// Canvas draws within the node's local bounds. Drawing is clipped to the node.
// The canvas is valid only while the Paint callback runs.
type Canvas interface {
	Bounds() image.Rectangle
	Fill(rect image.Rectangle, ink color.Color)
	Text(at image.Point, value string, ink color.Color)
}

func Paint(draw func(Canvas)) Node {
	return Node{kind: canvasNode, value: draw}
}

// Width sets the requested width in logical pixels. Zero uses the default.
func (n Node) Width(pixels int) Node { n.width = max(0, pixels); return n }

// Height sets the requested height in logical pixels. Zero uses the default.
func (n Node) Height(pixels int) Node { n.height = max(0, pixels); return n }

// Padding insets a node's contents on all four sides.
func (n Node) Padding(pixels int) Node { n.padding = max(0, pixels); return n }
