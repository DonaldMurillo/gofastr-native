package desktop

import (
	"bytes"
	"image"
	"image/color"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
)

type gpuTheme struct {
	body, heading, title, mono *text.GoTextFace
}

func (t gpuTheme) scaled(scale float64) gpuTheme {
	if scale == 1 {
		return t
	}
	resize := func(face *text.GoTextFace) *text.GoTextFace {
		copy := *face
		copy.Size *= scale
		return &copy
	}
	return gpuTheme{body: resize(t.body), heading: resize(t.heading), title: resize(t.title), mono: resize(t.mono)}
}

func newGPUTheme() (gpuTheme, error) {
	regular, err := text.NewGoTextFaceSource(bytes.NewReader(goregular.TTF))
	if err != nil {
		return gpuTheme{}, err
	}
	bold, err := text.NewGoTextFaceSource(bytes.NewReader(gobold.TTF))
	if err != nil {
		return gpuTheme{}, err
	}
	mono, err := text.NewGoTextFaceSource(bytes.NewReader(gomono.TTF))
	if err != nil {
		return gpuTheme{}, err
	}
	return gpuTheme{
		body:    &text.GoTextFace{Source: regular, Size: 14},
		heading: &text.GoTextFace{Source: bold, Size: 14},
		title:   &text.GoTextFace{Source: bold, Size: 22},
		mono:    &text.GoTextFace{Source: mono, Size: 13},
	}, nil
}

var (
	background = color.RGBA{22, 25, 30, 255}
	panel      = color.RGBA{29, 33, 40, 255}
	shaded     = color.RGBA{33, 38, 45, 255}
	control    = color.RGBA{42, 49, 58, 255}
	border     = color.RGBA{63, 73, 86, 255}
	selected   = color.RGBA{32, 65, 83, 255}
	textColor  = color.RGBA{234, 240, 247, 255}
	muted      = color.RGBA{168, 181, 196, 255}
	disabled   = color.RGBA{132, 145, 160, 255}
	accent     = color.RGBA{107, 219, 231, 255}
)

func (w *gpuWindow) Draw(screen *ebiten.Image) {
	p := gpuPainter{screen: screen, dpiScale: w.deviceScale}
	screen.Fill(background)
	for _, item := range w.scene.items {
		w.paintNode(p, item)
	}
	if item, ok := w.scene.find(w.openChoice); ok {
		for i, option := range item.node.value.(Choice).Options {
			w.drawControl(p, item.optionBox(i), option, false, i == item.state.choice)
		}
	}
	if item, ok := w.scene.find(w.openMenu); ok {
		w.drawMenuItems(p, item)
	}
	if w.firstFrame {
		w.firstFrame = false
		// Present the initial workspace even if the OS delivered no input.
		ebiten.SetFPSMode(ebiten.FPSModeVsyncOffMinimum)
	}
}

func (w *gpuWindow) paintNode(p gpuPainter, item placedNode) {
	node, box := item.node, item.box
	focus := node.key != "" && node.key == w.scene.focus
	switch node.kind {
	case textNode:
		p.label(box, node.value.(string), w.paintTheme.body, textColor, 2)
	case headingNode:
		p.label(box, node.value.(string), w.paintTheme.title, textColor, 2)
	case statusNode:
		status := node.value.(string)
		if w.status != "" {
			status = w.status
		}
		p.label(box, status, w.paintTheme.body, muted, 2)
	case buttonNode:
		button := node.value.(Button)
		w.drawControl(p, item.controlBox(), button.Label, !item.focusable(), focus)
	case choiceNode:
		w.drawChoice(p, item, focus)
	case tableNode:
		w.drawTable(p, item, focus)
	case detailNode:
		w.drawDetail(p, item, focus)
	case inputNode, areaNode:
		w.drawField(p, item, focus)
	case checkboxNode, switchNode:
		w.drawToggle(p, item, focus)
	case tabsNode:
		w.drawTabs(p, item, focus)
	case menuNode:
		w.drawMenuButton(p, item, focus)
	case dialogNode:
		w.drawDialog(p, item)
	case panelNode:
		p.fill(box, panel)
	case splitNode:
		x := box.Min.X + box.Dx()/2
		p.fill(image.Rect(x, box.Min.Y, x+1, box.Max.Y), border)
	case canvasNode:
		if draw := node.value.(func(Canvas)); draw != nil {
			draw(gpuCanvas{p, box, w.paintTheme.body})
		}
	}
}

func (w *gpuWindow) drawChoice(p gpuPainter, item placedNode, focused bool) {
	choice := item.node.value.(Choice)
	box := item.controlBox()
	value := ""
	if choice.Selected >= 0 && choice.Selected < len(choice.Options) {
		value = choice.Options[choice.Selected]
	}
	w.drawControl(p, box, value, !item.focusable(), focused)
	p.drawCaret(box)
}

// drawCaret draws the dropdown mark directly so it doesn't depend on font
// coverage.
func (p gpuPainter) drawCaret(box image.Rectangle) {
	x, y := box.Max.X-15, box.Min.Y+box.Dy()/2
	if box.Dx() >= 30 {
		for i := range 4 {
			p.fill(image.Rect(x-3+i, y-1+i, x+4-i, y+i), muted)
		}
	}
}

func (w *gpuWindow) drawMenuButton(p gpuPainter, item placedNode, focused bool) {
	menu := item.node.value.(Menu)
	w.drawControl(p, item.controlBox(), menu.Label, !item.focusable(), focused)
	p.drawCaret(item.controlBox())
}

func (w *gpuWindow) drawMenuItems(p gpuPainter, item placedNode) {
	menu := item.node.value.(Menu)
	for i, entry := range menu.Items {
		box := item.menuBox(i)
		if entry.separator() {
			p.fill(box, panel)
			y := box.Min.Y + box.Dy()/2
			p.fill(image.Rect(box.Min.X+6, y, box.Max.X-6, y+1), border)
			continue
		}
		w.drawControl(p, box, entry.Label, !entry.enabled(), i == item.state.menu)
	}
}

func (w *gpuWindow) drawTabs(p gpuPainter, item placedNode, focused bool) {
	tabs := item.node.value.(Tabs)
	p.fill(tabsHeader(item), control)
	for i, caption := range tabs.Labels {
		segment := tabSegment(item, i)
		ink := muted
		if i == tabs.Selected {
			ink = textColor
			p.fill(segment, shaded)
			mark := selected
			if focused {
				mark = accent
			}
			p.fill(image.Rect(segment.Min.X, segment.Max.Y-3, segment.Max.X, segment.Max.Y), mark)
		}
		p.label(segment.Inset(2), caption, w.paintTheme.heading, ink, 10)
	}
}

func (w *gpuWindow) drawDialog(p gpuPainter, item placedNode) {
	p.fill(item.box, color.RGBA{8, 10, 14, 150})
	p.fill(item.split, border)
	p.fill(item.split.Inset(1), panel)
}

func (w *gpuWindow) drawControl(p gpuPainter, box image.Rectangle, title string, inactive, focused bool) {
	outline, ink := border, textColor
	if focused && !inactive {
		outline = accent
	}
	if inactive {
		ink = disabled
	}
	p.fill(box, outline)
	p.fill(box.Inset(1), control)
	p.label(box.Inset(1), title, w.paintTheme.body, ink, 10)
}

func (w *gpuWindow) drawTable(p gpuPainter, item placedNode, focused bool) {
	table, v := item.node.value.(Table), item.state.viewport
	box := item.box
	rows, _ := item.tableBounds()
	p.fill(box, panel)
	header := image.Rect(box.Min.X, box.Min.Y, box.Max.X, rows.Min.Y)
	p.fill(header, control)
	for column, spec := range table.Columns {
		cell := cellRect(header, table.Columns, column)
		p.label(cell, spec.Title, w.paintTheme.heading, textColor, 9)
	}
	for i := 0; i < v.page; i++ {
		row := v.top + i
		if row >= table.Count || table.Cell == nil {
			break
		}
		y := rows.Min.Y + i*rowHeight
		line := image.Rect(rows.Min.X, y, rows.Max.X, y+rowHeight)
		if row == table.Selected {
			p.fill(line, selected)
			p.fill(image.Rect(line.Min.X, y, line.Min.X+3, y+rowHeight), accent)
		} else if row%2 == 1 {
			p.fill(line, shaded)
		}
		for column := range table.Columns {
			cell := cellRect(line, table.Columns, column)
			if cell.Dx() > 0 {
				p.label(cell, table.Cell(row, column), w.paintTheme.mono, textColor, 9)
			}
		}
	}
	if focused {
		p.fill(image.Rect(header.Min.X, header.Min.Y, header.Max.X, header.Min.Y+1), accent)
	}
	p.fill(scrollThumb(item), border)
}

func cellRect(line image.Rectangle, columns []Column, column int) image.Rectangle {
	left := line.Min.X
	for i := 0; i < column; i++ {
		left += max(0, columns[i].Width)
	}
	if left >= line.Max.X {
		return image.Rectangle{}
	}
	right := line.Max.X
	if column < len(columns)-1 {
		right = min(right, left+max(0, columns[column].Width))
	}
	return image.Rect(left, line.Min.Y, right, line.Max.Y)
}

func scrollThumb(item placedNode) image.Rectangle {
	_, scroll := item.tableBounds()
	track := scroll.Inset(2)
	v := item.state.viewport
	if v.count <= v.page || track.Empty() {
		return image.Rectangle{}
	}
	height := min(track.Dy(), max(30, track.Dy()*v.page/v.count))
	y := track.Min.Y + int(int64(track.Dy()-height)*int64(v.top)/int64(v.count-v.page))
	return image.Rect(track.Min.X, y, track.Max.X, y+height)
}

func (w *gpuWindow) drawDetail(p gpuPainter, item placedNode, focused bool) {
	box, detail := item.box, item.node.value.(Detail)
	p.fill(box, panel)
	header := image.Rect(box.Min.X, box.Min.Y, box.Max.X, min(box.Max.Y, box.Min.Y+30))
	p.fill(header, control)
	p.label(header, detail.Title, w.paintTheme.heading, textColor, 12)
	body := image.Rectangle{
		Min: image.Pt(box.Min.X+14, header.Max.Y+12),
		Max: image.Pt(box.Max.X-14, item.copyBox().Min.Y-10),
	}
	y := body.Min.Y
	for _, line := range strings.Split(strings.ReplaceAll(detail.Text, "\r\n", "\n"), "\n") {
		if body.Empty() || y+22 > body.Max.Y {
			break
		}
		for _, wrapped := range wrapLine(line, w.theme.body, body.Dx()) {
			if y+22 > body.Max.Y {
				break
			}
			p.label(image.Rect(body.Min.X, y, body.Max.X, y+22), wrapped, w.paintTheme.body, textColor, 0)
			y += 22
		}
	}
	w.drawControl(p, item.copyBox(), "Copy message", detail.Copy == "", focused)
}

type gpuPainter struct {
	screen *ebiten.Image
	dpiScale
}

type gpuCanvas struct {
	painter gpuPainter
	box     image.Rectangle
	font    *text.GoTextFace
}

func (c gpuCanvas) Bounds() image.Rectangle { return image.Rectangle{Max: c.box.Size()} }

func (c gpuCanvas) Fill(rect image.Rectangle, ink color.Color) {
	c.painter.fill(rect.Add(c.box.Min).Intersect(c.box), ink)
}

func (c gpuCanvas) Text(at image.Point, value string, ink color.Color) {
	point := at.Add(c.box.Min)
	c.painter.drawText(c.box, value, c.font, ink, float64(point.X), float64(point.Y))
}

func wrapLine(line string, face text.Face, width int) []string {
	var lines []string
	var current string
	for _, word := range strings.Fields(line) {
		if current == "" {
			current = word
			continue
		}
		next := current + " " + word
		if size, _ := text.Measure(next, face, 0); size > float64(width) {
			lines = append(lines, current)
			current = word
		} else {
			current = next
		}
	}
	return append(lines, current)
}

func (p gpuPainter) fill(r image.Rectangle, c color.Color) {
	r = p.rect(r)
	if !r.Empty() {
		vector.FillRect(p.screen, float32(r.Min.X), float32(r.Min.Y), float32(r.Dx()), float32(r.Dy()), c, false)
	}
}

func (p gpuPainter) label(r image.Rectangle, value string, face *text.GoTextFace, ink color.Color, inset int) {
	if r.Dx() <= 2*inset || r.Dy() <= 0 {
		return
	}
	y := float64(r.Min.Y) + (float64(r.Dy())-face.Size/float64(p.dpiScale))/2
	p.drawText(r, value, face, ink, float64(r.Min.X+inset), y)
}

func (p gpuPainter) drawText(clip image.Rectangle, value string, face *text.GoTextFace, ink color.Color, x, y float64) {
	clip = p.rect(clip).Intersect(p.screen.Bounds())
	if value == "" || clip.Empty() {
		return
	}
	var opts text.DrawOptions
	origin := p.pixel(x, y)
	opts.GeoM.Translate(float64(origin.X), float64(origin.Y))
	opts.ColorScale.ScaleWithColor(ink)
	text.Draw(p.screen.SubImage(clip).(*ebiten.Image), value, face, &opts)
}
