package desktop

import (
	"fmt"
	"image"
	"os"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ---- unit tests over the provider dispatch layer ----

func accTestSnapshot(t *testing.T, checked bool) accSnapshot {
	t.Helper()
	content := VStack(8,
		Heading("Test"),
		HStack(8,
			PushButton("new", Button{Label: "New note", Press: func() {}}),
			PushButton("save", Button{Label: "Save", Disabled: true, Press: func() {}}),
		),
		TextInput("title", Field{Label: "Title", Value: "draft", Change: func(string) {}}),
		Checkbox("pin", Toggle{Label: "Pin", Checked: checked, Change: func(bool) {}}),
		Status("ready"),
	)
	var s scene
	if err := s.set(content); err != nil {
		t.Fatal(err)
	}
	s.layout(image.Pt(400, 500))
	snap := s.accSnapshot()
	snap.Title = "Unit test window"
	return snap
}

func accNewTestProvider(actions accActions) *accProvider {
	p := &accProvider{objs: map[accKey]*accElementObj{}, seed: 7, actions: actions}
	p.root = &accRootObj{}
	p.root.init(p)
	return p
}

func accPropString(t *testing.T, obj *accElementObj, prop uintptr) string {
	t.Helper()
	var v accVariant
	if hr := accCbSimpleProp(unsafe.Pointer(&obj.simple), prop, unsafe.Pointer(&v)); hr != accHROk {
		t.Fatalf("GetPropertyValue(%d) hr=0x%x", prop, hr)
	}
	if v.VT != accVtBSTR {
		t.Fatalf("property %d vt=%d, want BSTR", prop, v.VT)
	}
	s := accBSTRString(accPtr(uintptr(v.Val)))
	accBSTRFree(uintptr(v.Val))
	return s
}

func TestAccProviderPropertiesAndIdentity(t *testing.T) {
	p := accNewTestProvider(accActions{})
	p.Publish(accTestSnapshot(t, true))

	pin := p.objs[accID("pin", -1)]
	if pin == nil {
		t.Fatal("pin element object missing after publish")
	}
	if name := accPropString(t, pin, accPropName); name != "Pin" {
		t.Errorf("pin name = %q", name)
	}
	var v accVariant
	if hr := accCbSimpleProp(unsafe.Pointer(&pin.simple), accPropEnabled, unsafe.Pointer(&v)); hr != accHROk || v.VT != accVtBool || v.Val == 0 {
		t.Errorf("pin enabled: hr=0x%x vt=%d val=%d", hr, v.VT, v.Val)
	}
	if hr := accCbSimpleProp(unsafe.Pointer(&pin.simple), accPropToggleState, unsafe.Pointer(&v)); hr != accHROk || v.Val != 1 {
		t.Errorf("pin toggle state: hr=0x%x val=%d (want On=1)", hr, v.Val)
	}
	save := p.objs[accID("save", -1)]
	if hr := accCbSimpleProp(unsafe.Pointer(&save.simple), accPropEnabled, unsafe.Pointer(&v)); hr != accHROk || v.Val != 0 {
		t.Errorf("save enabled: hr=0x%x val=%d (want false)", hr, v.Val)
	}
	title := p.objs[accID("title", -1)]
	if name := accPropString(t, title, accPropValue); name != "draft" {
		t.Errorf("title value = %q", name)
	}
	rootName := func() string {
		var rv accVariant
		accCbRootProp(unsafe.Pointer(&p.root.simple), accPropName, unsafe.Pointer(&rv))
		s := accBSTRString(accPtr(uintptr(rv.Val)))
		accBSTRFree(uintptr(rv.Val))
		return s
	}
	if got := rootName(); got != "Unit test window" {
		t.Errorf("root name = %q", got)
	}

	// QueryInterface hands out the pattern interfaces the role supports.
	var ppv unsafe.Pointer
	iidToggle := accIIDToggle
	if hr := accElementQueryInterface(pin, unsafe.Pointer(&iidToggle), unsafe.Pointer(&ppv)); hr != accHROk {
		t.Errorf("QI(Toggle) hr=0x%x", hr)
	} else if ppv != unsafe.Pointer(&pin.toggle) {
		t.Errorf("QI(Toggle) returned %p, want the toggle interface", ppv)
	}
	iidValue := accIIDValue
	ppv = nil
	if hr := accElementQueryInterface(pin, unsafe.Pointer(&iidValue), unsafe.Pointer(&ppv)); hr != accHRNoInterface {
		t.Errorf("QI(Value) on a checkbox hr=0x%x", hr)
	}
	var pat unsafe.Pointer
	if hr := accCbSimplePattern(unsafe.Pointer(&pin.simple), accPatToggle, unsafe.Pointer(&pat)); hr != accHROk || pat == nil {
		t.Errorf("GetPatternProvider(Toggle) hr=0x%x", hr)
	}
	pat = nil
	if hr := accCbSimplePattern(unsafe.Pointer(&pin.simple), accPatInvoke, unsafe.Pointer(&pat)); hr != accHROk || pat != nil {
		t.Errorf("GetPatternProvider(Invoke) on checkbox hr=0x%x", hr)
	}
}

func TestAccStaticTextDoesNotBreakModalTraversal(t *testing.T) {
	view := View{Content: DialogLayer("discard", Dialog{
		Title: "Discard draft?", Content: Text("Discard this note's edits."),
		Cancel: "Keep editing", OnCancel: func() {},
	}, Node{})}
	p := accNewTestProvider(accActions{})
	p.Publish(accScene(t, view, image.Pt(900, 600)))
	var body *accElementObj
	for _, obj := range p.objs {
		if obj.el.Role == accTextRole {
			body = obj
			break
		}
	}
	if body == nil {
		t.Fatal("dialog text missing")
	}
	var fragment unsafe.Pointer
	iid := accIIDFrag
	if hr := accElementQueryInterface(body, unsafe.Pointer(&iid), unsafe.Pointer(&fragment)); hr != accHROk || fragment != unsafe.Pointer(&body.fragment) {
		t.Fatalf("dialog text does not expose fragment navigation: hr=%#x", hr)
	}
	var sibling unsafe.Pointer
	cancel := p.objs[accID("discard-cancel", -1)]
	if hr := accCbFragNavigate(fragment, accNavNext, unsafe.Pointer(&sibling)); hr != accHROk || cancel == nil || sibling != unsafe.Pointer(&cancel.fragment) {
		t.Fatalf("could not navigate from the dialog text to its cancel button: hr=%#x", hr)
	}
}

func TestAccProviderNavigationAndLifecycle(t *testing.T) {
	tableView := func() Node {
		return VStack(6,
			PushButton("new", Button{Label: "New", Press: func() {}}),
			VirtualTable("table", Table{
				Key:      "rows",
				Columns:  []Column{{Title: "c"}},
				Count:    10,
				Cell:     func(int, int) string { return "cell" },
				Select:   func(int) {},
				Selected: 1,
			}),
		)
	}
	var s scene
	if err := s.set(tableView()); err != nil {
		t.Fatal(err)
	}
	s.layout(image.Pt(400, 400))
	p := accNewTestProvider(accActions{})
	p.Publish(s.accSnapshot())

	table := p.objs[accID("table", -1)]
	row0 := p.objs[accID("table", 0)]
	button := p.objs[accID("new", -1)]
	if table == nil || row0 == nil || button == nil {
		t.Fatal("element objects missing after publish")
	}
	var container unsafe.Pointer
	if hr := accCbSelItemContainer(unsafe.Pointer(&row0.selectionItem), unsafe.Pointer(&container)); hr != accHROk || container != unsafe.Pointer(&table.simple) {
		t.Fatalf("row selection container did not return the table's simple provider: hr=%#x", hr)
	}
	accCbSimpleRelease(container)

	p.mu.Lock()
	first := p.navigateLocked(table, accNavFirst)
	parent := p.navigateLocked(row0, accNavParent)
	next := p.navigateLocked(row0, accNavNext)
	sel := p.selectedObjLocked("table")
	p.mu.Unlock()
	if first == nil || first.el.Row != 0 {
		t.Errorf("table first child row = %d", first.el.Row)
	}
	if parent == nil || parent.el.Key != "table" || parent.el.Row != -1 {
		t.Errorf("row parent = %q row=%d", parent.el.Key, parent.el.Row)
	}
	if next == nil || next.el.Row != 1 {
		t.Errorf("row 0 next sibling row = %d", next.el.Row)
	}
	if sel == nil || sel.el.Row != 1 {
		t.Errorf("selected object row = %d", sel.el.Row)
	}

	// Republishing without the button retires it.
	trimmed := VStack(6,
		VirtualTable("table", Table{Key: "rows", Columns: []Column{{Title: "c"}}, Count: 10,
			Cell: func(int, int) string { return "cell" }, Select: func(int) {}}),
	)
	if err := s.set(trimmed); err != nil {
		t.Fatal(err)
	}
	s.layout(image.Pt(400, 400))
	p.Publish(s.accSnapshot())
	p.mu.Lock()
	dead := button.dead
	p.mu.Unlock()
	if !dead {
		t.Error("removed button not marked dead")
	}
	var v accVariant
	if hr := accCbSimpleProp(unsafe.Pointer(&button.simple), accPropName, unsafe.Pointer(&v)); hr != accHRElementGone {
		t.Errorf("property on dead element hr=0x%x", hr)
	}
}

func TestAccScrolledRowsRetireUntilClientReleasesThem(t *testing.T) {
	p := accNewTestProvider(accActions{})
	snapshot := func(row int) accSnapshot {
		return accSnapshot{Focus: -1, Elements: []accElement{
			{Key: "table", Role: accListRole, Row: -1, Parent: -1},
			{Key: "table", Role: accRowRole, Row: row, Parent: 0},
		}}
	}
	p.Publish(snapshot(0))
	row := p.objs[accID("table", 0)]
	if count := accCbSimpleAddRef(unsafe.Pointer(&row.simple)); count != 1 {
		t.Fatalf("row reference count = %d, want 1", count)
	}
	for i := 1; i <= 500; i++ {
		p.Publish(snapshot(i))
	}
	if len(p.objs) != 2 || !row.dead {
		t.Fatalf("provider kept %d active objects after scrolling, old row retired=%t", len(p.objs), row.dead)
	}
	if _, retained := accReferenced.Load(row); !retained {
		t.Fatal("a client-held row was released while still referenced")
	}
	p.Publish(snapshot(0))
	if current := p.objs[accID("table", 0)]; current == row || current.id == row.id {
		t.Fatal("a retired row was made current again")
	}
	accCbSimpleRelease(unsafe.Pointer(&row.simple))
	if _, retained := accReferenced.Load(row); retained {
		t.Fatal("the provider still retains the retired row after Release")
	}
}

func TestAccProviderActionsQueue(t *testing.T) {
	var mu sync.Mutex
	var log []string
	record := func(format string, args ...any) {
		mu.Lock()
		log = append(log, fmt.Sprintf(format, args...))
		mu.Unlock()
	}
	p := accNewTestProvider(accActions{
		Invoke:   func(key string) { record("invoke:%s", key) },
		Toggle:   func(key string) { record("toggle:%s", key) },
		SetText:  func(key, value string) { record("set:%s=%s", key, value) },
		Select:   func(key string, row int) { record("select:%s/%d", key, row) },
		SetFocus: func(key string) { record("focus:%s", key) },
	})
	p.Publish(accTestSnapshot(t, false))

	button := p.objs[accID("new", -1)]
	pin := p.objs[accID("pin", -1)]
	title := p.objs[accID("title", -1)]
	value := accBSTRAlloc("typed")
	accCbInvoke(unsafe.Pointer(&button.invoke))
	accCbToggleToggle(unsafe.Pointer(&pin.toggle))
	accCbValueSet(unsafe.Pointer(&title.value), accPtr(value))
	accCbFragSetFocus(unsafe.Pointer(&pin.fragment))
	accBSTRFree(uintptr(value))
	p.mu.Lock()
	queued := len(p.queue)
	p.mu.Unlock()
	if queued != 4 {
		t.Fatalf("queued actions = %d, want 4", queued)
	}

	// Row selection enqueues on the row element, not the table.
	p.Publish(func() accSnapshot {
		var s scene
		if err := s.set(VStack(6, VirtualTable("table", Table{
			Key: "rows", Columns: []Column{{Title: "c"}}, Count: 3,
			Cell: func(int, int) string { return "x" }, Select: func(int) {},
		}))); err != nil {
			t.Fatal(err)
		}
		s.layout(image.Pt(400, 300))
		snap := s.accSnapshot()
		snap.Title = "rows"
		return snap
	}())
	if p.objs[accID("table", -1)] == nil {
		t.Fatal("table element missing")
	}
	row1 := p.objs[accID("table", 1)]
	if row1 == nil {
		t.Fatal("row 1 missing")
	}
	accSelItemPick(unsafe.Pointer(&row1.selectionItem))

	p.Drain()
	mu.Lock()
	defer mu.Unlock()
	want := []string{"invoke:new", "toggle:pin", "set:title=typed", "focus:pin", "select:table/1"}
	if fmt.Sprint(log) != fmt.Sprint(want) {
		t.Fatalf("drained actions = %v, want %v", log, want)
	}
}

// ---- end-to-end: a real UI Automation client reads the provider ----

var (
	accCoCreate    = accOle32.NewProc("CoCreateInstance")
	accSysFreeBSTR = accOleaut.NewProc("SysFreeString")
	accPeekMessage = user32.NewProc("PeekMessageW")
	accDestroyWin  = user32.NewProc("DestroyWindow")

	accCLSIDUiautomation = windows.GUID{Data1: 0xFF48DBA4, Data2: 0x60EF, Data3: 0x4201, Data4: [8]byte{0xAA, 0x87, 0x54, 0x10, 0x3E, 0xEF, 0x59, 0x4E}}
	accIIDUIAutomation   = windows.GUID{Data1: 0x30CBE57D, Data2: 0xD9D0, Data3: 0x452A, Data4: [8]byte{0xAB, 0x13, 0x7A, 0xC5, 0xAC, 0x48, 0x25, 0xEE}}
)

type accMsg struct {
	Hwnd    uintptr
	Message uint32
	_       uint32
	Wparam  uintptr
	Lparam  uintptr
	Time    uint32
	_       uint32
	Pt      point
}

type accWndClassEx struct {
	Size                uint32
	Style               uint32
	WndProc             uintptr
	ClsExtra, WndExtra  int32
	Instance            uintptr
	Icon, Cursor, Brush uintptr
	MenuName, ClassName *uint16
	IconSm              uintptr
}

var accTestWndProc = syscall.NewCallback(func(hwnd, msg, wparam, lparam uintptr) uintptr {
	r, _, _ := defWindowProc.Call(hwnd, msg, wparam, lparam)
	return r
})

// accCallV calls a COM method by vtable index (IUnknown slots included).
func accCallV(obj unsafe.Pointer, index int, args ...uintptr) (uintptr, syscall.Errno) {
	vt := *(*unsafe.Pointer)(obj)
	fn := (*[64]uintptr)(vt)[index]
	switch len(args) {
	case 0:
		r1, _, err := syscall.SyscallN(fn, uintptr(obj))
		return r1, err
	case 1:
		r1, _, err := syscall.SyscallN(fn, uintptr(obj), args[0])
		return r1, err
	case 2:
		r1, _, err := syscall.SyscallN(fn, uintptr(obj), args[0], args[1])
		return r1, err
	default:
		r1, _, err := syscall.SyscallN(fn, uintptr(obj), args[0], args[1], args[2])
		return r1, err
	}
}

// accCallV1 calls a COM method and returns its first result.
func accCallV1(obj unsafe.Pointer, index int, args ...uintptr) uintptr {
	v, _ := accCallV(obj, index, args...)
	return v
}

// accPtr reinterprets a pointer value returned by a COM call as an
// unsafe.Pointer without a flagged conversion. The pointee stays alive
// through COM reference counting (client objects) or the provider's object
// maps (server objects).
func accPtr(u uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&u))
}

func accClientBSTR(p uintptr) string {
	s := accBSTRString(accPtr(p))
	accSysFreeBSTR.Call(p)
	return s
}

// TestAccUIAClientEndToEnd drives the provider through the Windows UI
// Automation client API the way a screen reader does: it creates a real
// window, attaches the provider, and reads names, control types, states and
// patterns with the CUIAutomation client.
//
// The client calls run on the window-owning thread, one call per pump
// iteration, because cross-apartment property calls into a thread that only
// services messages through PeekMessage are not delivered by COM. A real
// application pumps GetMessage, where cross-thread clients work; the test
// exercises the same provider stack single-threaded instead.
//
// It skips when COM or UI Automation is unavailable (for example in a
// session without an interactive desktop).
func TestAccUIAClientEndToEnd(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	module, _, _ := getModule.Call(0, 0)
	class := windows.StringToUTF16Ptr(fmt.Sprintf("GofastrAccTest%d", os.Getpid()))
	wc := accWndClassEx{
		Size:      uint32(unsafe.Sizeof(accWndClassEx{})),
		WndProc:   accTestWndProc,
		Instance:  module,
		ClassName: class,
	}
	if r, _, _ := registerClass.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		t.Skip("could not register a window class")
	}
	title := windows.StringToUTF16Ptr("Accessibility test window")
	hwnd, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)),
		0x00CF0000 /*WS_OVERLAPPEDWINDOW*/, 100, 100, 420, 460, 0, 0, module, 0)
	if hwnd == 0 {
		t.Skipf("could not create a window: %v", err)
	}
	defer accDestroyWin.Call(hwnd)

	var mu sync.Mutex
	var invoked, toggled bool
	p, err := accAttachProvider(hwnd, accActions{
		Invoke: func(string) { mu.Lock(); invoked = true; mu.Unlock() },
		Toggle: func(string) { mu.Lock(); toggled = true; mu.Unlock() },
	})
	if err != nil {
		t.Skipf("UI Automation provider unavailable: %v", err)
	}
	defer p.Close()
	snapshot := func(checked bool) accSnapshot {
		snap := accTestSnapshot(t, checked)
		snap.Title = "Accessibility test window"
		return snap
	}
	p.Publish(snapshot(false))

	pump := func() {
		var m accMsg
		for {
			r, _, _ := accPeekMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, 1 /*PM_REMOVE*/)
			if r == 0 {
				return
			}
			translateMessage.Call(uintptr(unsafe.Pointer(&m)))
			dispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
		}
	}

	// The client runs one call per iteration so the pump keeps servicing
	// messages between COM round trips.
	var uia, root, walker unsafe.Pointer
	type element struct {
		name         string
		ctl, enabled int32
		ptr          unsafe.Pointer
	}
	var elements []element
	byName := map[string]element{}
	var pattern unsafe.Pointer
	var state int32
	steps := []struct {
		name string
		run  func() error
	}{
		{"connect", func() error {
			var iface unsafe.Pointer
			r, _, _ := accCoCreate.Call(uintptr(unsafe.Pointer(&accCLSIDUiautomation)), 0, 1, /*CLSCTX_INPROC_SERVER*/
				uintptr(unsafe.Pointer(&accIIDUIAutomation)), uintptr(unsafe.Pointer(&iface)))
			if int32(r) < 0 || iface == nil {
				return fmt.Errorf("CoCreateInstance(CUIAutomation) hr=0x%x (no interactive desktop?)", r)
			}
			uia = iface
			return nil
		}},
		{"root", func() error {
			var slot uintptr
			accCallV(uia, 6 /*ElementFromHandle*/, hwnd, uintptr(unsafe.Pointer(&slot)))
			root = accPtr(slot)
			if root == nil {
				return fmt.Errorf("ElementFromHandle returned nothing")
			}
			return nil
		}},
		{"root name", func() error {
			var b uintptr
			accCallV(root, 23 /*CurrentName*/, uintptr(unsafe.Pointer(&b)))
			if got := accClientBSTR(b); got != "Accessibility test window" {
				return fmt.Errorf("root name = %q", got)
			}
			return nil
		}},
		{"walker", func() error {
			var slot uintptr
			accCallV(uia, 14 /*ControlViewWalker*/, uintptr(unsafe.Pointer(&slot)))
			walker = accPtr(slot)
			if walker == nil {
				return fmt.Errorf("ControlViewWalker returned nothing")
			}
			return nil
		}},
		{"first child", func() error {
			var slot uintptr
			accCallV(walker, 4 /*GetFirstChildElement*/, uintptr(root), uintptr(unsafe.Pointer(&slot)))
			child := accPtr(slot)
			for child != nil {
				var b uintptr
				accCallV(child, 23 /*CurrentName*/, uintptr(unsafe.Pointer(&b)))
				var ctl, enabled int32
				accCallV(child, 21 /*CurrentControlType*/, uintptr(unsafe.Pointer(&ctl)))
				accCallV(child, 28 /*CurrentIsEnabled*/, uintptr(unsafe.Pointer(&enabled)))
				e := element{accClientBSTR(b), ctl, enabled, child}
				elements = append(elements, e)
				byName[e.name] = e
				slot = 0
				accCallV(walker, 6 /*GetNextSiblingElement*/, uintptr(child), uintptr(unsafe.Pointer(&slot)))
				child = accPtr(slot)
			}
			if len(elements) == 0 {
				return fmt.Errorf("no children visible under the window element")
			}
			return nil
		}},
		{"control types", func() error {
			for _, check := range []struct {
				name    string
				ctl     int32
				enabled int32
			}{
				{"New note", 50000, 1},
				{"Save", 50000, 0},
				{"Title", 50004, 1},
				{"Pin", 50002, 1},
				{"ready", 50020, 1},
			} {
				e, ok := byName[check.name]
				if !ok {
					return fmt.Errorf("element %q not found among %d children", check.name, len(elements))
				}
				if e.ctl != check.ctl {
					return fmt.Errorf("%q control type = %d, want %d", check.name, e.ctl, check.ctl)
				}
				if e.enabled != check.enabled {
					return fmt.Errorf("%q enabled = %d, want %d", check.name, e.enabled, check.enabled)
				}
			}
			return nil
		}},
		{"invoke pattern", func() error {
			var slot uintptr
			hr, _ := accCallV(byName["New note"].ptr, 16 /*GetCurrentPattern*/, 10000 /*Invoke*/, uintptr(unsafe.Pointer(&slot)))
			pattern = accPtr(slot)
			if int32(hr) < 0 || pattern == nil {
				return fmt.Errorf("GetCurrentPattern(Invoke) hr=0x%x", hr)
			}
			return nil
		}},
		{"invoke", func() error {
			accCallV(pattern, 3 /*Invoke*/)
			return nil
		}},
		{"invoke drained", func() error {
			mu.Lock()
			defer mu.Unlock()
			if !invoked {
				return fmt.Errorf("Invoke pattern did not reach the provider actions")
			}
			return nil
		}},
		{"toggle pattern", func() error {
			var slot uintptr
			hr, _ := accCallV(byName["Pin"].ptr, 16 /*GetCurrentPattern*/, 10015 /*Toggle*/, uintptr(unsafe.Pointer(&slot)))
			pattern = accPtr(slot)
			if int32(hr) < 0 || pattern == nil {
				return fmt.Errorf("GetCurrentPattern(Toggle) hr=0x%x", hr)
			}
			return nil
		}},
		{"toggle off", func() error {
			accCallV(pattern, 4 /*ToggleState*/, uintptr(unsafe.Pointer(&state)))
			if state != 0 {
				return fmt.Errorf("Pin toggle state = %d, want Off=0", state)
			}
			return nil
		}},
		{"toggle", func() error {
			accCallV(pattern, 3 /*Toggle*/)
			return nil
		}},
		{"toggle drained", func() error {
			mu.Lock()
			defer mu.Unlock()
			if !toggled {
				return fmt.Errorf("Toggle pattern did not reach the provider actions")
			}
			return nil
		}},
		{"republish", func() error {
			p.Publish(snapshot(true))
			return nil
		}},
		{"toggle on", func() error {
			accCallV(pattern, 4 /*ToggleState*/, uintptr(unsafe.Pointer(&state)))
			if state != 1 {
				return fmt.Errorf("Pin toggle state after republish = %d, want On=1", state)
			}
			return nil
		}},
	}
	for _, step := range steps {
		if err := step.run(); err != nil {
			t.Errorf("%s: %v", step.name, err)
			return
		}
		p.Drain() // the renderer drains queued pattern actions on the UI thread
		pump()
	}
}
