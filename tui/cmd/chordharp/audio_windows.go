//go:build windows

package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/DonaldMurillo/gofastr-native/tui/cmd/chordharp/harp"

	"golang.org/x/sys/windows"
)

// The Windows output uses WinMM's waveOut, loaded from winmm.dll, so it
// needs no cgo. The driver signals an event as each buffer finishes, and
// one goroutine refills and requeues the finished ones.

var (
	winmm                 = windows.NewLazySystemDLL("winmm.dll")
	procWaveOutOpen       = winmm.NewProc("waveOutOpen")
	procWaveOutPrepare    = winmm.NewProc("waveOutPrepareHeader")
	procWaveOutUnprepare  = winmm.NewProc("waveOutUnprepareHeader")
	procWaveOutWrite      = winmm.NewProc("waveOutWrite")
	procWaveOutReset      = winmm.NewProc("waveOutReset")
	procWaveOutClose      = winmm.NewProc("waveOutClose")
	procWaveOutGetErrText = winmm.NewProc("waveOutGetErrorTextW")
)

const (
	waveFormatPCM = 1
	waveMapper    = 0xFFFFFFFF // the default output device
	callbackEvent = 0x00050000
	whdrDone      = 0x00000001

	// Four buffers of 768 frames: 17 ms each, about 70 ms queued. Smaller
	// buffers underrun on many WinMM drivers, whose own period is 10 ms.
	waveBuffers = 4
	waveFrames  = 768
)

// waveFormat is WAVEFORMATEX. The C struct is 18 bytes; the trailing
// padding Go adds is never read, since cbSize is 0 for PCM.
type waveFormat struct {
	formatTag      uint16
	channels       uint16
	samplesPerSec  uint32
	avgBytesPerSec uint32
	blockAlign     uint16
	bitsPerSample  uint16
	cbSize         uint16
}

// waveHdr is WAVEHDR. The driver writes flags from its own thread, so
// it is read atomically.
type waveHdr struct {
	data          *int16
	bufferLength  uint32
	bytesRecorded uint32
	user          uintptr
	flags         uint32
	loops         uint32
	next          uintptr
	reserved      uintptr
}

type waveOut struct {
	handle  uintptr
	event   windows.Handle
	headers [waveBuffers]*waveHdr
	buffers [waveBuffers][]int16
	pinner  runtime.Pinner
	closing atomic.Bool
	done    chan struct{}
	once    sync.Once
}

func mmError(call string, code uintptr) error {
	var text [256]uint16
	procWaveOutGetErrText.Call(code, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)))
	if msg := windows.UTF16ToString(text[:]); msg != "" {
		return fmt.Errorf("%s: %s", call, msg)
	}
	return fmt.Errorf("%s: MMSYSERR %d", call, code)
}

// openNative opens the default output device as 16-bit mono PCM.
func openNative(e *harp.Engine) (output, error) {
	if err := winmm.Load(); err != nil {
		return nil, err
	}
	event, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return nil, err
	}
	w := &waveOut{event: event, done: make(chan struct{})}
	format := waveFormat{
		formatTag: waveFormatPCM, channels: 1, samplesPerSec: sampleRate,
		avgBytesPerSec: sampleRate * 2, blockAlign: 2, bitsPerSample: 16,
	}
	if r, _, _ := procWaveOutOpen.Call(uintptr(unsafe.Pointer(&w.handle)), waveMapper,
		uintptr(unsafe.Pointer(&format)), uintptr(event), 0, callbackEvent); r != 0 {
		windows.CloseHandle(event)
		return nil, mmError("waveOutOpen", r)
	}
	// The driver keeps pointers to the headers and their samples until
	// they are unprepared, so both stay pinned until Close.
	for i := range w.headers {
		w.buffers[i] = make([]int16, waveFrames)
		w.pinner.Pin(&w.buffers[i][0])
		h := &waveHdr{data: &w.buffers[i][0], bufferLength: waveFrames * 2}
		w.pinner.Pin(h)
		w.headers[i] = h
		if r, _, _ := procWaveOutPrepare.Call(w.handle, uintptr(unsafe.Pointer(h)), unsafe.Sizeof(*h)); r != 0 {
			w.release(i)
			return nil, mmError("waveOutPrepareHeader", r)
		}
	}
	go w.run(e)
	return w, nil
}

// run keeps every buffer queued: it fills each one the driver has
// finished and writes it back, until Close.
func (w *waveOut) run(e *harp.Engine) {
	defer close(w.done)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	scratch := make([]float32, waveFrames)
	for i := range w.headers {
		w.fill(e, i, scratch)
	}
	for !w.closing.Load() {
		windows.WaitForSingleObject(w.event, 200)
		for i, h := range w.headers {
			if w.closing.Load() {
				return
			}
			if atomic.LoadUint32(&h.flags)&whdrDone != 0 {
				w.fill(e, i, scratch)
			}
		}
	}
}

func (w *waveOut) fill(e *harp.Engine, i int, scratch []float32) {
	e.Render(scratch)
	buf := w.buffers[i]
	for k, v := range scratch {
		buf[k] = int16(v * 32767)
	}
	// waveOutWrite clears the done flag and queues the buffer.
	procWaveOutWrite.Call(w.handle, uintptr(unsafe.Pointer(w.headers[i])), unsafe.Sizeof(*w.headers[i]))
}

// release resets the device, unprepares the first n headers, closes it
// and unpins the buffers.
func (w *waveOut) release(n int) {
	procWaveOutReset.Call(w.handle)
	for _, h := range w.headers[:n] {
		procWaveOutUnprepare.Call(w.handle, uintptr(unsafe.Pointer(h)), unsafe.Sizeof(*h))
	}
	procWaveOutClose.Call(w.handle)
	windows.CloseHandle(w.event)
	w.pinner.Unpin()
}

func (w *waveOut) Name() string { return "WinMM" }

func (w *waveOut) Close() error {
	w.once.Do(func() {
		w.closing.Store(true)
		windows.SetEvent(w.event)
		<-w.done
		w.release(len(w.headers))
	})
	return nil
}
