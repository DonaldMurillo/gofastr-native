package desktop

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"syscall"
	"time"
	"unsafe"

	"github.com/hajimehoshi/ebiten/v2"
)

// This file drives the real DirectX window with synthetic Win32 input
// messages (WM_CHAR, WM_KEYDOWN/WM_KEYUP, WM_IME_ENDCOMPOSITION) through
// the composer's window subclass and per-session event channel, with cgo
// disabled. Enable it with GOFASTR_DESKTOP_IME_TEST=1; like the DirectX
// check it runs from TestMain on the main thread.
//
// It is NOT a real IME run. Committed text typed through WM_CHAR follows
// the same pipeline an IME commit takes (the subclass emits the same
// committed-state event an IME's GCS_RESULTSTR does), and
// WM_IME_ENDCOMPOSITION is the message every IME cancel ends with, but
// composition content (GCS_COMPSTR read from the input method context)
// cannot be synthesized without an installed IME: ImmSetCompositionString
// fails on the default input context. Composition and clause policy is
// covered headlessly in editor_ime_test.go instead.

var (
	imeUser32          = syscall.NewLazyDLL("user32.dll")
	imeKernel32        = syscall.NewLazyDLL("kernel32.dll")
	imeSetFocus        = imeUser32.NewProc("SetFocus")
	imeSetForeground   = imeUser32.NewProc("SetForegroundWindow")
	imeGetForeground   = imeUser32.NewProc("GetForegroundWindow")
	imeEnumWindows     = imeUser32.NewProc("EnumWindows")
	imeIsWindowVisible = imeUser32.NewProc("IsWindowVisible")
	imeWindowThread    = imeUser32.NewProc("GetWindowThreadProcessId")
	imeCurrentThread   = imeKernel32.NewProc("GetCurrentThreadId")
	imeAttachInput     = imeUser32.NewProc("AttachThreadInput")
)

const (
	imeWMNull    = 0x0000
	imeWMKeyDown = 0x0100
	imeWMKeyUp   = 0x0101
	imeWMChar    = 0x0102
	imeWMImeEnd  = 0x0110

	imeVKBack    = 0x08
	imeVKReturn  = 0x0d
	imeVKControl = 0x11
	imeVKLeft    = 0x25
)

func imePost(hwnd uintptr, msg uint32, wParam uintptr) {
	imePostL(hwnd, msg, wParam, 0)
}

func imePostL(hwnd uintptr, msg uint32, wParam, lParam uintptr) {
	postMessage.Call(hwnd, uintptr(msg), wParam, lParam)
}

// imeType posts WM_CHAR units. Surrogate pairs are posted as two units and
// reassembled by the composer's window procedure, as a real keyboard or IME
// delivers them.
func imeType(hwnd uintptr, units ...rune) {
	for _, unit := range units {
		imePost(hwnd, imeWMChar, uintptr(unit))
	}
}

// imeFocusWindow brings the test window to the foreground and gives it
// keyboard focus. The composer starts no session unless the window is
// active, and a plain test process may not own the foreground, so the
// helper attaches its thread to the window's input queue first.
func imeFocusWindow() (uintptr, error) {
	pid := uintptr(os.Getpid())
	found := uintptr(0)
	// The game window is the only visible top-level window this process
	// owns; matching by process avoids depending on the window title.
	enumerate := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var owner uintptr
		imeWindowThread.Call(hwnd, uintptr(unsafe.Pointer(&owner)))
		if owner != pid {
			return 1
		}
		if visible, _, _ := imeIsWindowVisible.Call(hwnd); visible == 0 {
			return 1
		}
		found = hwnd
		return 0
	})
	deadline := time.Now().Add(8 * time.Second)
	for found == 0 {
		imeEnumWindows.Call(enumerate, 0)
		if time.Now().After(deadline) {
			return 0, errors.New("the DirectX window never appeared")
		}
		time.Sleep(50 * time.Millisecond)
	}
	us, _, _ := imeCurrentThread.Call()
	thread, _, _ := imeWindowThread.Call(found, 0)
	fore, _, _ := imeGetForeground.Call()
	foreThread, _, _ := imeWindowThread.Call(fore, 0)
	imeAttachInput.Call(us, foreThread, 1)
	imeAttachInput.Call(us, thread, 1)
	imeSetForeground.Call(found)
	imeSetFocus.Call(found)
	imeAttachInput.Call(us, thread, 0)
	imeAttachInput.Call(us, foreThread, 0)
	return found, nil
}

func testIMEPipeline() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const title = "IME pipeline test"
	line, area := "", ""
	updates := make(chan struct{}, 1)
	window, err := newGPUWindow(ctx, func() View {
		return View{Title: title, Content: VStack(8,
			TextInput("field", Field{Value: line, Change: func(next string) { line = next }}).Width(320),
			TextArea("area", Field{Value: area, Change: func(next string) { area = next }}).Height(120),
		).Padding(10)}
	})
	if err != nil {
		return err
	}
	hwndCh := make(chan uintptr, 1)
	go func() {
		defer close(hwndCh)
		hwnd, err := imeFocusWindow()
		if err == nil {
			hwndCh <- hwnd
		}
	}()
	// VsyncOffMinimum (set after the first draw) only renders on demand, and
	// synthetic messages alone do not pace the run. Keep frames coming.
	pacerDone := make(chan struct{})
	go func() {
		defer close(pacerDone)
		ticks := time.NewTicker(time.Second / 60)
		defer ticks.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticks.C:
				ebiten.ScheduleFrame()
			}
		}
	}()
	var failure error
	fail := func(format string, args ...any) {
		failure = fmt.Errorf(format, args...)
		cancel()
	}
	var hwnd uintptr
	node := func(key string) placedNode {
		item, _ := window.scene.find(key)
		return item
	}
	center := func(key string) image.Point {
		item := node(key)
		box := item.editBox()
		return box.Min.Add(box.Size().Div(2))
	}
	// Ebiten serves event-driven redraws without running Update, so the run
	// cannot count frames. Each step waits for an observable effect of the
	// previous one before sending the next input; key releases are timed
	// because a synthetic press has no observable "still held" state that
	// Update has not consumed yet.
	type stage struct {
		wait func() bool
		work func()
	}
	// Scheduled frames can present without running Update, but a message
	// forces a full frame. Hold each key long enough for one such frame to
	// observe the press, and poke the queue so one arrives. Extended keys
	// (the arrows) also need their scancode and extended bit in lParam, or
	// the posted press maps to no key at all.
	tap := func(vk, lParam uintptr) {
		imePostL(hwnd, imeWMKeyDown, vk, lParam)
		time.AfterFunc(120*time.Millisecond, func() { imePostL(hwnd, imeWMNull, 0, 0) })
		time.AfterFunc(400*time.Millisecond, func() {
			imePostL(hwnd, imeWMKeyUp, vk, lParam|0x80000000)
			time.AfterFunc(120*time.Millisecond, func() { imePostL(hwnd, imeWMNull, 0, 0) })
		})
	}
	leftDown := uintptr(0x014b0000)
	caretAt := func(want int) func() bool {
		return func() bool { return node("field").state.edit.cursor == want }
	}
	stages := []stage{
		{wait: func() bool { return ebiten.IsFocused() && hwnd != 0 }, work: func() {
			window.click(center("field"))
		}},
		{wait: func() bool { return window.text.key == "field" }, work: func() {
			// Committed text follows the same pipeline an IME commit takes.
			imeType(hwnd, 'h', 'e', 'l', 'l', 'o')
		}},
		{wait: func() bool { return line == "hello" }, work: func() {
			tap(imeVKLeft, leftDown)
		}},
		{wait: func() bool { return caretAt(4)() && !ebiten.IsKeyPressed(ebiten.KeyArrowLeft) }, work: func() {
			tap(imeVKLeft, leftDown)
		}},
		{wait: func() bool { return caretAt(3)() && !ebiten.IsKeyPressed(ebiten.KeyArrowLeft) }, work: func() {
			imeType(hwnd, 'X')
		}},
		{wait: func() bool { return line == "helXlo" }, work: func() {
			// The message every IME cancel ends with; the session must
			// survive it and keep accepting input.
			imePost(hwnd, imeWMImeEnd, 0)
			time.AfterFunc(120*time.Millisecond, func() { imeType(hwnd, 0xd83d, 0xde00) })
		}},
		{wait: func() bool { return line == "helX😀lo" }, work: func() {
			// Delete the emoji: backspace must remove the whole grapheme.
			tap(imeVKBack, 0)
		}},
		{wait: func() bool { return line == "helXlo" }, work: func() {
			window.click(center("area"))
		}},
		{wait: func() bool { return window.text.key == "area" }, work: func() {
			tap(imeVKReturn, 0)
		}},
		{wait: func() bool { return area == "\n" }, work: func() {
			time.AfterFunc(200*time.Millisecond, cancel)
		}},
	}
	var next int
	deadline := time.Now().Add(20 * time.Second)
	game := &gpuTestGame{gpuWindow: window, afterDraw: func(screen *ebiten.Image) {
		if hwnd == 0 {
			// Never block the draw callback: the helper may still be
			// looking for the window.
			select {
			case handle, ok := <-hwndCh:
				if !ok {
					fail("the window handle was never found")
					return
				}
				hwnd = handle
			default:
			}
		}
		if failure != nil || window.err != nil || next >= len(stages) {
			if window.err != nil {
				fail("window error: %v", window.err)
			}
			return
		}
		if !stages[next].wait() {
			if time.Now().After(deadline) {
				fail("stage %d stalled: line=%q area=%q key=%q", next, line, area, window.text.key)
			}
			return
		}
		stages[next].work()
		next++
		deadline = time.Now().Add(5 * time.Second)
	}}
	err = runGPUWindow(window, updates, game)
	<-pacerDone
	if failure != nil {
		return failure
	}
	if err != nil {
		return err
	}
	if next < len(stages) {
		return fmt.Errorf("the run ended at stage %d of %d", next, len(stages))
	}
	return nil
}
