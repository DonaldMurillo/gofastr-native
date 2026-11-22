package desktop

// Windows UI Automation provider for the GPU desktop renderer.
//
// The renderer paints the whole window with DirectX, so assistive technology
// receives no control information from the system. This file implements a
// UI Automation server-side provider in pure Go: COM objects built from
// syscall callbacks, exported DLL entry points from uiautomationcore.dll,
// and no C toolchain. No new dependencies.
//
// Renderer integration contract (two call sites, owned by gpu_windows.go):
//
//  1. In Update, after w.scene.layout(...):
//     w.accessibilityUpdate()
//     Attaches the provider on the first frame that has a window handle,
//     drains pattern actions queued by UIA clients on the UI thread, and
//     publishes the scene snapshot (scene.accSnapshot) when it changed.
//  2. On the termination path (when Update returns ebiten.Termination):
//     w.accessibilityClose()
//
// Pattern actions run through the same renderer code paths as keyboard
// input, on the UI thread: Invoke presses buttons and cycles choices, Toggle
// flips checkboxes and switches, SetValue commits field text, Select chooses
// tabs and table rows, and SetFocus moves keyboard focus.
//
// Semantics exposed per element: name, control type, enabled state, keyboard
// focus, help text (field errors), live region for status lines, bounding
// rectangle in screen coordinates, and the patterns Invoke (buttons, combo
// cycling, detail copy, menu items), ExpandCollapse (menu buttons), Toggle
// (checkboxes and switches), Value (text fields and read-only choices),
// Selection (tab groups and tables), and SelectionItem (tabs and visible rows).
// UI Automation sees the table's visible rows.

import (
	"fmt"
	"image"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/hajimehoshi/ebiten/v2"
	"golang.org/x/sys/windows"
)

// ---- UI Automation constants (Windows SDK, UIAutomationClient/Core .idl) ----

const (
	accWMGetObject = 0x003D
	accRootObject  = 0xFFFFFFE7 // UiaRootObjectId, (DWORD)-25

	accHROk          = 0
	accHRNoInterface = 0x80004002
	accHRElementGone = 0x80040201 // UIA_E_ELEMENTNOTAVAILABLE
	accHRFalse       = 1

	accProviderServerSide = 2 // ProviderOptions_ServerSideProvider

	accPatInvoke        = 10000
	accPatSelection     = 10001
	accPatValue         = 10002
	accPatExpand        = 10005
	accPatSelectionItem = 10010
	accPatToggle        = 10015

	accPropRuntimeId          = 30000
	accPropRect               = 30001
	accPropControlType        = 30003
	accPropLocalType          = 30004
	accPropName               = 30005
	accPropHasFocus           = 30008
	accPropFocusable          = 30009
	accPropEnabled            = 30010
	accPropHelp               = 30013
	accPropIsControl          = 30016
	accPropIsContent          = 30017
	accPropIsPassword         = 30019
	accPropValue              = 30045
	accPropReadOnly           = 30046
	accPropSelection          = 30059
	accPropCanMulti           = 30060
	accPropIsRequired         = 30061
	accPropExpanded           = 30070
	accPropIsSelected         = 30079
	accPropSelectionContainer = 30080
	accPropToggleState        = 30086
	accPropAriaRole           = 30101
	accPropLive               = 30135

	accEventStructureChanged = 20002
	accEventFocusChanged     = 20005
	accEventItemSelected     = 20012
	accEventLiveChanged      = 20024
	accEventItemRemoved      = 20011

	accCtlButton   = 50000
	accCtlCheckBox = 50002
	accCtlCombo    = 50003
	accCtlEdit     = 50004
	accCtlListItem = 50007
	accCtlList     = 50008
	accCtlMenu     = 50009
	accCtlMenuItem = 50011
	accCtlTab      = 50018
	accCtlTabItem  = 50019
	accCtlText     = 50020
	accCtlThumb    = 50027
	accCtlPane     = 50033

	accNavParent = 0
	accNavNext   = 1
	accNavPrev   = 2
	accNavFirst  = 3
	accNavLast   = 4

	accVtEmpty        = 0
	accVtI4           = 3
	accVtR8           = 5
	accVtBool         = 11
	accVtUnknown      = 13
	accVtBSTR         = 8
	accVtArrayR8      = 5 | 0x2000
	accVtArrayI4      = 3 | 0x2000
	accVtArrayUnknown = accVtUnknown | 0x2000
)

var (
	accIIDUnknown = windows.GUID{Data1: 0x00000000, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	accIIDSimple  = windows.GUID{Data1: 0xD6DD68D1, Data2: 0x86FD, Data3: 0x4332, Data4: [8]byte{0x86, 0x66, 0x9A, 0xBE, 0xDE, 0xA2, 0xD2, 0x4C}}
	accIIDFrag    = windows.GUID{Data1: 0xF7063DA8, Data2: 0x8359, Data3: 0x439C, Data4: [8]byte{0x92, 0x97, 0xBB, 0xC5, 0x29, 0x9A, 0x7D, 0x87}}
	accIIDFragRt  = windows.GUID{Data1: 0x620CE2A5, Data2: 0xAB8F, Data3: 0x40A9, Data4: [8]byte{0x86, 0xCB, 0xDE, 0x3C, 0x75, 0x59, 0x9B, 0x58}}
	accIIDInvoke  = windows.GUID{Data1: 0x54FCB24B, Data2: 0xE18E, Data3: 0x47A2, Data4: [8]byte{0xB4, 0xD3, 0xEC, 0xCB, 0xE7, 0x75, 0x99, 0xA2}}
	accIIDExpand  = windows.GUID{Data1: 0xD847D3A5, Data2: 0xCAB0, Data3: 0x4A98, Data4: [8]byte{0x8C, 0x32, 0xEC, 0xB4, 0x5C, 0x59, 0xAD, 0x24}}
	accIIDSelect  = windows.GUID{Data1: 0xFB8B03AF, Data2: 0x3BDF, Data3: 0x48D4, Data4: [8]byte{0xBD, 0x36, 0x1A, 0x65, 0x79, 0x3B, 0xE1, 0x68}}
	accIIDSelItem = windows.GUID{Data1: 0x2ACAD808, Data2: 0xB2D4, Data3: 0x452D, Data4: [8]byte{0xA4, 0x07, 0x91, 0xFF, 0x1A, 0xD1, 0x67, 0xB2}}
	accIIDToggle  = windows.GUID{Data1: 0x56D00BD0, Data2: 0xC4F4, Data3: 0x433C, Data4: [8]byte{0xA8, 0x36, 0x1A, 0x52, 0xA5, 0x7E, 0x08, 0x92}}
	accIIDValue   = windows.GUID{Data1: 0xC7935180, Data2: 0x6FB3, Data3: 0x4201, Data4: [8]byte{0xB1, 0x74, 0x7D, 0xF7, 0x3A, 0xDB, 0xF6, 0x4A}}
)

// ---- DLL entry points ----

var (
	accUiaCore           = windows.NewLazySystemDLL("uiautomationcore.dll")
	accUiaReturnProvider = accUiaCore.NewProc("UiaReturnRawElementProvider")
	accUiaHostProvider   = accUiaCore.NewProc("UiaHostProviderFromHwnd")
	accUiaRaiseEvent     = accUiaCore.NewProc("UiaRaiseAutomationEvent")
	accUiaRaiseProp      = accUiaCore.NewProc("UiaRaiseAutomationPropertyChangedEvent")
	accUiaRaiseStructure = accUiaCore.NewProc("UiaRaiseStructureChangedEvent")
	accUiaDisconnect     = accUiaCore.NewProc("UiaDisconnectProvider")

	accOle32        = windows.NewLazySystemDLL("ole32.dll")
	accCoInitialize = accOle32.NewProc("CoInitializeEx")
	accCoUninit     = accOle32.NewProc("CoUninitialize")

	accOleaut            = windows.NewLazySystemDLL("oleaut32.dll")
	accSysAllocString    = accOleaut.NewProc("SysAllocStringLen")
	accSysFreeString     = accOleaut.NewProc("SysFreeString")
	accSafeArrayCreate   = accOleaut.NewProc("SafeArrayCreateVector")
	accSafeArrayAccess   = accOleaut.NewProc("SafeArrayAccessData")
	accSafeArrayUnaccess = accOleaut.NewProc("SafeArrayUnaccessData")
	accSafeArrayDestroy  = accOleaut.NewProc("SafeArrayDestroy")

	accCallWindowProc  = user32.NewProc("CallWindowProcW")
	accSetWindowLong   = user32.NewProc("SetWindowLongPtrW")
	accGetClassName    = user32.NewProc("GetClassNameW")
	accIsWindowVisible = user32.NewProc("IsWindowVisible")
	accEnumWindows     = user32.NewProc("EnumWindows")
	accClientToScreen  = user32.NewProc("ClientToScreen")
)

// ---- VARIANT and BSTR helpers ----

// accVariant matches the Win32 VARIANT layout (24 bytes): a VARTYPE, three
// reserved words, and an 8-byte payload plus the DECIMAL continuation.
type accVariant struct {
	VT  uint16
	R1  uint16
	R2  uint16
	R3  uint16
	Val uint64
	Ext uint64
}

func accVarBool(v bool) accVariant {
	var n uint16
	if v {
		n = 0xFFFF
	}
	return accVariant{VT: accVtBool, Val: uint64(n)}
}

func accVarI4(v int32) accVariant { return accVariant{VT: accVtI4, Val: uint64(uint32(v))} }

func accBSTRAlloc(s string) uintptr {
	u := utf16.Encode([]rune(s))
	var ptr *uint16
	if len(u) > 0 {
		ptr = &u[0]
	}
	bstr, _, _ := accSysAllocString.Call(uintptr(unsafe.Pointer(ptr)), uintptr(len(u)))
	runtime.KeepAlive(u)
	return bstr
}

func accVarBSTR(s string) accVariant {
	return accVariant{VT: accVtBSTR, Val: uint64(accBSTRAlloc(s))}
}

func accBSTRFree(b uintptr) {
	if b != 0 {
		accSysFreeString.Call(b)
	}
}

func accBSTRString(p unsafe.Pointer) string {
	if p == nil {
		return ""
	}
	w := (*[1 << 20]uint16)(p)
	n := 0
	for n < len(w) && w[n] != 0 {
		n++
	}
	return string(utf16.Decode(w[:n]))
}

func accSafeArrayI4(values []int32) uintptr {
	return accMakeSafeArray(accVtI4, len(values),
		func(i int) unsafe.Pointer { return unsafe.Pointer(&values[i]) })
}

func accSafeArrayR8(values []float64) uintptr {
	return accMakeSafeArray(accVtR8, len(values),
		func(i int) unsafe.Pointer { return unsafe.Pointer(&values[i]) })
}

// accMakeSafeArray builds a flat SAFEARRAY with the given element values.
// It writes through SafeArrayAccessData rather than SafeArrayPutElement,
// which computes element addresses through a path that faults under some
// oleaut32 builds when called from Go callbacks.
func accMakeSafeArray(vt uint16, n int, elem func(int) unsafe.Pointer) uintptr {
	sa, _, _ := accSafeArrayCreate.Call(uintptr(vt), 0, uintptr(n))
	if sa == 0 {
		return 0
	}
	var data unsafe.Pointer
	if r, _, _ := accSafeArrayAccess.Call(sa, uintptr(unsafe.Pointer(&data))); r != 0 || data == nil {
		accSafeArrayDestroy.Call(sa)
		return 0
	}
	size := uintptr(4)
	if vt == accVtR8 {
		size = 8
	} else if vt == accVtUnknown {
		size = unsafe.Sizeof(uintptr(0))
	}
	for i := range n {
		dst := unsafe.Pointer(uintptr(data) + uintptr(i)*size)
		src := elem(i)
		switch vt {
		case accVtUnknown:
			*(*uintptr)(dst) = *(*uintptr)(src)
		case accVtR8:
			*(*float64)(dst) = *(*float64)(src)
		default:
			*(*int32)(dst) = *(*int32)(src)
		}
	}
	accSafeArrayUnaccess.Call(sa)
	return sa
}

// ---- COM plumbing ----
//
// Each COM interface we expose is an accIface value whose first word is a
// pointer to a package-level vtable. The syscall callbacks receive &iface.VT
// as the COM self pointer and recover the owning object through the fixed
// accIface layout. The provider holds the current tree and the reference
// registry holds any objects COM still owns after they leave the tree.

type accIface struct {
	vt *accVTable
	el *accElementObj
}

type accRootIface struct {
	vt   *accVTable
	root *accRootObj
}

type accVTable struct{ fns [12]uintptr }

var accReferenced sync.Map

type accRefCount struct {
	mu    sync.Mutex
	count uintptr
}

func (r *accRefCount) add(obj any) uintptr {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.count == 0 {
		accReferenced.Store(obj, struct{}{})
	}
	r.count++
	return r.count
}

func (r *accRefCount) release(obj any) uintptr {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.count > 0 {
		r.count--
		if r.count == 0 {
			accReferenced.Delete(obj)
		}
	}
	return r.count
}

func accStorePtr(ppv unsafe.Pointer, v uintptr) {
	*(*uintptr)(ppv) = v
}

func accSameIID(p unsafe.Pointer, g windows.GUID) bool {
	return *(*windows.GUID)(p) == g
}

// accElementObj is the COM element for one snapshot entry.
type accElementObj struct {
	refs     accRefCount
	provider *accProvider
	id       uint32
	role     accRole
	idx      int
	el       accElement
	phys     [4]float64 // left, top, width, height in screen pixels
	dead     bool

	simple        accIface
	fragment      accIface
	invoke        accIface
	expand        accIface
	toggle        accIface
	value         accIface
	selection     accIface
	selectionItem accIface
}

func (o *accElementObj) init(p *accProvider) {
	o.provider = p
	o.simple = accIface{vt: &accVTSimple, el: o}
	o.fragment = accIface{vt: &accVTFrag, el: o}
	o.invoke = accIface{vt: &accVTInvoke, el: o}
	o.expand = accIface{vt: &accVTExpand, el: o}
	o.toggle = accIface{vt: &accVTToggle, el: o}
	o.value = accIface{vt: &accVTValue, el: o}
	o.selection = accIface{vt: &accVTSelection, el: o}
	o.selectionItem = accIface{vt: &accVTSelItem, el: o}
}

// patterns reports which pattern interfaces apply to the element's role.
func (o *accElementObj) patterns() (invoke, toggle, value, selection, selItem bool) {
	switch o.role {
	case accButtonRole, accMenuButtonRole, accMenuItemRole:
		invoke = true
	case accToggleRole:
		toggle = true
	case accComboRole:
		invoke, value = true, true
	case accInputRole, accAreaRole:
		value = true
	case accListRole, accTabRole:
		selection = true
	case accRowRole, accTabItemRole:
		selItem = true
	}
	return
}

// accRootObj is the fragment root: the window's own element.
type accRootObj struct {
	refs     accRefCount
	provider *accProvider
	simple   accRootIface
	fragment accRootIface
	root     accRootIface
}

func (r *accRootObj) init(p *accProvider) {
	r.provider = p
	r.simple = accRootIface{vt: &accVTSimpleRoot, root: r}
	r.fragment = accRootIface{vt: &accVTFragRoot, root: r}
	r.root = accRootIface{vt: &accVTFragRootRoot, root: r}
}

func accRootQueryInterface(r *accRootObj, riid, ppv unsafe.Pointer) uintptr {
	accStorePtr(ppv, 0)
	switch {
	case accSameIID(riid, accIIDUnknown), accSameIID(riid, accIIDSimple):
		accStorePtr(ppv, uintptr(unsafe.Pointer(&r.simple)))
	case accSameIID(riid, accIIDFrag):
		accStorePtr(ppv, uintptr(unsafe.Pointer(&r.fragment)))
	case accSameIID(riid, accIIDFragRt):
		accStorePtr(ppv, uintptr(unsafe.Pointer(&r.root)))
	default:
		return accHRNoInterface
	}
	r.refs.add(r)
	return accHROk
}

func accElementQueryInterface(o *accElementObj, riid, ppv unsafe.Pointer) uintptr {
	accStorePtr(ppv, 0)
	invoke, toggle, value, selection, selItem := o.patterns()
	switch {
	case accSameIID(riid, accIIDUnknown), accSameIID(riid, accIIDSimple):
		accStorePtr(ppv, uintptr(unsafe.Pointer(&o.simple)))
	case accSameIID(riid, accIIDFrag):
		accStorePtr(ppv, uintptr(unsafe.Pointer(&o.fragment)))
	case accSameIID(riid, accIIDInvoke) && invoke:
		accStorePtr(ppv, uintptr(unsafe.Pointer(&o.invoke)))
	case accSameIID(riid, accIIDExpand) && o.role == accMenuButtonRole:
		accStorePtr(ppv, uintptr(unsafe.Pointer(&o.expand)))
	case accSameIID(riid, accIIDToggle) && toggle:
		accStorePtr(ppv, uintptr(unsafe.Pointer(&o.toggle)))
	case accSameIID(riid, accIIDValue) && value:
		accStorePtr(ppv, uintptr(unsafe.Pointer(&o.value)))
	case accSameIID(riid, accIIDSelect) && selection:
		accStorePtr(ppv, uintptr(unsafe.Pointer(&o.selection)))
	case accSameIID(riid, accIIDSelItem) && selItem:
		accStorePtr(ppv, uintptr(unsafe.Pointer(&o.selectionItem)))
	default:
		return accHRNoInterface
	}
	o.refs.add(o)
	return accHROk
}

// ---- provider ----

type accPending struct {
	verb string // "invoke", "toggle", "settext", "select", "focus"
	key  string
	row  int
	text string
}

type accActions struct {
	Wake      func()
	Invoke    func(key string)
	MenuItem  func(key string, index int)
	MenuFocus func(key string, index int)
	SetMenu   func(key string, expanded bool)
	Toggle    func(key string)
	SetText   func(key, value string)
	Select    func(key string, row int)
	SetFocus  func(key string)
}

type accProvider struct {
	hwnd     uintptr
	origProc uintptr
	seed     int32
	ownsCOM  bool
	actions  accActions

	mu     sync.Mutex
	snap   accSnapshot
	objs   map[accKey]*accElementObj
	nextID uint32
	root   *accRootObj
	// phys is the logical-to-screen transform captured at publish time.
	originX, originY int
	scale            float64
	closed           bool
	queue            []accPending
}

var accByHwnd sync.Map // uintptr -> *accProvider

// accAttachProvider subclasses the window to answer WM_GETOBJECT and returns
// the provider. It must run on the thread that owns the window.
func accAttachProvider(hwnd uintptr, actions accActions) (*accProvider, error) {
	if runtime.GOARCH != "amd64" {
		return nil, fmt.Errorf("desktop: UI Automation requires Windows amd64")
	}
	for _, proc := range []*windows.LazyProc{
		accUiaReturnProvider, accUiaHostProvider, accUiaRaiseEvent,
		accUiaRaiseProp, accUiaRaiseStructure, accUiaDisconnect,
	} {
		if err := proc.Find(); err != nil {
			return nil, fmt.Errorf("desktop: UI Automation is unavailable: %w", err)
		}
	}
	if old, loaded := accByHwnd.Load(hwnd); loaded {
		return old.(*accProvider), nil
	}
	// The window thread must live in a COM apartment for UIA to marshal the
	// provider to its worker threads. APARTMENTTHREADED matches a window
	// thread; RPC_E_CHANGED_MODE means another subsystem picked a mode
	// already, which also works.
	hr, _, _ := accCoInitialize.Call(0, 2)
	ownsCOM := hr == 0 || hr == 1             // S_OK or S_FALSE
	if !ownsCOM && uint32(hr) != 0x80010106 { // RPC_E_CHANGED_MODE
		return nil, fmt.Errorf("desktop: initialize UI Automation COM apartment: %#x", hr)
	}
	p := &accProvider{hwnd: hwnd, actions: actions, ownsCOM: ownsCOM, objs: map[accKey]*accElementObj{}}
	p.seed = int32(uint32(hwnd) ^ uint32(uintptr(unsafe.Pointer(p))))
	p.root = &accRootObj{}
	p.root.init(p)
	var err error
	p.origProc, _, err = accSetWindowLong.Call(hwnd, ^uintptr(3), syscall.NewCallback(accSubclassProc))
	if p.origProc == 0 {
		if p.ownsCOM {
			accCoUninit.Call()
		}
		return nil, fmt.Errorf("desktop: attach UI Automation provider: %w", err)
	}
	accByHwnd.Store(hwnd, p)
	return p, nil
}

func accSubclassProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	v, ok := accByHwnd.Load(hwnd)
	if !ok {
		r, _, _ := defWindowProc.Call(hwnd, msg, wparam, lparam)
		return r
	}
	p := v.(*accProvider)
	// Compare as DWORD: senders may sign-extend UiaRootObjectId (-25).
	if msg == accWMGetObject && uint32(lparam) == accRootObject {
		p.mu.Lock()
		root, closed := p.root, p.closed
		p.mu.Unlock()
		if !closed {
			r, _, _ := accUiaReturnProvider.Call(hwnd, wparam, lparam,
				uintptr(unsafe.Pointer(&root.simple)))
			if r != 0 {
				return r
			}
		}
	}
	p.mu.Lock()
	orig := p.origProc
	p.mu.Unlock()
	if orig == 0 {
		r, _, _ := defWindowProc.Call(hwnd, msg, wparam, lparam)
		return r
	}
	r, _, _ := accCallWindowProc.Call(orig, hwnd, msg, wparam, lparam)
	return r
}

// Close disconnects the provider from UI Automation and restores the window
// procedure. It must run on the thread that owns the window.
func (p *accProvider) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	root := p.root
	if p.origProc != 0 {
		accSetWindowLong.Call(p.hwnd, ^uintptr(3), p.origProc)
		p.origProc = 0
	}
	for _, obj := range p.objs {
		obj.dead = true
	}
	objs := p.objs
	p.objs = nil
	p.mu.Unlock()
	accUiaDisconnect.Call(uintptr(unsafe.Pointer(&root.simple)))
	for _, obj := range objs {
		accUiaDisconnect.Call(uintptr(unsafe.Pointer(&obj.simple)))
	}
	accByHwnd.Delete(p.hwnd)
	if p.ownsCOM {
		accCoUninit.Call()
	}
}

// enqueue records a pattern action from a UI Automation thread.
func (p *accProvider) enqueue(item accPending) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.queue = append(p.queue, item)
	p.mu.Unlock()
	if p.actions.Wake != nil {
		p.actions.Wake()
	}
}

// Drain runs queued pattern actions with fn on the calling (UI) thread.
func (p *accProvider) Drain() {
	p.mu.Lock()
	items := p.queue
	p.queue = nil
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return
	}
	for _, it := range items {
		switch it.verb {
		case "invoke":
			if p.actions.Invoke != nil {
				p.actions.Invoke(it.key)
			}
		case "menu-item":
			if p.actions.MenuItem != nil {
				p.actions.MenuItem(it.key, it.row)
			}
		case "menu-focus":
			if p.actions.MenuFocus != nil {
				p.actions.MenuFocus(it.key, it.row)
			}
		case "expand", "collapse":
			if p.actions.SetMenu != nil {
				p.actions.SetMenu(it.key, it.verb == "expand")
			}
		case "toggle":
			if p.actions.Toggle != nil {
				p.actions.Toggle(it.key)
			}
		case "settext":
			if p.actions.SetText != nil {
				p.actions.SetText(it.key, it.text)
			}
		case "select":
			if p.actions.Select != nil {
				p.actions.Select(it.key, it.row)
			}
		case "focus":
			if p.actions.SetFocus != nil {
				p.actions.SetFocus(it.key)
			}
		}
	}
}

// Publish installs a new snapshot and raises UI Automation events for the
// changes. Call it on the thread that owns the window, after layout.
func (p *accProvider) Publish(snap accSnapshot) {
	type change struct {
		obj     *accElementObj
		kind    string
		old, ne accElement
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	prev := p.snap
	changedStructure := snap.Structure != prev.Structure
	focusMoved := accFocusKey(&snap) != accFocusKey(&prev)
	changes := []change{}
	newObjs := make([]*accElementObj, 0, len(snap.Elements))
	next := make(map[accKey]*accElementObj, len(snap.Elements))
	p.updateTransformLocked(snap)
	for i := range snap.Elements {
		e := &snap.Elements[i]
		key := e.identity()
		obj := p.objs[key]
		var old accElement
		reused := obj != nil && obj.role == e.Role
		if reused {
			old = obj.el
		}
		if obj == nil || obj.role != e.Role {
			p.nextID++
			obj = &accElementObj{id: p.nextID, role: e.Role}
			obj.init(p)
		}
		next[key] = obj
		obj.el, obj.idx = *e, i
		obj.phys = p.physRect(e.Box)
		newObjs = append(newObjs, obj)
		if reused {
			if old.Checked != e.Checked {
				changes = append(changes, change{obj, "toggle", old, *e})
			}
			if old.Expanded != e.Expanded {
				changes = append(changes, change{obj, "expand", old, *e})
			}
			if old.Enabled != e.Enabled {
				changes = append(changes, change{obj, "enabled", old, *e})
			}
			if e.Role == accStatusRole && old.Name != e.Name {
				changes = append(changes, change{obj, "live", old, *e})
			}
			if old.Value != e.Value {
				changes = append(changes, change{obj, "value", old, *e})
			}
			if e.Selected && !old.Selected {
				changes = append(changes, change{obj, "selected", old, *e})
			}
			if old.Selected && !e.Selected {
				changes = append(changes, change{obj, "removed", old, *e})
			}
		}
	}
	for key, obj := range p.objs {
		if next[key] != obj {
			obj.dead = true
		}
	}
	p.snap, p.objs = snap, next
	root := p.root
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return
	}
	if changedStructure {
		rid := []int32{3, p.seed}
		accUiaRaiseStructure.Call(uintptr(unsafe.Pointer(&root.simple)), 2,
			uintptr(unsafe.Pointer(&rid[0])), uintptr(len(rid)))
	}
	if focusMoved && snap.Focus >= 0 && snap.Focus < len(newObjs) {
		accUiaRaiseEvent.Call(uintptr(unsafe.Pointer(&newObjs[snap.Focus].simple)), accEventFocusChanged)
	}
	for _, c := range changes {
		switch c.kind {
		case "toggle":
			oldV, newV := accVarBool(c.old.Checked), accVarBool(c.ne.Checked)
			accUiaRaiseProp.Call(uintptr(unsafe.Pointer(&c.obj.simple)), accPropToggleState,
				uintptr(unsafe.Pointer(&oldV)), uintptr(unsafe.Pointer(&newV)))
		case "expand":
			oldV, newV := accVarI4(accExpandState(c.old.Expanded)), accVarI4(accExpandState(c.ne.Expanded))
			accUiaRaiseProp.Call(uintptr(unsafe.Pointer(&c.obj.simple)), accPropExpanded,
				uintptr(unsafe.Pointer(&oldV)), uintptr(unsafe.Pointer(&newV)))
		case "enabled":
			oldV, newV := accVarBool(c.old.Enabled), accVarBool(c.ne.Enabled)
			accUiaRaiseProp.Call(uintptr(unsafe.Pointer(&c.obj.simple)), accPropEnabled,
				uintptr(unsafe.Pointer(&oldV)), uintptr(unsafe.Pointer(&newV)))
		case "live":
			accUiaRaiseEvent.Call(uintptr(unsafe.Pointer(&c.obj.simple)), accEventLiveChanged)
		case "value":
			oldV, newV := accVarBSTR(c.old.Value), accVarBSTR(c.ne.Value)
			accUiaRaiseProp.Call(uintptr(unsafe.Pointer(&c.obj.simple)), accPropValue,
				uintptr(unsafe.Pointer(&oldV)), uintptr(unsafe.Pointer(&newV)))
			accBSTRFree(uintptr(oldV.Val))
			accBSTRFree(uintptr(newV.Val))
		case "selected":
			accUiaRaiseEvent.Call(uintptr(unsafe.Pointer(&c.obj.simple)), accEventItemSelected)
		case "removed":
			accUiaRaiseEvent.Call(uintptr(unsafe.Pointer(&c.obj.simple)), accEventItemRemoved)
		}
	}
}

// updateTransformLocked captures the client-to-screen transform used to
// convert logical bounds to physical screen coordinates.
func (p *accProvider) updateTransformLocked(snap accSnapshot) {
	p.originX, p.originY, p.scale = 0, 0, 1
	var rc rect
	if v, _, _ := getClientRect.Call(p.hwnd, uintptr(unsafe.Pointer(&rc))); v == 0 || rc.Right <= rc.Left {
		return
	}
	pt := point{}
	if v, _, _ := accClientToScreen.Call(p.hwnd, uintptr(unsafe.Pointer(&pt))); v == 0 {
		return
	}
	p.originX, p.originY = int(pt.X), int(pt.Y)
	if snap.Size.X > 0 {
		p.scale = float64(rc.Right-rc.Left) / float64(snap.Size.X)
	}
}

func (p *accProvider) physRect(box image.Rectangle) [4]float64 {
	return [4]float64{
		float64(p.originX) + float64(box.Min.X)*p.scale,
		float64(p.originY) + float64(box.Min.Y)*p.scale,
		float64(box.Dx()) * p.scale,
		float64(box.Dy()) * p.scale,
	}
}

// ---- IRawElementProviderSimple callbacks ----

func accCbSimpleQI(self, riid, ppv unsafe.Pointer) uintptr {
	iface := (*accIface)(self)
	return accElementQueryInterface(iface.el, riid, ppv)
}

func accCbSimpleAddRef(self unsafe.Pointer) uintptr {
	iface := (*accIface)(self)
	return iface.el.refs.add(iface.el)
}

func accCbSimpleRelease(self unsafe.Pointer) uintptr {
	iface := (*accIface)(self)
	return iface.el.refs.release(iface.el)
}

func accCbSimplePattern(self unsafe.Pointer, pattern uintptr, ppv unsafe.Pointer) uintptr {
	iface := (*accIface)(self)
	obj := iface.el
	obj.provider.mu.Lock()
	dead := obj.dead
	obj.provider.mu.Unlock()
	accStorePtr(ppv, 0)
	if dead {
		return accHRElementGone
	}
	invoke, toggle, value, selection, selItem := obj.patterns()
	var target *accIface
	switch pattern {
	case accPatInvoke:
		target = &obj.invoke
	case accPatExpand:
		target = &obj.expand
	case accPatToggle:
		target = &obj.toggle
	case accPatValue:
		target = &obj.value
	case accPatSelection:
		target = &obj.selection
	case accPatSelectionItem:
		target = &obj.selectionItem
	}
	if target == nil {
		return accHROk
	}
	switch {
	case invoke && target == &obj.invoke,
		target == &obj.expand && obj.role == accMenuButtonRole,
		toggle && target == &obj.toggle,
		value && target == &obj.value,
		selection && target == &obj.selection,
		selItem && target == &obj.selectionItem:
	default:
		return accHROk
	}
	accStorePtr(ppv, uintptr(unsafe.Pointer(target)))
	obj.refs.add(obj)
	return accHROk
}

func accCbSimpleProp(self unsafe.Pointer, prop uintptr, pvariant unsafe.Pointer) uintptr {
	iface := (*accIface)(self)
	obj := iface.el
	obj.provider.mu.Lock()
	dead := obj.dead
	el, phys := obj.el, obj.phys
	obj.provider.mu.Unlock()
	if dead {
		return accHRElementGone
	}
	out := (*accVariant)(pvariant)
	switch prop {
	case accPropRuntimeId:
		*out = accVariant{VT: accVtArrayI4, Val: uint64(accSafeArrayI4([]int32{3, obj.provider.seed, int32(obj.id)}))}
	case accPropRect:
		*out = accVariant{VT: accVtArrayR8, Val: uint64(accSafeArrayR8(phys[:]))}
	case accPropControlType:
		*out = accVarI4(accControlType(el.Role))
	case accPropLocalType:
		*out = accVarBSTR(accLocalType(el.Role))
	case accPropName:
		*out = accVarBSTR(el.Name)
	case accPropHasFocus:
		*out = accVarBool(el.Focused)
	case accPropFocusable:
		*out = accVarBool(el.Focusable)
	case accPropEnabled:
		*out = accVarBool(el.Enabled)
	case accPropHelp:
		*out = accVarBSTR(el.Help)
	case accPropIsControl:
		*out = accVarBool(true)
	case accPropIsContent:
		*out = accVarBool(el.Role != accSplitterRole)
	case accPropIsPassword:
		*out = accVarBool(false)
	case accPropValue:
		switch el.Role {
		case accInputRole, accAreaRole, accComboRole:
			*out = accVarBSTR(el.Value)
		}
	case accPropReadOnly:
		switch el.Role {
		case accComboRole:
			*out = accVarBool(true)
		case accInputRole, accAreaRole:
			*out = accVarBool(!el.Enabled)
		}
	case accPropSelection:
		if el.Role.selectionContainer() {
			obj.provider.mu.Lock()
			selection := obj.provider.selectionArrayLocked(el.Key)
			obj.provider.mu.Unlock()
			*out = accVariant{VT: accVtArrayUnknown, Val: uint64(selection)}
		}
	case accPropCanMulti:
		if el.Role.selectionContainer() {
			*out = accVarBool(false)
		}
	case accPropIsRequired:
		if el.Role.selectionContainer() {
			*out = accVarBool(el.Role == accTabRole)
		}
	case accPropExpanded:
		if el.Role == accMenuButtonRole {
			*out = accVarI4(accExpandState(el.Expanded))
		}
	case accPropIsSelected:
		if el.Role.selectionItem() {
			*out = accVarBool(el.Selected)
		}
	case accPropSelectionContainer:
		if el.Role.selectionItem() {
			container := obj.provider.selectionContainer(el.Key)
			if container != 0 {
				*out = accVariant{VT: accVtUnknown, Val: uint64(container)}
			}
		}
	case accPropToggleState:
		if el.Role == accToggleRole {
			state := int32(0)
			if el.Checked {
				state = 1
			}
			*out = accVarI4(state)
		}
	case accPropAriaRole:
		*out = accVarBSTR(accAriaRole(el.Role))
	case accPropLive:
		if el.Role == accStatusRole {
			*out = accVarI4(1) // polite
		}
	default:
		*out = accVariant{}
	}
	return accHROk
}

func accCbSimpleHost(self, ppv unsafe.Pointer) uintptr {
	accStorePtr(ppv, 0)
	return accHROk // only the fragment root delegates to the window host
}

// selectedObjLocked returns the selected tab or visible table row in the
// current snapshot. Call with the provider lock.
func (p *accProvider) selectedObjLocked(tableKey string) *accElementObj {
	for i := range p.snap.Elements {
		e := &p.snap.Elements[i]
		if e.Selected && e.Role.selectionItem() && e.Key == tableKey {
			return p.objs[e.identity()]
		}
	}
	return nil
}

func (p *accProvider) selectionArrayLocked(key string) uintptr {
	sel := p.selectedObjLocked(key)
	if sel == nil {
		array, _, _ := accSafeArrayCreate.Call(accVtUnknown, 0, 0)
		return array
	}
	unknown := uintptr(unsafe.Pointer(&sel.simple))
	array := accMakeSafeArray(accVtUnknown, 1, func(int) unsafe.Pointer {
		return unsafe.Pointer(&unknown)
	})
	if array != 0 {
		sel.refs.add(sel)
	}
	return array
}

func accControlType(r accRole) int32 {
	switch r {
	case accButtonRole, accMenuButtonRole:
		return accCtlButton
	case accMenuRole:
		return accCtlMenu
	case accMenuItemRole:
		return accCtlMenuItem
	case accTabRole:
		return accCtlTab
	case accTabItemRole:
		return accCtlTabItem
	case accToggleRole:
		return accCtlCheckBox
	case accComboRole:
		return accCtlCombo
	case accInputRole, accAreaRole:
		return accCtlEdit
	case accListRole:
		return accCtlList
	case accRowRole:
		return accCtlListItem
	case accSplitterRole:
		return accCtlThumb
	}
	return accCtlText
}

func accLocalType(r accRole) string {
	switch r {
	case accButtonRole, accMenuButtonRole:
		return "button"
	case accMenuRole:
		return "menu"
	case accMenuItemRole:
		return "menu item"
	case accTabRole:
		return "tab control"
	case accTabItemRole:
		return "tab item"
	case accToggleRole:
		return "check box"
	case accComboRole:
		return "combo box"
	case accInputRole, accAreaRole:
		return "edit"
	case accListRole:
		return "list"
	case accRowRole:
		return "item"
	case accSplitterRole:
		return "splitter"
	}
	return "text"
}

func accAriaRole(r accRole) string {
	switch r {
	case accButtonRole, accMenuButtonRole:
		return "button"
	case accMenuRole:
		return "menu"
	case accMenuItemRole:
		return "menuitem"
	case accTabRole:
		return "tablist"
	case accTabItemRole:
		return "tab"
	case accToggleRole:
		return "checkbox"
	case accComboRole:
		return "combobox"
	case accInputRole, accAreaRole:
		return "textbox"
	case accHeadingRole:
		return "heading"
	case accListRole:
		return "list"
	case accRowRole:
		return "listitem"
	case accStatusRole:
		return "status"
	case accSplitterRole:
		return "separator"
	}
	return "text"
}

// ---- IRawElementProviderFragment callbacks ----

func accCbFragNavigate(self unsafe.Pointer, dir uintptr, ppv unsafe.Pointer) uintptr {
	iface := (*accIface)(self)
	obj := iface.el
	p := obj.provider
	p.mu.Lock()
	defer p.mu.Unlock()
	accStorePtr(ppv, 0)
	if p.closed || obj.dead {
		return accHRElementGone
	}
	if int32(dir) == accNavParent && obj.el.Parent < 0 {
		accStorePtr(ppv, uintptr(unsafe.Pointer(&p.root.fragment)))
		p.root.refs.add(p.root)
		return accHROk
	}
	target := p.navigateLocked(obj, int32(dir))
	if target == nil {
		return accHROk
	}
	accStorePtr(ppv, uintptr(unsafe.Pointer(&target.fragment)))
	target.refs.add(target)
	return accHROk
}

// navigateLocked resolves tree navigation; window children have Parent < 0,
// rows and copy buttons point at their owning element's index.
func (p *accProvider) navigateLocked(obj *accElementObj, dir int32) *accElementObj {
	if dir == accNavFirst || dir == accNavLast {
		want := obj.idx // children point at their owning element's index
		for i := range p.snap.Elements {
			j := i
			if dir == accNavLast {
				j = len(p.snap.Elements) - 1 - i
			}
			e := &p.snap.Elements[j]
			if e.Parent == want {
				return p.objs[e.identity()]
			}
		}
		return nil
	}
	switch dir {
	case accNavParent:
		if obj.el.Parent < 0 {
			return nil // the window root is the parent
		}
		parent := &p.snap.Elements[obj.el.Parent]
		return p.objs[parent.identity()]
	case accNavNext, accNavPrev:
		for step := 1; step < len(p.snap.Elements); step++ {
			j := obj.idx + step
			if dir == accNavPrev {
				j = obj.idx - step
			}
			if j < 0 || j >= len(p.snap.Elements) {
				break
			}
			if p.snap.Elements[j].Parent == obj.el.Parent {
				return p.objs[p.snap.Elements[j].identity()]
			}
		}
	}
	return nil
}

func accCbFragRuntimeId(self unsafe.Pointer, ppsa unsafe.Pointer) uintptr {
	iface := (*accIface)(self)
	obj := iface.el
	accStorePtr(ppsa, accSafeArrayI4([]int32{3, obj.provider.seed, int32(obj.id)}))
	return accHROk
}

func accCbFragRect(self unsafe.Pointer, prect unsafe.Pointer) uintptr {
	iface := (*accIface)(self)
	obj := iface.el
	obj.provider.mu.Lock()
	phys := obj.phys
	obj.provider.mu.Unlock()
	r := (*[4]float64)(prect)
	*r = phys
	return accHROk
}

func accCbFragRoots(self unsafe.Pointer, ppsa unsafe.Pointer) uintptr {
	accStorePtr(ppsa, 0)
	return accHROk
}

func accCbFragSetFocus(self unsafe.Pointer) uintptr {
	iface := (*accIface)(self)
	obj := iface.el
	obj.provider.mu.Lock()
	el, dead := obj.el, obj.dead
	obj.provider.mu.Unlock()
	if dead {
		return accHRElementGone
	}
	if el.Focusable {
		verb := "focus"
		switch el.Role {
		case accTabItemRole:
			verb = "select"
		case accMenuItemRole:
			verb = "menu-focus"
		}
		obj.provider.enqueue(accPending{verb: verb, key: el.Key, row: el.Row})
	}
	return accHROk
}

func accCbFragFragmentRoot(self unsafe.Pointer, ppv unsafe.Pointer) uintptr {
	root := (*accIface)(self).el.provider.root
	root.refs.add(root)
	accStorePtr(ppv, uintptr(unsafe.Pointer(&root.root)))
	return accHROk
}

func accCbFragRootFragmentRoot(self unsafe.Pointer, ppv unsafe.Pointer) uintptr {
	iface := (*accRootIface)(self)
	accStorePtr(ppv, uintptr(unsafe.Pointer(&iface.root.root)))
	iface.root.refs.add(iface.root)
	return accHROk
}

// ---- pattern callbacks ----

func accCbInvoke(self unsafe.Pointer) uintptr {
	iface := (*accIface)(self)
	obj := iface.el
	obj.provider.mu.Lock()
	el, dead := obj.el, obj.dead
	obj.provider.mu.Unlock()
	if dead {
		return accHRElementGone
	}
	if el.Enabled {
		verb := "invoke"
		if el.Role == accMenuItemRole {
			verb = "menu-item"
		}
		obj.provider.enqueue(accPending{verb: verb, key: el.Key, row: el.Row})
	}
	return accHROk
}

func accExpandState(expanded bool) int32 {
	if expanded {
		return 1
	}
	return 0
}

func accCbExpand(self unsafe.Pointer) uintptr   { return accQueueExpand(self, "expand") }
func accCbCollapse(self unsafe.Pointer) uintptr { return accQueueExpand(self, "collapse") }

func accQueueExpand(self unsafe.Pointer, verb string) uintptr {
	obj := (*accIface)(self).el
	obj.provider.mu.Lock()
	el, dead := obj.el, obj.dead
	obj.provider.mu.Unlock()
	if dead {
		return accHRElementGone
	}
	if el.Enabled && el.Role == accMenuButtonRole {
		obj.provider.enqueue(accPending{verb: verb, key: el.Key})
	}
	return accHROk
}

func accCbExpanded(self, pval unsafe.Pointer) uintptr {
	obj := (*accIface)(self).el
	obj.provider.mu.Lock()
	expanded, dead := obj.el.Expanded, obj.dead
	obj.provider.mu.Unlock()
	if dead {
		return accHRElementGone
	}
	accStoreI32(pval, accExpandState(expanded))
	return accHROk
}

func accCbToggleToggle(self unsafe.Pointer) uintptr {
	obj := (*accIface)(self).el
	obj.provider.mu.Lock()
	el, dead := obj.el, obj.dead
	obj.provider.mu.Unlock()
	if dead {
		return accHRElementGone
	}
	if el.Enabled {
		obj.provider.enqueue(accPending{verb: "toggle", key: el.Key})
	}
	return accHROk
}

func accCbToggleState(self unsafe.Pointer, pval unsafe.Pointer) uintptr {
	iface := (*accIface)(self)
	obj := iface.el
	obj.provider.mu.Lock()
	checked := obj.el.Checked
	obj.provider.mu.Unlock()
	state := int32(0)
	if checked {
		state = 1
	}
	accStoreI32(pval, state)
	return accHROk
}

func accCbValueSet(self unsafe.Pointer, val unsafe.Pointer) uintptr {
	obj := (*accIface)(self).el
	obj.provider.mu.Lock()
	el, dead := obj.el, obj.dead
	obj.provider.mu.Unlock()
	if dead {
		return accHRElementGone
	}
	if !el.Enabled || el.Role == accComboRole {
		return accHRFalse
	}
	obj.provider.enqueue(accPending{verb: "settext", key: el.Key, text: accBSTRString(val)})
	return accHROk
}

func accCbValueValue(self unsafe.Pointer, pbstr unsafe.Pointer) uintptr {
	iface := (*accIface)(self)
	obj := iface.el
	obj.provider.mu.Lock()
	value := obj.el.Value
	obj.provider.mu.Unlock()
	accStorePtr(pbstr, accBSTRAlloc(value))
	return accHROk
}

func accCbValueReadOnly(self unsafe.Pointer, pval unsafe.Pointer) uintptr {
	iface := (*accIface)(self)
	obj := iface.el
	obj.provider.mu.Lock()
	enabled, role := obj.el.Enabled, obj.el.Role
	obj.provider.mu.Unlock()
	readonly := role == accComboRole || !enabled
	v := int32(0)
	if readonly {
		v = 0xFFFF
	}
	accStoreI32(pval, v)
	return accHROk
}

func accCbSelGetSelection(self unsafe.Pointer, ppsa unsafe.Pointer) uintptr {
	iface := (*accIface)(self)
	obj := iface.el
	p := obj.provider
	p.mu.Lock()
	selection := p.selectionArrayLocked(obj.el.Key)
	p.mu.Unlock()
	accStorePtr(ppsa, selection)
	return accHROk
}

func accCbSelCanMulti(self unsafe.Pointer, pval unsafe.Pointer) uintptr {
	accStoreI32(pval, 0)
	return accHROk
}

func accCbSelIsRequired(self unsafe.Pointer, pval unsafe.Pointer) uintptr {
	obj := (*accIface)(self).el
	obj.provider.mu.Lock()
	role := obj.el.Role
	obj.provider.mu.Unlock()
	value := int32(0)
	if role == accTabRole {
		value = 0xFFFF
	}
	accStoreI32(pval, value)
	return accHROk
}

func accCbSelItemSelect(self unsafe.Pointer) uintptr {
	return accSelItemPick(self)
}

func accCbSelItemAdd(self unsafe.Pointer) uintptr {
	return accSelItemPick(self)
}

func accCbSelItemRemove(self unsafe.Pointer) uintptr {
	return accHROk // single-selection list; removal is not an action
}

func accSelItemPick(self unsafe.Pointer) uintptr {
	iface := (*accIface)(self)
	obj := iface.el
	obj.provider.mu.Lock()
	el, dead := obj.el, obj.dead
	obj.provider.mu.Unlock()
	if dead {
		return accHRElementGone
	}
	if !el.Enabled {
		return accHRFalse
	}
	obj.provider.enqueue(accPending{verb: "select", key: el.Key, row: el.Row})
	return accHROk
}

func accCbSelItemSelected(self unsafe.Pointer, pval unsafe.Pointer) uintptr {
	iface := (*accIface)(self)
	obj := iface.el
	obj.provider.mu.Lock()
	selected := obj.el.Selected
	obj.provider.mu.Unlock()
	v := int32(0)
	if selected {
		v = 0xFFFF
	}
	accStoreI32(pval, v)
	return accHROk
}

func accCbSelItemContainer(self unsafe.Pointer, ppv unsafe.Pointer) uintptr {
	obj := (*accIface)(self).el
	obj.provider.mu.Lock()
	key := obj.el.Key
	obj.provider.mu.Unlock()
	accStorePtr(ppv, obj.provider.selectionContainer(key))
	return accHROk
}

func (p *accProvider) selectionContainer(key string) uintptr {
	p.mu.Lock()
	defer p.mu.Unlock()
	container := p.objs[accID(key, -1)]
	if container == nil {
		return 0
	}
	container.refs.add(container)
	return uintptr(unsafe.Pointer(&container.simple))
}

// ---- root callbacks ----

func accCbRootQI(self, riid, ppv unsafe.Pointer) uintptr {
	iface := (*accRootIface)(self)
	return accRootQueryInterface(iface.root, riid, ppv)
}

func accCbRootAddRef(self unsafe.Pointer) uintptr {
	iface := (*accRootIface)(self)
	return iface.root.refs.add(iface.root)
}

func accCbRootRelease(self unsafe.Pointer) uintptr {
	iface := (*accRootIface)(self)
	return iface.root.refs.release(iface.root)
}

func accCbRootOptions(self unsafe.Pointer, pval unsafe.Pointer) uintptr {
	accStoreI32(pval, accProviderServerSide)
	return accHROk
}

func accCbRootPattern(self unsafe.Pointer, pattern uintptr, ppv unsafe.Pointer) uintptr {
	accStorePtr(ppv, 0)
	return accHROk
}

func accCbRootProp(self unsafe.Pointer, prop uintptr, pvariant unsafe.Pointer) uintptr {
	iface := (*accRootIface)(self)
	p := iface.root.provider
	p.mu.Lock()
	title, focus, closed := p.snap.Title, p.snap.Focus, p.closed
	seed := p.seed
	p.mu.Unlock()
	out := (*accVariant)(pvariant)
	switch prop {
	case accPropRuntimeId:
		*out = accVariant{VT: accVtArrayI4, Val: uint64(accSafeArrayI4([]int32{3, seed}))}
	case accPropName:
		*out = accVarBSTR(title)
	case accPropControlType:
		*out = accVarI4(accCtlPane)
	case accPropLocalType:
		*out = accVarBSTR("pane")
	case accPropEnabled:
		*out = accVarBool(true)
	case accPropFocusable:
		*out = accVarBool(true)
	case accPropHasFocus:
		*out = accVarBool(focus < 0)
	case accPropIsControl, accPropIsContent:
		*out = accVarBool(true)
	default:
		*out = accVariant{}
	}
	if closed {
		return accHRElementGone
	}
	return accHROk
}

func accCbRootHost(self unsafe.Pointer, ppv unsafe.Pointer) uintptr {
	iface := (*accRootIface)(self)
	r, _, _ := accUiaHostProvider.Call(iface.root.provider.hwnd, uintptr(ppv))
	return r
}

func accCbRootNavigate(self unsafe.Pointer, dir uintptr, ppv unsafe.Pointer) uintptr {
	iface := (*accRootIface)(self)
	p := iface.root.provider
	p.mu.Lock()
	defer p.mu.Unlock()
	accStorePtr(ppv, 0)
	if p.closed {
		return accHRElementGone
	}
	if dir != accNavFirst && dir != accNavLast {
		return accHROk // root has no parent or siblings
	}
	for i := range p.snap.Elements {
		j := i
		if dir == accNavLast {
			j = len(p.snap.Elements) - 1 - i
		}
		e := &p.snap.Elements[j]
		if e.Parent < 0 {
			target := p.objs[e.identity()]
			accStorePtr(ppv, uintptr(unsafe.Pointer(&target.fragment)))
			target.refs.add(target)
			return accHROk
		}
	}
	return accHROk
}

func accCbRootRuntimeId(self unsafe.Pointer, ppsa unsafe.Pointer) uintptr {
	iface := (*accRootIface)(self)
	accStorePtr(ppsa, accSafeArrayI4([]int32{3, iface.root.provider.seed}))
	return accHROk
}

func accCbRootRect(self unsafe.Pointer, prect unsafe.Pointer) uintptr {
	iface := (*accRootIface)(self)
	p := iface.root.provider
	var rc rect
	if v, _, _ := getClientRect.Call(p.hwnd, uintptr(unsafe.Pointer(&rc))); v != 0 {
		pt := point{}
		if ok, _, _ := accClientToScreen.Call(p.hwnd, uintptr(unsafe.Pointer(&pt))); ok != 0 {
			r := (*[4]float64)(prect)
			r[0], r[1] = float64(pt.X), float64(pt.Y)
			r[2], r[3] = float64(rc.Right-rc.Left), float64(rc.Bottom-rc.Top)
		}
	}
	return accHROk
}

func accCbRootRoots(self unsafe.Pointer, ppsa unsafe.Pointer) uintptr {
	accStorePtr(ppsa, 0)
	return accHROk
}

func accCbRootSetFocus(self unsafe.Pointer) uintptr { return accHROk }

// accCbRootFromPoint cannot honor the point: the callback ABI cannot carry
// the double arguments (syscall callbacks reject float arguments), so any
// point resolves to the window root. Clients fall back to tree navigation.
func accCbRootFromPoint(self unsafe.Pointer, x, y uintptr, ppv unsafe.Pointer) uintptr {
	iface := (*accRootIface)(self)
	accStorePtr(ppv, uintptr(unsafe.Pointer(&iface.root.fragment)))
	iface.root.refs.add(iface.root)
	return accHROk
}

func accCbRootGetFocus(self unsafe.Pointer, ppv unsafe.Pointer) uintptr {
	iface := (*accRootIface)(self)
	p := iface.root.provider
	p.mu.Lock()
	defer p.mu.Unlock()
	accStorePtr(ppv, 0)
	if p.closed || p.snap.Focus < 0 || p.snap.Focus >= len(p.snap.Elements) {
		return accHROk
	}
	e := &p.snap.Elements[p.snap.Focus]
	obj := p.objs[e.identity()]
	if obj == nil {
		return accHROk
	}
	accStorePtr(ppv, uintptr(unsafe.Pointer(&obj.fragment)))
	obj.refs.add(obj)
	return accHROk
}

// ---- vtables ----

var (
	accVTSimple = accVTable{[12]uintptr{
		syscall.NewCallback(accCbSimpleQI),
		syscall.NewCallback(accCbSimpleAddRef),
		syscall.NewCallback(accCbSimpleRelease),
		syscall.NewCallback(func(self, pval unsafe.Pointer) uintptr {
			accStoreI32(pval, accProviderServerSide)
			return accHROk
		}),
		syscall.NewCallback(accCbSimplePattern),
		syscall.NewCallback(accCbSimpleProp),
		syscall.NewCallback(accCbSimpleHost),
	}}
	accVTFrag = accVTable{[12]uintptr{
		syscall.NewCallback(accCbSimpleQI),
		syscall.NewCallback(accCbSimpleAddRef),
		syscall.NewCallback(accCbSimpleRelease),
		syscall.NewCallback(accCbFragNavigate),
		syscall.NewCallback(accCbFragRuntimeId),
		syscall.NewCallback(accCbFragRect),
		syscall.NewCallback(accCbFragRoots),
		syscall.NewCallback(accCbFragSetFocus),
		syscall.NewCallback(accCbFragFragmentRoot),
	}}
	accVTInvoke = accVTable{[12]uintptr{
		syscall.NewCallback(accCbSimpleQI),
		syscall.NewCallback(accCbSimpleAddRef),
		syscall.NewCallback(accCbSimpleRelease),
		syscall.NewCallback(accCbInvoke),
	}}
	accVTExpand = accVTable{[12]uintptr{
		syscall.NewCallback(accCbSimpleQI),
		syscall.NewCallback(accCbSimpleAddRef),
		syscall.NewCallback(accCbSimpleRelease),
		syscall.NewCallback(accCbExpand),
		syscall.NewCallback(accCbCollapse),
		syscall.NewCallback(accCbExpanded),
	}}
	accVTToggle = accVTable{[12]uintptr{
		syscall.NewCallback(accCbSimpleQI),
		syscall.NewCallback(accCbSimpleAddRef),
		syscall.NewCallback(accCbSimpleRelease),
		syscall.NewCallback(accCbToggleToggle),
		syscall.NewCallback(accCbToggleState),
	}}
	accVTValue = accVTable{[12]uintptr{
		syscall.NewCallback(accCbSimpleQI),
		syscall.NewCallback(accCbSimpleAddRef),
		syscall.NewCallback(accCbSimpleRelease),
		syscall.NewCallback(accCbValueSet),
		syscall.NewCallback(accCbValueValue),
		syscall.NewCallback(accCbValueReadOnly),
	}}
	accVTSelection = accVTable{[12]uintptr{
		syscall.NewCallback(accCbSimpleQI),
		syscall.NewCallback(accCbSimpleAddRef),
		syscall.NewCallback(accCbSimpleRelease),
		syscall.NewCallback(accCbSelGetSelection),
		syscall.NewCallback(accCbSelCanMulti),
		syscall.NewCallback(accCbSelIsRequired),
	}}
	accVTSelItem = accVTable{[12]uintptr{
		syscall.NewCallback(accCbSimpleQI),
		syscall.NewCallback(accCbSimpleAddRef),
		syscall.NewCallback(accCbSimpleRelease),
		syscall.NewCallback(accCbSelItemSelect),
		syscall.NewCallback(accCbSelItemAdd),
		syscall.NewCallback(accCbSelItemRemove),
		syscall.NewCallback(accCbSelItemSelected),
		syscall.NewCallback(accCbSelItemContainer),
	}}
	accVTSimpleRoot = accVTable{[12]uintptr{
		syscall.NewCallback(accCbRootQI),
		syscall.NewCallback(accCbRootAddRef),
		syscall.NewCallback(accCbRootRelease),
		syscall.NewCallback(accCbRootOptions),
		syscall.NewCallback(accCbRootPattern),
		syscall.NewCallback(accCbRootProp),
		syscall.NewCallback(accCbRootHost),
	}}
	accVTFragRoot = accVTable{[12]uintptr{
		syscall.NewCallback(accCbRootQI),
		syscall.NewCallback(accCbRootAddRef),
		syscall.NewCallback(accCbRootRelease),
		syscall.NewCallback(accCbRootNavigate),
		syscall.NewCallback(accCbRootRuntimeId),
		syscall.NewCallback(accCbRootRect),
		syscall.NewCallback(accCbRootRoots),
		syscall.NewCallback(accCbRootSetFocus),
		syscall.NewCallback(accCbFragRootFragmentRoot),
	}}
	accVTFragRootRoot = accVTable{[12]uintptr{
		syscall.NewCallback(accCbRootQI),
		syscall.NewCallback(accCbRootAddRef),
		syscall.NewCallback(accCbRootRelease),
		syscall.NewCallback(accCbRootFromPoint),
		syscall.NewCallback(accCbRootGetFocus),
	}}
)

func accStoreI32(p unsafe.Pointer, v int32) {
	*(*int32)(p) = v
}

// ---- ebiten window discovery ----

// accFindAppWindow locates the GPU renderer's window: a visible window of
// this process with the engine's window class.
func accFindAppWindow() uintptr {
	found := uintptr(0)
	accEnumWindows.Call(uintptr(accEnumCallback), uintptr(unsafe.Pointer(&found)))
	return found
}

var accEnumCallback = syscall.NewCallback(func(hwnd uintptr, lparam unsafe.Pointer) uintptr {
	target := (*uintptr)(lparam)
	var buf [16]uint16
	n, _, _ := accGetClassName.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if windows.UTF16ToString(buf[:n]) != "GLFW30" {
		return 1
	}
	var pid uint32
	getWindowPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if int(pid) != os.Getpid() {
		return 1
	}
	if v, _, _ := accIsWindowVisible.Call(hwnd); v == 0 {
		return 1
	}
	*target = hwnd
	return 0
})

// ---- gpuWindow integration ----

// windowAccessibility is the per-window state for accessibilityUpdate.
type windowAccessibility struct {
	mu       sync.Mutex
	provider *accProvider
	attempts int
}

var accWindows sync.Map // *gpuWindow -> *windowAccessibility

// accessibilityUpdate attaches the UIA provider on the first frames that can
// find the window, drains pattern actions on the UI thread, and publishes the
// scene snapshot. Call it from Update, after w.scene.layout.
func (w *gpuWindow) accessibilityUpdate() {
	v, _ := accWindows.LoadOrStore(w, &windowAccessibility{})
	acc := v.(*windowAccessibility)
	acc.mu.Lock()
	if acc.provider == nil && acc.attempts < 600 {
		acc.attempts++
		ebiten.RunOnMainThread(func() {
			if hwnd := accFindAppWindow(); hwnd != 0 {
				acc.provider, _ = accAttachProvider(hwnd, w.accDefaultActions())
			}
		})
	}
	provider := acc.provider
	acc.mu.Unlock()
	if provider == nil {
		return
	}
	provider.Drain()
	snap := w.scene.accSnapshotWithMenu(w.openMenu)
	snap.Title = w.current.Title
	ebiten.RunOnMainThread(func() { provider.Publish(snap) })
}

// accessibilityClose disconnects the UIA provider. Call it on the termination
// path (before the window is destroyed), on the UI thread.
func (w *gpuWindow) accessibilityClose() {
	v, ok := accWindows.LoadAndDelete(w)
	if !ok {
		return
	}
	acc := v.(*windowAccessibility)
	acc.mu.Lock()
	provider := acc.provider
	acc.provider = nil
	acc.mu.Unlock()
	if provider != nil {
		ebiten.RunOnMainThread(provider.Close)
	}
}

func (w *gpuWindow) accessibleControl(key string) (placedNode, bool) {
	item, ok := w.scene.find(key)
	if dialog, shown := w.scene.topDialog(); shown && item.dialog != dialog.node.key {
		return placedNode{}, false
	}
	return item, ok
}

// accDefaultActions maps UIA pattern actions to the renderer's input paths.
// They run on the UI thread from Update.
func (w *gpuWindow) accDefaultActions() accActions {
	return accActions{
		Wake: func() {
			select {
			case w.accessibilityWake <- struct{}{}:
			default:
			}
		},
		Invoke: func(key string) {
			item, ok := w.accessibleControl(key)
			if !ok {
				return
			}
			w.confirmInput()
			switch item.node.kind {
			case buttonNode:
				w.press(item)
			case menuNode:
				if w.openMenu == key {
					w.openMenu = ""
				} else {
					w.openMenuList(item)
				}
			case choiceNode:
				c := item.node.value.(Choice)
				if len(c.Options) > 0 {
					w.scene.focus = key
					w.changeChoice(item, (max(0, c.Selected)+1)%len(c.Options))
				}
			case detailNode:
				if d := item.node.value.(Detail); d.Copy != "" {
					w.scene.focus = key
					w.copyMessage(d.Copy)
				}
			}
		},
		MenuItem: func(key string, index int) {
			item, ok := w.accessibleControl(key)
			if ok && item.node.kind == menuNode && w.openMenu == key {
				w.openMenu = ""
				w.pickMenu(item, index)
			}
		},
		MenuFocus: func(key string, index int) {
			item, ok := w.accessibleControl(key)
			if ok && item.node.kind == menuNode && w.openMenu == key {
				menu := item.node.value.(Menu)
				if index >= 0 && index < len(menu.Items) && menu.Items[index].enabled() {
					item.state.menu = index
				}
			}
		},
		SetMenu: func(key string, expanded bool) {
			item, ok := w.accessibleControl(key)
			if !ok || item.node.kind != menuNode {
				return
			}
			if expanded && w.openMenu != key {
				w.confirmInput()
				w.openMenuList(item)
			} else if !expanded && w.openMenu == key {
				w.openMenu = ""
			}
		},
		Toggle: func(key string) {
			if item, ok := w.accessibleControl(key); ok {
				w.pressToggle(item)
			}
		},
		SetText: func(key, value string) {
			item, ok := w.accessibleControl(key)
			if !ok {
				return
			}
			if f, ok := item.node.value.(Field); ok && f.Change != nil && !f.Disabled {
				w.confirmInput()
				w.scene.focus = key
				f.Change(value)
				w.refresh()
			}
		},
		Select: func(key string, row int) {
			if item, ok := w.accessibleControl(key); ok {
				w.confirmInput()
				w.openChoice, w.openMenu = "", ""
				if item.node.kind == tabsNode {
					w.scene.focus = key
					w.selectTab(item, row)
				} else if item.node.kind == tableNode {
					w.selectRow(item, row)
				}
			}
		},
		SetFocus: func(key string) {
			w.confirmInput()
			if item, ok := w.accessibleControl(key); ok && item.focusable() {
				if key != w.openMenu {
					w.openMenu = ""
				}
				if key != w.openChoice {
					w.openChoice = ""
				}
				w.scene.focus = key
			}
		},
	}
}
