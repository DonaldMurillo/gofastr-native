package desktop

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wmDestroy       = 0x0002
	wmSize          = 0x0005
	wmClose         = 0x0010
	wmSetFont       = 0x0030
	wmNotify        = 0x004e
	wmCommand       = 0x0111
	wmMouseMove     = 0x0200
	wmLeftDown      = 0x0201
	wmLeftUp        = 0x0202
	wmCaptureChange = 0x0215
	wmRefresh       = 0x8001
	wmSelect        = 0x8002

	lvmBase          = 0x1000
	lvmGetNext       = lvmBase + 12
	lvmEnsureVisible = lvmBase + 19
	lvmSetState      = lvmBase + 43
	lvmSetCount      = lvmBase + 47
	lvmSetExtended   = lvmBase + 54
	lvmInsertColumn  = lvmBase + 97
	lvnItemChanged   = -101
	lvnStateChanged  = -115
	lvnGetDisplay    = -177
	lvnKeyDown       = -155
	lvSelected       = 2
	lvFocused        = 1
	lvText           = 1

	idFilter = 10
	idCopy   = 11
	idTable  = 12
	idButton = 100
)

var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	gdi32            = windows.NewLazySystemDLL("gdi32.dll")
	registerClass    = user32.NewProc("RegisterClassExW")
	createWindow     = user32.NewProc("CreateWindowExW")
	destroyWindow    = user32.NewProc("DestroyWindow")
	defWindowProc    = user32.NewProc("DefWindowProcW")
	showWindow       = user32.NewProc("ShowWindow")
	getMessage       = user32.NewProc("GetMessageW")
	dispatchMessage  = user32.NewProc("DispatchMessageW")
	translateMessage = user32.NewProc("TranslateMessage")
	isDialogMessage  = user32.NewProc("IsDialogMessageW")
	setFocus         = user32.NewProc("SetFocus")
	postMessage      = user32.NewProc("PostMessageW")
	sendMessage      = user32.NewProc("SendMessageW")
	moveWindow       = user32.NewProc("MoveWindow")
	getClientRect    = user32.NewProc("GetClientRect")
	setWindowText    = user32.NewProc("SetWindowTextW")
	enableWindow     = user32.NewProc("EnableWindow")
	invalidateRect   = user32.NewProc("InvalidateRect")
	loadCursor       = user32.NewProc("LoadCursorW")
	setCapture       = user32.NewProc("SetCapture")
	releaseCapture   = user32.NewProc("ReleaseCapture")
	getKeyState      = user32.NewProc("GetKeyState")
	openClipboard    = user32.NewProc("OpenClipboard")
	closeClipboard   = user32.NewProc("CloseClipboard")
	emptyClipboard   = user32.NewProc("EmptyClipboard")
	setClipboardData = user32.NewProc("SetClipboardData")
	getModule        = kernel32.NewProc("GetModuleHandleW")
	globalAlloc      = kernel32.NewProc("GlobalAlloc")
	globalLock       = kernel32.NewProc("GlobalLock")
	globalUnlock     = kernel32.NewProc("GlobalUnlock")
	globalFree       = kernel32.NewProc("GlobalFree")
	stockObject      = gdi32.NewProc("GetStockObject")
	initControls     = windows.NewLazySystemDLL("comctl32.dll").NewProc("InitCommonControlsEx")
	setDPIAwareness  = user32.NewProc("SetProcessDpiAwarenessContext")
	getDPI           = user32.NewProc("GetDpiForWindow")
)

type rect struct{ Left, Top, Right, Bottom int32 }
type point struct{ X, Y int32 }

type message struct {
	Window  uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Point   point
	Private uint32
}

type windowClass struct {
	Size, Style                   uint32
	Proc                          uintptr
	ClassExtra, WindowExtra       int32
	Instance, Icon, Cursor, Brush uintptr
	Menu, Name                    *uint16
	SmallIcon                     uintptr
}

type notification struct {
	From, ID uintptr
	Code     int32
}

type listItem struct {
	Mask             uint32
	Index, Column    int32
	State, StateMask uint32
	Text             *uint16
	TextLimit, Image int32
	Param            uintptr
	Indent, Group    int32
	ColumnCount      uint32
	Columns, Formats uintptr
	GroupIndex       int32
}

type displayInfo struct {
	Header notification
	Item   listItem
}

type listColumn struct {
	Mask          uint32
	Format, Width int32
	Text          *uint16
	TextLimit     int32
	SubItem       int32
	Image, Order  int32
	Min, Default  int32
	Ideal         int32
}

type keyDown struct {
	Header notification
	Key    uint16
	Flags  uint32
}

const windowClassName = "GoFastrDesktopWorkspace"

var windowsByHandle sync.Map

var initWindowClass = sync.OnceValues(func() (uintptr, error) {
	if setDPIAwareness.Find() == nil {
		setDPIAwareness.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2
	}
	settings := [2]uint32{8, 1} // ICC_LISTVIEW_CLASSES
	if ok, _, err := initControls.Call(uintptr(unsafe.Pointer(&settings))); ok == 0 {
		return 0, fmt.Errorf("initialize list control: %w", err)
	}
	instance, _, err := getModule.Call(0)
	if instance == 0 {
		return 0, err
	}
	cursor, _, _ := loadCursor.Call(0, 32512) // IDC_ARROW
	class := windowClass{
		Proc: windows.NewCallback(windowProc), Instance: instance,
		Cursor: cursor, Brush: 6, Name: wide(windowClassName), // COLOR_WINDOW + 1
	}
	class.Size = uint32(unsafe.Sizeof(class))
	if ok, _, err := registerClass.Call(uintptr(unsafe.Pointer(&class))); ok == 0 {
		return 0, fmt.Errorf("register desktop window: %w", err)
	}
	runtime.KeepAlive(class.Name)
	return instance, nil
})

type nativeWindow struct {
	hwnd, filter, table, detailTitle, detail, copy, status, divider uintptr
	buttons                                                         []uintptr
	view                                                            func() View
	current                                                         View
	closed, reconciling, dragging                                   bool
	refreshPending                                                  atomic.Bool
	split, splitX                                                   int
}

func runNative(ctx context.Context, view func() View, updates <-chan struct{}) error {
	if view == nil {
		return errors.New("desktop: a view function is required")
	}
	if ctx.Err() != nil {
		return nil
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	w, err := openWindow(view)
	if err != nil {
		return err
	}
	defer w.close()
	done := make(chan struct{})
	watcherDone := make(chan struct{})
	go w.watch(ctx, done, watcherDone, updates)
	defer func() {
		close(done)
		<-watcherDone
	}()
	showWindow.Call(w.hwnd, 5) // SW_SHOW
	setFocus.Call(w.table)
	for !w.closed {
		var msg message
		pointer := unsafe.Pointer(&msg)
		ok, _, err := getMessage.Call(uintptr(pointer), 0, 0, 0)
		switch int32(ok) {
		case -1:
			return fmt.Errorf("read desktop event: %w", err)
		case 0:
			return nil
		}
		if handled, _, _ := isDialogMessage.Call(w.hwnd, uintptr(pointer)); handled == 0 {
			translateMessage.Call(uintptr(pointer))
			dispatchMessage.Call(uintptr(pointer))
		}
		runtime.KeepAlive(&msg)
	}
	return nil
}

func openWindow(view func() View) (*nativeWindow, error) {
	instance, err := initWindowClass()
	if err != nil {
		return nil, err
	}
	initial := view()
	class, title := wide(windowClassName), wide(initial.Title)
	const defaultPosition = 0x80000000
	hwnd, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)),
		0x00cf0000, defaultPosition, defaultPosition, 1120, 700, 0, 0, instance, 0)
	runtime.KeepAlive(class)
	runtime.KeepAlive(title)
	if hwnd == 0 {
		return nil, fmt.Errorf("create desktop window: %w", err)
	}
	w := &nativeWindow{hwnd: hwnd, view: view, split: 68}
	windowsByHandle.Store(hwnd, w)
	if err := w.createControls(initial); err != nil {
		w.close()
		return nil, err
	}
	w.render(initial)
	w.layout()
	return w, nil
}

func (w *nativeWindow) close() {
	if !w.closed {
		destroyWindow.Call(w.hwnd)
	}
	windowsByHandle.Delete(w.hwnd)
}

func (w *nativeWindow) watch(ctx context.Context, done <-chan struct{}, stopped chan<- struct{}, updates <-chan struct{}) {
	defer close(stopped)
	for {
		select {
		case <-ctx.Done():
			postMessage.Call(w.hwnd, wmClose, 0, 0)
			return
		case <-done:
			return
		case _, open := <-updates:
			if !open {
				updates = nil
			} else if !w.refreshPending.Swap(true) {
				postMessage.Call(w.hwnd, wmRefresh, 0, 0)
			}
		}
	}
}

func (w *nativeWindow) createControls(initial View) error {
	const (
		tabStop = 0x10000
		border  = 0x800000
		vscroll = 0x200000
	)
	font, _, _ := stockObject.Call(17) // DEFAULT_GUI_FONT
	create := func(class string, id, style uintptr) uintptr {
		name := wide(class)
		hwnd, _, _ := createWindow.Call(0, uintptr(unsafe.Pointer(name)), 0, 0x50000000|style,
			0, 0, 0, 0, w.hwnd, id, 0, 0)
		runtime.KeepAlive(name)
		if hwnd != 0 {
			send(hwnd, wmSetFont, font, 0)
		}
		return hwnd
	}
	w.filter = create("COMBOBOX", idFilter, tabStop|vscroll|3)
	for i := range initial.Buttons {
		w.buttons = append(w.buttons, create("BUTTON", uintptr(idButton+i), tabStop))
	}
	w.table = create("SysListView32", idTable, tabStop|border|0x100d) // REPORT, SINGLESEL, SHOWSELALWAYS, OWNERDATA
	w.detailTitle = create("STATIC", 0, 0)
	w.detail = create("EDIT", 0, tabStop|border|vscroll|0x0804) // READONLY, MULTILINE
	w.copy = create("BUTTON", idCopy, tabStop)
	w.status = create("STATIC", 0, 0)
	w.divider = create("STATIC", 0, 0x11) // SS_ETCHEDVERT
	for _, hwnd := range append([]uintptr{w.filter, w.table, w.detailTitle, w.detail, w.copy, w.status, w.divider}, w.buttons...) {
		if hwnd == 0 {
			return errors.New("desktop: could not create a workspace control")
		}
	}
	setText(w.copy, "Copy message")
	send(w.table, 0x2005, 1, 0)               // CCM_SETUNICODEFORMAT
	send(w.table, lvmSetExtended, 0x20, 0x20) // LVS_EX_FULLROWSELECT
	for _, option := range initial.Filter.Options {
		sendPtr(w.filter, 0x143, 0, unsafe.Pointer(wide(option))) // CB_ADDSTRING
	}
	for i, spec := range initial.Table.Columns {
		col := listColumn{Mask: 7, Width: int32(w.px(spec.Width)), Text: wide(spec.Title)}
		sendPtr(w.table, lvmInsertColumn, uintptr(i), unsafe.Pointer(&col))
	}
	return nil
}

func (w *nativeWindow) render(next View) {
	old := w.current
	w.current = next
	w.reconciling = true
	defer func() { w.reconciling = false }()
	if next.Title != old.Title {
		setText(w.hwnd, next.Title)
	}
	for i, control := range w.buttons {
		if i >= len(next.Buttons) {
			enableWindow.Call(control, 0)
			continue
		}
		button := next.Buttons[i]
		if i >= len(old.Buttons) || button.Label != old.Buttons[i].Label {
			setText(control, button.Label)
		}
		if i >= len(old.Buttons) || button.Disabled != old.Buttons[i].Disabled {
			enableWindow.Call(control, flag(!button.Disabled))
		}
	}
	if old.Table.Cell == nil || next.Filter.Selected != old.Filter.Selected {
		send(w.filter, 0x14e, uintptr(next.Filter.Selected), 0) // CB_SETCURSEL
	}
	if next.Detail.Title != old.Detail.Title {
		setText(w.detailTitle, next.Detail.Title)
	}
	if next.Detail.Text != old.Detail.Text {
		setText(w.detail, next.Detail.Text)
	}
	if old.Table.Cell == nil || (next.Detail.Copy == "") != (old.Detail.Copy == "") {
		enableWindow.Call(w.copy, flag(next.Detail.Copy != ""))
	}
	if next.Status != old.Status {
		setText(w.status, next.Status)
	}

	reset := next.Table.Key != old.Table.Key
	if reset || next.Table.Count != old.Table.Count {
		send(w.table, lvmSetCount, uintptr(next.Table.Count), 3) // preserve viewport and cached paint
	}
	if reset {
		w.selectRow(-1, 0)
		send(w.table, lvmEnsureVisible, 0, 0)
		invalidateRect.Call(w.table, 0, 1)
	}
	if (reset || next.Table.Selected != old.Table.Selected) && next.Table.Selected != w.selectedRow() {
		w.selectRow(-1, 0)
		if next.Table.Selected >= 0 {
			w.selectRow(next.Table.Selected, lvSelected|lvFocused)
		}
	}
	if next.Table.Follow && (reset || next.Table.Count != old.Table.Count || !old.Table.Follow) {
		w.followTail()
	}
}

func (w *nativeWindow) followTail() {
	if w.current.Table.Count > 0 {
		send(w.table, lvmEnsureVisible, uintptr(w.current.Table.Count-1), 0)
	}
}

func (w *nativeWindow) selectRow(row int, state uint32) {
	item := listItem{State: state, StateMask: lvSelected | lvFocused}
	sendPtr(w.table, lvmSetState, uintptr(row), unsafe.Pointer(&item))
}

func (w *nativeWindow) selectedRow() int {
	return int(int32(send(w.table, lvmGetNext, ^uintptr(0), lvSelected)))
}

func (w *nativeWindow) px(dp int) int {
	if getDPI.Find() == nil {
		if dpi, _, _ := getDPI.Call(w.hwnd); dpi > 0 {
			return (dp*int(dpi) + 48) / 96
		}
	}
	return dp
}

func (w *nativeWindow) layout() {
	var area rect
	getClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&area)))
	width, height := int(area.Right), int(area.Bottom)
	margin, pad, line := w.px(12), w.px(8), w.px(30)
	x := margin
	move(w.filter, x, margin, w.px(120), w.px(220))
	x += w.px(120) + pad
	for _, button := range w.buttons {
		move(button, x, margin, w.px(165), line)
		x += w.px(165) + pad
	}
	bodyY := margin + line + pad
	bodyHeight := max(0, height-bodyY-line-pad)
	usable := max(0, width-margin*2-pad*2)
	tableWidth := usable * w.split / 100
	w.splitX = margin + tableWidth + pad
	move(w.table, margin, bodyY, tableWidth, bodyHeight)
	move(w.divider, w.splitX-1, bodyY, 2, bodyHeight)
	detailX, detailWidth := w.splitX+pad, usable-tableWidth
	move(w.detailTitle, detailX, bodyY, detailWidth, line)
	move(w.detail, detailX, bodyY+line, detailWidth, bodyHeight-line*2-pad)
	move(w.copy, detailX, bodyY+bodyHeight-line, min(detailWidth, w.px(125)), line)
	move(w.status, margin, height-line, max(0, width-margin*2), line)
	if w.current.Table.Follow && !w.dragging {
		w.followTail()
	}
}

func windowProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	if item, ok := windowsByHandle.Load(hwnd); ok {
		w := item.(*nativeWindow)
		switch msg {
		case wmDestroy:
			w.closed = true
			return 0
		case wmSize:
			w.layout()
			return 0
		case wmRefresh:
			w.refreshPending.Store(false)
			w.render(w.view())
			return 0
		case wmSelect:
			row := w.selectedRow()
			if row != w.current.Table.Selected && w.current.Table.Select != nil {
				w.current.Table.Select(row)
				w.render(w.view())
			}
			return 0
		case wmCommand:
			w.command(int(uint16(wparam)), uint16(wparam>>16))
			return 0
		case wmNotify:
			if lparam != 0 {
				// WM_NOTIFY supplies a pointer valid for this callback. Other
				// messages may use lparam as an integer or packed coordinates.
				w.notify(*(**notification)(unsafe.Pointer(&lparam)))
			}
			return 0
		case wmLeftDown:
			x := int(int16(lparam))
			if x >= w.splitX-w.px(8) && x <= w.splitX+w.px(8) {
				w.dragging = true
				setCapture.Call(hwnd)
			}
		case wmMouseMove:
			if w.dragging {
				w.drag(int(int16(lparam)))
			}
		case wmLeftUp:
			if w.dragging {
				releaseCapture.Call()
			}
		case wmCaptureChange:
			w.dragging = false
		}
	}
	result, _, _ := defWindowProc.Call(hwnd, uintptr(msg), wparam, lparam)
	return result
}

func (w *nativeWindow) drag(x int) {
	var area rect
	getClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&area)))
	if width := int(area.Right) - w.px(40); width > 0 {
		w.split = max(35, min(82, (x-w.px(20))*100/width))
		w.layout()
	}
}

func (w *nativeWindow) command(id int, code uint16) {
	switch {
	case id == idFilter && code == 1 && w.current.Filter.Change != nil: // CBN_SELCHANGE
		index := int(int32(send(w.filter, 0x147, 0, 0))) // CB_GETCURSEL
		w.current.Filter.Change(index)
		w.render(w.view())
	case id == idCopy && code == 0:
		w.copyMessage()
	case id >= idButton && id-idButton < len(w.current.Buttons) && code == 0:
		button := w.current.Buttons[id-idButton]
		if !button.Disabled && button.Press != nil {
			button.Press()
			w.render(w.view())
		}
	}
}

func (w *nativeWindow) notify(n *notification) {
	if n.From != w.table {
		return
	}
	switch n.Code {
	case lvnGetDisplay:
		item := &(*displayInfo)(unsafe.Pointer(n)).Item
		if item.Mask&lvText != 0 && item.TextLimit > 0 && item.Text != nil && w.current.Table.Cell != nil {
			buffer := unsafe.Slice(item.Text, int(item.TextLimit))
			text := w.current.Table.Cell(int(item.Index), int(item.Column))
			end := copy(buffer[:len(buffer)-1], utf16.Encode([]rune(text)))
			if end > 0 && buffer[end-1] >= 0xd800 && buffer[end-1] <= 0xdbff {
				end--
			}
			buffer[end] = 0
		}
	case lvnItemChanged, lvnStateChanged:
		if !w.reconciling && w.selectedRow() != w.current.Table.Selected {
			postMessage.Call(w.hwnd, wmSelect, 0, 0)
		}
	case lvnKeyDown:
		if (*keyDown)(unsafe.Pointer(n)).Key == 'C' {
			ctrl, _, _ := getKeyState.Call(0x11) // VK_CONTROL
			if int16(ctrl) < 0 {
				w.copyMessage()
			}
		}
	}
}

func (w *nativeWindow) copyMessage() {
	text := w.current.Detail.Copy
	// A selection notification can be queued behind a key event.
	if row := w.selectedRow(); row != w.current.Table.Selected && w.current.Table.Select != nil {
		w.current.Table.Select(row)
		w.render(w.view())
		text = w.current.Detail.Copy
	}
	if text == "" {
		return
	}
	if writeClipboard(w.hwnd, text) {
		setText(w.status, "Message copied")
	} else {
		setText(w.status, "Could not copy the message")
	}
}

func send(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	result, _, _ := sendMessage.Call(hwnd, uintptr(message), wparam, lparam)
	return result
}

func sendPtr(hwnd uintptr, message uint32, wparam uintptr, lparam unsafe.Pointer) uintptr {
	result, _, _ := sendMessage.Call(hwnd, uintptr(message), wparam, uintptr(lparam))
	return result
}

func move(hwnd uintptr, x, y, width, height int) {
	moveWindow.Call(hwnd, uintptr(x), uintptr(y), uintptr(max(0, width)), uintptr(max(0, height)), 1)
}

func flag(value bool) uintptr {
	if value {
		return 1
	}
	return 0
}

func wide(value string) *uint16 {
	result, _ := windows.UTF16PtrFromString(value)
	return result
}

func setText(hwnd uintptr, text string) {
	setWindowText.Call(hwnd, uintptr(unsafe.Pointer(wide(text))))
}
