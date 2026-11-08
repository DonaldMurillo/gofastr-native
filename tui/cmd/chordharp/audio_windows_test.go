//go:build windows

package main

import (
	"testing"
	"unsafe"
)

// The structs passed to winmm.dll must match the C layout of WAVEHDR and
// the first 18 bytes of WAVEFORMATEX.
func TestWaveStructLayout(t *testing.T) {
	ptr := unsafe.Sizeof(uintptr(0))
	var h waveHdr
	if got, want := unsafe.Offsetof(h.flags), 2*ptr+8; got != want {
		t.Errorf("WAVEHDR.dwFlags at %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(h), 4*ptr+16; got != want {
		t.Errorf("WAVEHDR is %d bytes, want %d", got, want)
	}
	var f waveFormat
	if got := unsafe.Offsetof(f.cbSize); got != 16 {
		t.Errorf("WAVEFORMATEX.cbSize at %d, want 16", got)
	}
}
