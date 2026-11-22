package desktop

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/framework"
)

func TestNativeViewportDuringReplay(t *testing.T) {
	if os.Getenv("GOFASTR_DESKTOP_WINDOW_TEST") != "1" {
		t.Skip("set GOFASTR_DESKTOP_WINDOW_TEST=1 on Windows to exercise the native window")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	const initial = 1_000_000
	count, selected := initial, -1
	follow := true
	view := func() View {
		return View{Title: "Log Explorer test", Table: Table{
			Key: "all", Columns: []Column{{Title: "Message", Width: 200}},
			Count: count, Follow: follow, Selected: selected,
			Cell: func(row, _ int) string { return fmt.Sprintf("Entry %d", row+1) },
			Select: func(row int) {
				selected = row
				follow = false
			},
		}}
	}
	w, err := openWindow(view)
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	const getTop = lvmBase + 39
	top := int(send(w.table, getTop, 0, 0))
	if top < initial-100 {
		t.Fatalf("initial top row = %d, want live tail", top)
	}
	const target = initial - 6
	w.selectRow(target, lvSelected|lvFocused)
	send(w.hwnd, wmSelect, 0, 0)
	if selected != target || follow {
		t.Fatalf("selection = %d, follow = %t", selected, follow)
	}

	for range 500 {
		count += 100
		send(w.hwnd, wmRefresh, 0, 0)
		if got := int(send(w.table, getTop, 0, 0)); got != top {
			t.Fatalf("at %d rows, paused viewport moved from %d to %d", count, top, got)
		}
		if got := w.selectedRow(); got != target {
			t.Fatalf("at %d rows, native selection = %d, want %d", count, got, target)
		}
	}
	w.drag(w.splitX - 120)
	if got := w.selectedRow(); got != target {
		t.Fatalf("resizing the split changed selection to %d", got)
	}
	buffer := [32]uint16{}
	n := displayInfo{
		Header: notification{From: w.table, Code: lvnGetDisplay},
		Item: listItem{
			Mask: lvText, Index: target, Text: &buffer[0], TextLimit: int32(len(buffer)),
		},
	}
	sendPtr(w.hwnd, wmNotify, idTable, unsafe.Pointer(&n))
	if got := string(utf16.Decode(buffer[:13])); got != "Entry 999995\x00" {
		t.Fatalf("virtual row text = %q", got)
	}
	follow = true
	w.render(view())
	if tail := int(send(w.table, getTop, 0, 0)); tail < count-100 {
		t.Fatalf("resumed top row = %d, want the live tail at %d", tail, count)
	}
}

func TestNativeHostWindowLifecycle(t *testing.T) {
	if os.Getenv("GOFASTR_DESKTOP_WINDOW_TEST") != "1" {
		t.Skip("set GOFASTR_DESKTOP_WINDOW_TEST=1 on Windows to exercise the native window")
	}
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{
		Name: "Desktop lifecycle test", DisableSignalHandling: true,
	}))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	updates := make(chan struct{}, 1)
	views := 0
	err := app.Run(ctx, Host{
		Updates: updates, NativeControls: true,
		View: func() View {
			select {
			case <-app.ServicesReady():
			default:
				t.Error("view was called before GoFastr services were ready")
			}
			views++
			if views == 1 {
				updates <- struct{}{}
			} else {
				cancel()
			}
			return View{Title: "Desktop lifecycle test"}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if views != 2 {
		t.Fatalf("rendered %d views, want initial render and one worker update", views)
	}
}
