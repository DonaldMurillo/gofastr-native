package desktop

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	getClipboardData = user32.NewProc("GetClipboardData")
	globalSize       = kernel32.NewProc("GlobalSize")
)

func readClipboard() (string, bool) {
	if ok, _, _ := openClipboard.Call(0); ok == 0 {
		return "", false
	}
	defer closeClipboard.Call()
	block, _, _ := getClipboardData.Call(13) // CF_UNICODETEXT
	if block == 0 {
		return "", false
	}
	size, _, _ := globalSize.Call(block)
	if size < 2 || size > 4<<20 {
		return "", false
	}
	address, _, _ := globalLock.Call(block)
	if address == 0 {
		return "", false
	}
	defer globalUnlock.Call(block)
	data := make([]uint16, size/2)
	var read uintptr
	if err := windows.ReadProcessMemory(windows.CurrentProcess(), address, (*byte)(unsafe.Pointer(&data[0])), uintptr(len(data))*2, &read); err != nil {
		return "", false
	}
	return windows.UTF16ToString(data), true
}

func writeClipboard(hwnd uintptr, text string) bool {
	data, err := windows.UTF16FromString(text)
	if err != nil {
		return false
	}
	if ok, _, _ := openClipboard.Call(hwnd); ok == 0 {
		return false
	}
	defer closeClipboard.Call()
	block, _, _ := globalAlloc.Call(0x02, uintptr(len(data))*2) // GMEM_MOVEABLE
	if block == 0 {
		return false
	}
	owned := true
	defer func() {
		if owned {
			globalFree.Call(block)
		}
	}()
	if err := fillClipboardBlock(block, data); err != nil {
		return false
	}
	if ok, _, _ := emptyClipboard.Call(); ok == 0 {
		return false
	}
	result, _, _ := setClipboardData.Call(13, block) // CF_UNICODETEXT
	owned = result == 0
	return !owned
}

func copyForeground(text string, process int) bool {
	window, _, _ := getForeground.Call()
	var owner uint32
	if window == 0 {
		return false
	}
	getWindowPID.Call(window, uintptr(unsafe.Pointer(&owner)))
	return int(owner) == process && writeClipboard(window, text)
}

func fillClipboardBlock(block uintptr, data []uint16) error {
	address, _, err := globalLock.Call(block)
	if address == 0 {
		return err
	}
	defer globalUnlock.Call(block)
	var written uintptr
	return windows.WriteProcessMemory(windows.CurrentProcess(), address, (*byte)(unsafe.Pointer(&data[0])), uintptr(len(data))*2, &written)
}
