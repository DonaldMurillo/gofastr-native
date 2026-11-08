package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr-native/tui/cmd/chordharp/harp"
)

const sampleRate = 44100

// output is a running audio stream that pulls samples from an engine.
type output interface {
	Name() string
	Close() error
}

// players are external programs that play raw mono float32 samples from
// standard input, tried in order when there is no native output.
var players = []struct {
	name string
	args []string
}{
	{"pw-cat", []string{"--playback", "--format", "f32", "--rate", "44100", "--channels", "1", "--latency", "20ms", "-"}},
	{"pacat", []string{"--playback", "--format=float32le", "--rate=44100", "--channels=1", "--latency-msec=20"}},
	{"aplay", []string{"-q", "-t", "raw", "-f", "FLOAT_LE", "-r", "44100", "-c", "1", "--buffer-time=40000"}},
	{"ffplay", []string{"-nodisp", "-autoexit", "-loglevel", "quiet", "-fflags", "nobuffer", "-probesize", "32", "-f", "f32le", "-ar", "44100", "-ch_layout", "mono", "-i", "-"}},
}

// openPipe starts the first player found on PATH and feeds it from e.
func openPipe(e *harp.Engine) (output, error) {
	for _, p := range players {
		path, err := exec.LookPath(p.name)
		if err != nil {
			continue
		}
		cmd := exec.Command(path, p.args...)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			continue
		}
		out := &pipeOutput{name: p.name, cmd: cmd, stdin: stdin, done: make(chan struct{})}
		go out.feed(e)
		return out, nil
	}
	return nil, errors.New("no audio player found (install pipewire, pulseaudio, alsa-utils or ffmpeg)")
}

type pipeOutput struct {
	name  string
	cmd   *exec.Cmd
	stdin io.WriteCloser
	done  chan struct{}
	once  sync.Once
}

func (p *pipeOutput) Name() string { return p.name }

// lead is how far ahead of the clock the feeder stays. A pipe buffers
// about a third of a second, so writing as fast as the player reads would
// delay every note by that much.
const lead = 30 * time.Millisecond

func (p *pipeOutput) feed(e *harp.Engine) {
	const frames = 256
	samples := make([]float32, frames)
	bytes := make([]byte, frames*4)
	start := time.Now()
	var written int64
	for {
		select {
		case <-p.done:
			return
		default:
		}
		ahead := time.Duration(written)*time.Second/sampleRate - time.Since(start)
		if ahead > lead {
			time.Sleep(ahead - lead)
		} else if ahead < -200*time.Millisecond {
			// Fell behind (the machine slept): start the clock again.
			start, written = time.Now(), 0
		}
		e.Render(samples)
		for i, s := range samples {
			binary.LittleEndian.PutUint32(bytes[i*4:], math.Float32bits(s))
		}
		if _, err := p.stdin.Write(bytes); err != nil {
			return
		}
		written += frames
	}
}

func (p *pipeOutput) Close() error {
	p.once.Do(func() {
		close(p.done)
		p.stdin.Close()
		done := make(chan error, 1)
		go func() { done <- p.cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(500 * time.Millisecond):
			p.cmd.Process.Kill()
			<-done
		}
	})
	return nil
}

// silent is the output when audio is off or unavailable.
type silent struct{ reason string }

func (s silent) Name() string { return s.reason }
func (silent) Close() error   { return nil }

// openAudio opens the native output if there is one, then a player.
func openAudio(e *harp.Engine) (output, error) {
	if out, err := openNative(e); err == nil {
		return out, nil
	} else if !errors.Is(err, errNoNative) {
		if pipe, perr := openPipe(e); perr == nil {
			return pipe, nil
		}
		return nil, fmt.Errorf("native audio: %w", err)
	}
	return openPipe(e)
}

var errNoNative = errors.New("no native audio output in this build")
