package desktop

import "image"

// Tabs is a controlled tab strip. Labels holds the tab captions in order and
// Selected is the active tab; the TabStrip child renders below the strip.
// Arrow keys, Home/End and clicks report the new index through Change.
type Tabs struct {
	Labels   []string
	Selected int
	Change   func(index int)
}

// TabStrip lays out a row of tabs above content. The strip is retained by
// key across view updates; the selection belongs to the application.
func TabStrip(key string, tabs Tabs, content Node) Node {
	return Node{kind: tabsNode, key: key, value: tabs, children: []Node{content}}
}

const tabsHeaderHeight = 36

func tabsHeader(item placedNode) image.Rectangle {
	return image.Rect(item.box.Min.X, item.box.Min.Y, item.box.Max.X,
		min(item.box.Max.Y, item.box.Min.Y+tabsHeaderHeight))
}

func tabSegment(item placedNode, i int) image.Rectangle {
	header := tabsHeader(item)
	count := max(1, len(item.node.value.(Tabs).Labels))
	return image.Rect(header.Min.X+header.Dx()*i/count, header.Min.Y,
		header.Min.X+header.Dx()*(i+1)/count, header.Max.Y)
}
