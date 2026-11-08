//go:build darwin && cgo

package main

/*
#cgo LDFLAGS: -framework AudioToolbox -framework CoreFoundation
#include <AudioToolbox/AudioToolbox.h>

extern void chordharpFill(float *samples, int frames);

static void chordharpCallback(void *user, AudioQueueRef queue, AudioQueueBufferRef buffer) {
	int frames = buffer->mAudioDataBytesCapacity / sizeof(float);
	chordharpFill((float *)buffer->mAudioData, frames);
	buffer->mAudioDataByteSize = frames * sizeof(float);
	AudioQueueEnqueueBuffer(queue, buffer, 0, NULL);
}

static OSStatus chordharpStart(double rate, int frames, int buffers, AudioQueueRef *out) {
	AudioStreamBasicDescription f = {0};
	f.mSampleRate = rate;
	f.mFormatID = kAudioFormatLinearPCM;
	f.mFormatFlags = kLinearPCMFormatFlagIsFloat | kLinearPCMFormatFlagIsPacked;
	f.mBytesPerPacket = sizeof(float);
	f.mFramesPerPacket = 1;
	f.mBytesPerFrame = sizeof(float);
	f.mChannelsPerFrame = 1;
	f.mBitsPerChannel = 32;
	AudioQueueRef queue;
	// A NULL run loop makes the queue call back on its own thread.
	OSStatus status = AudioQueueNewOutput(&f, chordharpCallback, NULL, NULL, NULL, 0, &queue);
	if (status != noErr) return status;
	for (int i = 0; i < buffers; i++) {
		AudioQueueBufferRef buffer;
		status = AudioQueueAllocateBuffer(queue, frames * sizeof(float), &buffer);
		if (status != noErr) {
			AudioQueueDispose(queue, true);
			return status;
		}
		chordharpCallback(NULL, queue, buffer);
	}
	status = AudioQueueStart(queue, NULL);
	if (status != noErr) {
		AudioQueueDispose(queue, true);
		return status;
	}
	*out = queue;
	return noErr;
}

static void chordharpStop(AudioQueueRef queue) {
	AudioQueueStop(queue, true);
	AudioQueueDispose(queue, true);
}
*/
import "C"

import (
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/DonaldMurillo/gofastr-native/tui/cmd/chordharp/harp"
)

// active is the engine the audio thread renders. The callback cannot carry
// a Go pointer, so there is one output per process.
var active atomic.Pointer[harp.Engine]

//export chordharpFill
func chordharpFill(samples *C.float, frames C.int) {
	out := unsafe.Slice((*float32)(unsafe.Pointer(samples)), int(frames))
	if e := active.Load(); e != nil {
		e.Render(out)
		return
	}
	clear(out)
}

type coreAudio struct {
	queue C.AudioQueueRef
	once  sync.Once
}

// openNative starts an AudioQueue: three buffers of 256 frames, about
// 17 ms of latency at 44.1 kHz.
func openNative(e *harp.Engine) (output, error) {
	if !active.CompareAndSwap(nil, e) {
		return nil, fmt.Errorf("audio output already open")
	}
	var queue C.AudioQueueRef
	if status := C.chordharpStart(C.double(sampleRate), 256, 3, &queue); status != 0 {
		active.Store(nil)
		return nil, fmt.Errorf("AudioQueue error %d", int(status))
	}
	return &coreAudio{queue: queue}, nil
}

func (c *coreAudio) Name() string { return "Core Audio" }

func (c *coreAudio) Close() error {
	c.once.Do(func() {
		C.chordharpStop(c.queue)
		active.Store(nil)
	})
	return nil
}
