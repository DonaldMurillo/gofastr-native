package desktop

import (
	"context"
	"errors"
	"image"
	"math"
	"os"
	"sync/atomic"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

type gpuWindow struct {
	ctx               context.Context
	view              func() View
	current           View
	dirty             atomic.Bool
	accessibilityWake chan struct{}
	scene             scene
	theme             gpuTheme
	paintTheme        gpuTheme
	deviceScale       dpiScale
	layoutSize        image.Point
	firstFrame        bool
	drag              string
	dragOffset        int
	openChoice        string
	openMenu          string
	status            string
	err               error
	text              gpuTextInput
}

func run(ctx context.Context, view func() View, updates <-chan struct{}) error {
	if view == nil {
		return errors.New("desktop: a view function is required")
	}
	if ctx.Err() != nil {
		return nil
	}
	window, err := newGPUWindow(ctx, view)
	if err != nil {
		return err
	}
	return runGPUWindow(window, updates, window)
}

func newGPUWindow(ctx context.Context, view func() View) (*gpuWindow, error) {
	theme, err := newGPUTheme()
	if err != nil {
		return nil, err
	}
	w := &gpuWindow{
		ctx: ctx, view: view, current: view(), theme: theme, paintTheme: theme,
		deviceScale: 1, firstFrame: true,
		accessibilityWake: make(chan struct{}, 1),
	}
	if err := w.scene.set(workspaceContent(w.current)); err != nil {
		return nil, err
	}
	return w, nil
}

func runGPUWindow(window *gpuWindow, updates <-chan struct{}, game ebiten.Game) error {
	ebiten.SetWindowTitle(window.current.Title)
	ebiten.SetWindowSize(1120, 700)
	ebiten.SetWindowSizeLimits(720, 400, -1, -1)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetWindowClosingHandled(true)
	ebiten.SetFPSMode(ebiten.FPSModeVsyncOn)

	done := make(chan struct{})
	stopped := make(chan struct{})
	go window.watch(updates, done, stopped)
	defer func() {
		close(done)
		<-stopped
	}()
	return ebiten.RunGameWithOptions(game, &ebiten.RunGameOptions{
		GraphicsLibrary: ebiten.GraphicsLibraryDirectX,
	})
}

func (w *gpuWindow) watch(updates <-chan struct{}, done <-chan struct{}, stopped chan<- struct{}) {
	defer close(stopped)
	var retry *time.Ticker
	var ticks <-chan time.Time
	defer func() {
		if retry != nil {
			retry.Stop()
		}
	}()
	requestFrame := func() {
		ebiten.ScheduleFrame()
		if ticks == nil {
			if retry == nil {
				retry = time.NewTicker(time.Second / 60)
			} else {
				retry.Reset(time.Second / 60)
			}
			ticks = retry.C
		}
	}
	canceled := w.ctx.Done()
	for {
		select {
		case <-canceled:
			canceled = nil
			requestFrame()
		case <-done:
			return
		case <-w.accessibilityWake:
			w.dirty.Store(true)
			requestFrame()
		case <-ticks:
			if w.dirty.Load() || w.ctx.Err() != nil {
				// A native wakeup can be consumed before the game loop waits.
				// Retry until Update observes the pending work.
				ebiten.ScheduleFrame()
			} else {
				retry.Stop()
				ticks = nil
			}
		case _, open := <-updates:
			if !open {
				updates = nil
			} else if !w.dirty.Swap(true) {
				requestFrame()
			}
		}
	}
}

func (w *gpuWindow) Layout(width, height int) (int, int) {
	return max(1, width), max(1, height)
}

type dpiScale float64

func (s dpiScale) pixel(x, y float64) image.Point {
	return image.Pt(int(math.Round(x*float64(s))), int(math.Round(y*float64(s))))
}

func (s dpiScale) rect(r image.Rectangle) image.Rectangle {
	return image.Rectangle{
		Min: s.pixel(float64(r.Min.X), float64(r.Min.Y)),
		Max: s.pixel(float64(r.Max.X), float64(r.Max.Y)),
	}
}

func (s dpiScale) logicalPoint(x, y float64) image.Point {
	// Reverse the rounded paint edges so each pixel belongs to the control
	// that painted it, including at fractional scales.
	x = math.Ceil((math.Floor(x)+0.5)/float64(s)) - 1
	y = math.Ceil((math.Floor(y)+0.5)/float64(s)) - 1
	return image.Pt(int(x), int(y))
}

func (w *gpuWindow) LayoutF(width, height float64) (float64, float64) {
	scale := dpiScale(1)
	if monitor := ebiten.Monitor(); monitor != nil {
		scale = dpiScale(monitor.DeviceScaleFactor())
	}
	if w.deviceScale != scale {
		w.deviceScale = scale
		w.paintTheme = w.theme.scaled(float64(scale))
	}
	// Ebitengine's offscreen matches the Windows framebuffer. The view tree
	// and its input targets still use device-independent pixels.
	w.layoutSize = image.Pt(max(1, int(math.Round(width))), max(1, int(math.Round(height))))
	pixels := scale.pixel(width, height)
	return float64(max(1, pixels.X)), float64(max(1, pixels.Y))
}

func (w *gpuWindow) refresh() {
	next := w.view()
	if err := w.scene.set(workspaceContent(next)); err != nil {
		w.err = err
		return
	}
	if w.current.Title != next.Title {
		ebiten.SetWindowTitle(next.Title)
	}
	if next.Content.kind != emptyNode || w.current.Status != next.Status {
		w.status = ""
	}
	w.current = next
	if _, exists := w.scene.states[w.drag]; !exists {
		w.drag = ""
	}
	w.scene.layout(w.scene.size)
	if item, ok := w.scene.find(w.openChoice); !ok || item.node.kind != choiceNode || !item.focusable() {
		w.openChoice = ""
	} else {
		item.state.choice = min(item.state.choice, len(item.node.value.(Choice).Options)-1)
	}
	if item, ok := w.scene.find(w.openMenu); !ok || item.node.kind != menuNode || !item.focusable() {
		w.openMenu = ""
	} else {
		menu := item.node.value.(Menu)
		item.state.menu = min(item.state.menu, len(menu.Items)-1)
		if !menu.Items[item.state.menu].enabled() {
			item.state.menu = menu.firstEnabled()
		}
	}
	if dialog, ok := w.scene.topDialog(); ok {
		// Popups and drags outside a modal dialog cannot stay open behind it.
		if item, found := w.scene.find(w.openChoice); !found || item.dialog != dialog.node.key {
			w.openChoice = ""
		}
		if item, found := w.scene.find(w.openMenu); !found || item.dialog != dialog.node.key {
			w.openMenu = ""
		}
		if item, found := w.scene.find(w.drag); !found || item.dialog != dialog.node.key {
			w.drag = ""
		}
	}
}

func (w *gpuWindow) Update() (err error) {
	defer func() {
		if err != nil {
			w.accessibilityClose()
		}
	}()
	if w.ctx.Err() != nil || ebiten.IsWindowBeingClosed() {
		w.confirmInput()
		return ebiten.Termination
	}
	if w.dirty.Swap(false) {
		w.refresh()
	}
	if w.err != nil {
		return w.err
	}
	w.scene.layout(w.layoutSize)
	x, y := ebiten.CursorPositionF()
	w.pointer(w.deviceScale.logicalPoint(x, y))
	w.keyboard()
	if w.err == nil && ebiten.IsWindowVisible() {
		w.accessibilityUpdate()
	}
	return w.err
}

func (w *gpuWindow) pointer(point image.Point) {
	hit, _ := w.hit(point)
	cursor := ebiten.CursorShapeDefault
	drag, dragging := w.scene.find(w.drag)
	if (dragging && drag.node.kind == splitNode) || hit.node.kind == splitNode {
		cursor = ebiten.CursorShapeEWResize
	}
	if ebiten.CursorShape() != cursor {
		ebiten.SetCursorShape(cursor)
	}
	if !ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
		w.drag = ""
		dragging = false
	}
	switch {
	case dragging && drag.node.kind == splitNode:
		x := point.X - drag.split.Min.X - w.dragOffset
		ratio := clampSplit(x * 100 / max(1, drag.split.Dx()-drag.box.Dx()))
		if ratio != drag.state.split {
			drag.state.split = ratio
			w.scene.dirty = true
		}
	case dragging && drag.node.kind == tableNode:
		w.scrollTo(drag, point.Y)
	case dragging && drag.isText():
		drag.state.edit.move(w.textPosition(drag, point), true)
		w.revealCaret(drag)
	case inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft):
		w.click(point)
	}
	if hit.node.kind == tableNode && w.openChoice == "" && w.openMenu == "" {
		_, delta := ebiten.Wheel()
		hit.state.wheel -= delta * 3
		steps := int(hit.state.wheel)
		hit.state.wheel -= float64(steps)
		hit.state.viewport.scroll(steps)
	}
	if hit.isText() && w.openChoice == "" && w.openMenu == "" {
		_, delta := ebiten.Wheel()
		b := hit.state.edit
		b.scrollLine = max(0, min(len(b.lines)-visibleTextLines(hit), b.scrollLine-int(delta*3)))
	}
}

func (w *gpuWindow) hit(point image.Point) (placedNode, bool) {
	for i := len(w.scene.items) - 1; i >= 0; i-- {
		item := w.scene.items[i]
		if point.In(item.box) && item.state != nil {
			return item, true
		}
	}
	return placedNode{}, false
}

func (w *gpuWindow) click(point image.Point) {
	if item, open := w.scene.find(w.openChoice); open {
		w.openChoice = ""
		for i := range item.node.value.(Choice).Options {
			if point.In(item.optionBox(i)) {
				w.changeChoice(item, i)
				return
			}
		}
		return
	}
	if item, open := w.scene.find(w.openMenu); open {
		w.openMenu = ""
		menu := item.node.value.(Menu)
		for i := range menu.Items {
			if point.In(item.menuBox(i)) {
				w.pickMenu(item, i)
				return
			}
		}
		return
	}
	item, ok := w.hit(point)
	if !ok {
		return
	}
	w.confirmInput()
	item, ok = w.scene.find(item.node.key)
	if !ok {
		return
	}
	switch item.node.kind {
	case buttonNode:
		if point.In(item.controlBox()) {
			w.press(item)
		}
	case choiceNode:
		if point.In(item.controlBox()) {
			w.toggleChoice(item)
		}
	case tableNode:
		rows, scroll := item.tableBounds()
		switch {
		case point.In(rows):
			w.scene.focus = item.node.key
			w.selectRow(item, item.state.viewport.top+(point.Y-rows.Min.Y)/rowHeight)
		case point.In(scroll):
			w.scene.focus = item.node.key
			w.drag = item.node.key
			w.scrollTo(item, point.Y)
		}
	case splitNode:
		w.drag = item.node.key
		w.dragOffset = point.X - item.box.Min.X
	case detailNode:
		if point.In(item.copyBox()) && item.focusable() {
			w.scene.focus = item.node.key
			w.copyMessage(item.node.value.(Detail).Copy)
		}
	case inputNode, areaNode:
		if item.focusable() && point.In(item.editBox()) {
			w.scene.focus = item.node.key
			w.drag = item.node.key
			item.state.edit.move(w.textPosition(item, point), ebiten.IsKeyPressed(ebiten.KeyShift))
			w.revealCaret(item)
		}
	case checkboxNode, switchNode:
		w.pressToggle(item)
	case tabsNode:
		for i := range item.node.value.(Tabs).Labels {
			if point.In(tabSegment(item, i)) {
				w.scene.focus = item.node.key
				w.selectTab(item, i)
				return
			}
		}
	case menuNode:
		if point.In(item.controlBox()) {
			w.openMenuList(item)
		}
	case dialogNode:
		if !point.In(item.split) {
			w.dismissDialog(item)
		}
	}
}

func (w *gpuWindow) scrollTo(item placedNode, y int) {
	_, scroll := item.tableBounds()
	track := scroll.Inset(2)
	thumb := scrollThumb(item)
	v := &item.state.viewport
	room := track.Dy() - thumb.Dy()
	if room > 0 && v.count > v.page {
		offset := max(0, min(room, y-track.Min.Y-thumb.Dy()/2))
		v.top = int(int64(offset) * int64(v.count-v.page) / int64(room))
	}
}

func (w *gpuWindow) selectRow(item placedNode, row int) {
	table := item.node.value.(Table)
	if row >= 0 && row < table.Count && table.Select != nil {
		table.Select(row)
		w.refresh()
		if next, ok := w.scene.find(item.node.key); ok && next.state == item.state &&
			next.node.value.(Table).Key == table.Key {
			next.state.viewport.reveal(row)
		}
	}
}

func (w *gpuWindow) toggleChoice(item placedNode) {
	if !item.focusable() {
		return
	}
	choice := item.node.value.(Choice)
	item.state.choice = max(0, min(choice.Selected, len(choice.Options)-1))
	w.scene.focus = item.node.key
	w.openMenu = ""
	w.openChoice = item.node.key
}

func (w *gpuWindow) changeChoice(item placedNode, i int) {
	choice := item.node.value.(Choice)
	if choice.Change != nil && i >= 0 && i < len(choice.Options) {
		choice.Change(i)
		w.refresh()
	}
}

func (w *gpuWindow) press(item placedNode) {
	if item.focusable() {
		w.scene.focus = item.node.key
		item.node.value.(Button).Press()
		w.refresh()
	}
}

// keyEvents carries one frame of just-pressed keys so keyboard routing can
// run without a live event source.
type keyEvents struct {
	tab, shift, escape bool
	up, down           bool
	left, right        bool
	home, end          bool
	pageUp, pageDown   bool
	enter, space       bool
	controlC           bool
}

func (w *gpuWindow) keyboard() {
	if w.inputKeys() {
		return
	}
	just := inpututil.IsKeyJustPressed
	w.handleKeys(keyEvents{
		tab: just(ebiten.KeyTab), shift: ebiten.IsKeyPressed(ebiten.KeyShift),
		escape: just(ebiten.KeyEscape),
		up:     just(ebiten.KeyArrowUp), down: just(ebiten.KeyArrowDown),
		left: just(ebiten.KeyArrowLeft), right: just(ebiten.KeyArrowRight),
		home: just(ebiten.KeyHome), end: just(ebiten.KeyEnd),
		pageUp: just(ebiten.KeyPageUp), pageDown: just(ebiten.KeyPageDown),
		enter: just(ebiten.KeyEnter), space: just(ebiten.KeySpace),
		controlC: just(ebiten.KeyC) && ebiten.IsKeyPressed(ebiten.KeyControl),
	})
}

func (w *gpuWindow) handleKeys(ev keyEvents) {
	if ev.tab {
		w.confirmInput()
		w.openChoice = ""
		w.openMenu = ""
		w.scene.tab(ev.shift)
		return
	}
	activate := ev.enter || ev.space
	if item, ok := w.scene.find(w.openChoice); ok {
		switch {
		case ev.escape:
			w.openChoice = ""
		case ev.up:
			item.state.choice = max(0, item.state.choice-1)
		case ev.down:
			item.state.choice = min(len(item.node.value.(Choice).Options)-1, item.state.choice+1)
		case activate:
			w.openChoice = ""
			w.changeChoice(item, item.state.choice)
		}
		return
	}
	if item, ok := w.scene.find(w.openMenu); ok {
		w.menuKeys(item, ev, activate)
		return
	}
	if dialog, ok := w.scene.topDialog(); ok && ev.escape {
		w.dismissDialog(dialog)
		return
	}
	item, ok := w.scene.find(w.scene.focus)
	if !ok {
		return
	}
	switch item.node.kind {
	case tableNode:
		w.tableKeys(item, ev)
	case choiceNode:
		if activate || ev.down {
			w.toggleChoice(item)
		}
	case menuNode:
		if activate || ev.down {
			w.openMenuList(item)
		}
	case tabsNode:
		w.tabsKeys(item, ev)
	case detailNode:
		if activate {
			w.copyMessage(item.node.value.(Detail).Copy)
		}
	case buttonNode:
		if activate {
			w.press(item)
		}
	case checkboxNode, switchNode:
		if activate {
			w.pressToggle(item)
		}
	}
}

// selectTab reports a tab choice to the application. Re-selecting the active
// tab does nothing, so Enter on the focused strip is a no-op.
func (w *gpuWindow) selectTab(item placedNode, index int) {
	tabs := item.node.value.(Tabs)
	if item.focusable() && index >= 0 && index < len(tabs.Labels) && index != tabs.Selected {
		tabs.Change(index)
		w.refresh()
	}
}

func (w *gpuWindow) tabsKeys(item placedNode, ev keyEvents) {
	tabs := item.node.value.(Tabs)
	next := tabs.Selected
	switch {
	case ev.left:
		next = max(0, tabs.Selected-1)
	case ev.right:
		next = min(len(tabs.Labels)-1, tabs.Selected+1)
	case ev.home:
		next = 0
	case ev.end:
		next = len(tabs.Labels) - 1
	default:
		return
	}
	w.selectTab(item, next)
}

func (w *gpuWindow) openMenuList(item placedNode) {
	if !item.focusable() {
		return
	}
	item.state.menu = item.node.value.(Menu).firstEnabled()
	w.scene.focus = item.node.key
	w.openChoice = ""
	w.openMenu = item.node.key
}

func (w *gpuWindow) pickMenu(item placedNode, index int) {
	menu := item.node.value.(Menu)
	if index >= 0 && index < len(menu.Items) && menu.Items[index].enabled() {
		w.scene.focus = item.node.key
		menu.Items[index].Action()
		w.refresh()
	}
}

func (w *gpuWindow) menuKeys(item placedNode, ev keyEvents, activate bool) {
	menu := item.node.value.(Menu)
	switch {
	case ev.escape:
		w.openMenu = ""
	case ev.up:
		item.state.menu = menu.step(item.state.menu, -1)
	case ev.down:
		item.state.menu = menu.step(item.state.menu, 1)
	case activate:
		w.openMenu = ""
		w.pickMenu(item, item.state.menu)
	}
}

// dismissDialog runs the cancel callback for Escape and outside clicks.
func (w *gpuWindow) dismissDialog(item placedNode) {
	w.confirmInput()
	if onCancel := item.node.value.(Dialog).OnCancel; onCancel != nil {
		onCancel()
		w.refresh()
	}
}

func (w *gpuWindow) pressToggle(item placedNode) {
	if item.focusable() {
		w.scene.focus = item.node.key
		toggle := item.node.value.(Toggle)
		toggle.Change(!toggle.Checked)
		w.refresh()
	}
}

func (w *gpuWindow) tableKeys(item placedNode, ev keyEvents) {
	table, v := item.node.value.(Table), item.state.viewport
	row := table.Selected
	if row < 0 {
		row = v.top - 1
	}
	switch {
	case ev.up:
		w.selectRow(item, max(0, row-1))
	case ev.down:
		w.selectRow(item, row+1)
	case ev.pageUp:
		w.selectRow(item, max(0, row-v.page))
	case ev.pageDown:
		w.selectRow(item, min(table.Count-1, row+v.page))
	case ev.home:
		w.selectRow(item, 0)
	case ev.end:
		w.selectRow(item, table.Count-1)
	case ev.controlC:
		w.copyMessage(table.Copy)
	}
}

var (
	getForeground = user32.NewProc("GetForegroundWindow")
	getWindowPID  = user32.NewProc("GetWindowThreadProcessId")
)

func (w *gpuWindow) copyMessage(value string) {
	if value == "" {
		return
	}
	w.copyText(value)
}

func (w *gpuWindow) copyText(value string) bool {
	if copyForeground(value, os.Getpid()) {
		w.status = "Message copied"
		return true
	} else {
		w.status = "Could not copy the message"
	}
	return false
}
