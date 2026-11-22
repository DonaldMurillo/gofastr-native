//go:build windows

package main

import (
	"golang.org/x/sys/windows"
	"unsafe"
)

var processMemoryInfo = windows.NewLazySystemDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")

// processMemoryCounters mirrors PROCESS_MEMORY_COUNTERS.
type processMemoryCounters struct {
	Size                  uint32
	PageFaultCount        uint32
	PeakWorkingSetSize    uintptr
	WorkingSetSize        uintptr
	QuotaPeakPagedPool    uintptr
	QuotaPagedPool        uintptr
	QuotaPeakNonPagedPool uintptr
	QuotaNonPagedPool     uintptr
	PagefileUsage         uintptr
	PeakPagefileUsage     uintptr
}

// memorySample is a process-wide resource snapshot.
type memorySample struct {
	WorkingSetBytes     uint64
	PeakWorkingSetBytes uint64
}

func sampleMemory() memorySample {
	var counters processMemoryCounters
	counters.Size = uint32(unsafe.Sizeof(counters))
	handle := windows.CurrentProcess()
	ret, _, _ := processMemoryInfo.Call(
		uintptr(handle), uintptr(unsafe.Pointer(&counters)), uintptr(counters.Size))
	if ret == 0 {
		return memorySample{}
	}
	return memorySample{WorkingSetBytes: uint64(counters.WorkingSetSize), PeakWorkingSetBytes: uint64(counters.PeakWorkingSetSize)}
}

// cpuSample is the process's accumulated CPU time.
type cpuSample struct {
	UserMS   float64
	KernelMS float64
}

func sampleCPU() cpuSample {
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &created, &exited, &kernel, &user); err != nil {
		return cpuSample{}
	}
	return cpuSample{UserMS: float64(user.Nanoseconds()) / 1e6, KernelMS: float64(kernel.Nanoseconds()) / 1e6}
}

func (s cpuSample) totalMS() float64 { return s.UserMS + s.KernelMS }

// processCreated reports when the operating system created this process,
// before the Go runtime started. It anchors cold-start measurements.
func processCreated() (msSinceUnixEpoch float64, ok bool) {
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &created, &exited, &kernel, &user); err != nil {
		return 0, false
	}
	return float64(created.Nanoseconds()) / 1e6, true
}

// foregroundIsOwnWindow reports whether the window the user is interacting
// with belongs to this process. A hidden or background window never is.
func foregroundIsOwnWindow() bool {
	foreground := windows.GetForegroundWindow()
	if foreground == 0 {
		return false
	}
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(foreground, &pid); err != nil {
		return false
	}
	return pid == uint32(windows.GetCurrentProcessId())
}
