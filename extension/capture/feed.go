package capture

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// The functions in this file are the device side's entry points. Package
// capturebind exports them to Java; apps do not call them.

// Attach sets the directory the device writes recordings under. The Android
// adapter calls it at startup with a private directory of the app.
func Attach(root string) error {
	if root == "" || !filepath.IsAbs(root) {
		return errors.New("capture: root must be an absolute directory")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	hub.mu.Lock()
	hub.root = filepath.Clean(root)
	hub.mu.Unlock()
	// Settle recordings a previous process left running.
	enforceBudget(0, nil)
	return nil
}

// Event delivers a lifecycle event or telemetry sample as JSON and returns
// the session's directives ("stop", "keyframe", "bitrate:N", comma-separated).
// An unknown session gets "stop".
func Event(sessionID, kind, payload string) string {
	session := lookup(sessionID)
	if session == nil {
		return "stop"
	}
	switch kind {
	case "started":
		session.markStarted(payload)
	case "heartbeat":
		var progress struct{ Frames, Bytes int64 }
		_ = json.Unmarshal([]byte(payload), &progress)
		session.mu.Lock()
		session.frames = max(session.frames, progress.Frames)
		session.bytes = max(session.bytes, progress.Bytes)
		session.mu.Unlock()
		session.checkCut()
		session.tick()
	case "location":
		var location Location
		if json.Unmarshal([]byte(payload), &location) == nil {
			session.mu.Lock()
			session.location = &location
			session.mu.Unlock()
			session.telemetry(Telemetry{Location: &location})
		}
	case "orientation":
		var orientation Orientation
		if json.Unmarshal([]byte(payload), &orientation) == nil {
			session.mu.Lock()
			session.motion = &orientation
			session.mu.Unlock()
			session.telemetry(Telemetry{Orientation: &orientation})
		}
	case "rotation":
		var rotation Rotation
		if json.Unmarshal([]byte(payload), &rotation) == nil {
			session.telemetry(Telemetry{Rotation: &rotation})
		}
	case "acceleration":
		var acceleration Acceleration
		if json.Unmarshal([]byte(payload), &acceleration) == nil {
			session.mu.Lock()
			session.accel = &acceleration
			impact := session.config.Impact > 0 && acceleration.G() >= session.config.Impact
			if impact {
				session.impact = &acceleration
			}
			session.mu.Unlock()
			session.telemetry(Telemetry{Acceleration: &acceleration})
			if impact {
				session.Lock(LockImpact)
			}
		}
	case "segment":
		// A device muxer finished a segment file, or failed to: either way
		// the next one has begun.
		var report struct {
			Index      int
			Start, End int64
			Failed     bool
		}
		if json.Unmarshal([]byte(payload), &report) == nil {
			session.segmentDone(report.Index, report.Start, report.End, !report.Failed)
		}
	case "lock":
		// The notification's Lock action.
		var report struct{ Reason string }
		_ = json.Unmarshal([]byte(payload), &report)
		session.Lock(report.Reason)
	case "stopped", "failed":
		var report struct{ Reason, Message string }
		_ = json.Unmarshal([]byte(payload), &report)
		state := Stopped
		if kind == "failed" {
			state = Failed
		}
		session.finish(state, report.Reason, report.Message)
	}
	return session.directives()
}

// Sample delivers one encoded access unit. Flag bit 1 marks a keyframe.
func Sample(sessionID string, track, flags int, clock int64, data []byte) string {
	session := lookup(sessionID)
	if session == nil {
		return "stop"
	}
	media := Media{Track: Track(track), Clock: clock, Key: flags&1 != 0, Data: append([]byte(nil), data...)}
	// Read before mu: Start takes the hub lock and then a session's.
	directory := session.directory()
	session.mu.Lock()
	switch media.Track {
	case Video:
		media.Codec = "h264"
		session.frames++
		session.bytes += int64(len(data))
	case Audio:
		media.Codec = "opus"
		media.Key = true
	default:
		session.mu.Unlock()
		return session.directives()
	}
	finished, index, err := session.cutRecorder(media, directory)
	recorder := session.recorder
	session.mu.Unlock()
	if err != nil {
		// Writing on past the segment length would break the loop budget.
		session.finish(Failed, "storage", "The next segment could not be created: "+err.Error())
		return session.directives()
	}
	if finished != nil {
		session.closeSegment(finished, index)
	}
	if recorder != nil {
		recorder.write(media)
		if err := recorder.failed(); err != nil {
			session.finish(Failed, "storage", "The recording could not be written: "+err.Error())
			return session.directives()
		}
	}
	session.publish(sinkItem{media: &media})
	if media.Track == Video {
		// Frame and byte counts change with every frame, telemetry or not.
		session.tick()
	}
	return session.directives()
}

// startReport is Info plus what the device asks of Go. Recorder "go" means
// the device has no muxer of its own: Go writes the MP4 from the samples.
type startReport struct {
	Info
	Recorder string `json:"recorder"`
}

func (s *Session) markStarted(payload string) {
	var report startReport
	if err := json.Unmarshal([]byte(payload), &report); err != nil {
		s.finish(Failed, "protocol", "The device sent an unreadable start report.")
		return
	}
	s.mu.Lock()
	state := s.state
	if state == Pending {
		s.state, s.started, s.info = Running, time.Now(), report.Info
	}
	s.mu.Unlock()
	switch state {
	case Pending:
	case Running, Stopping:
		// A second start report comes from a device that lost the session,
		// such as a reloaded WebView page.
		s.finish(Stopped, "interrupted", "Capture restarted on the device.")
		return
	default:
		return
	}
	if s.config.Record {
		if directory := s.directory(); directory != "" {
			err := os.MkdirAll(directory, 0o700)
			if err == nil {
				sidecar := openSidecar(directory, 1)
				var recorder *mp4Writer
				if report.Recorder == "go" {
					recorder, err = newMP4Writer(filepath.Join(directory, segmentFile(1, ".mp4")), report.Info)
				}
				s.mu.Lock()
				s.sidecar, s.recorder = sidecar, recorder
				s.segment, s.segmentStarted = 1, time.Now()
				s.mu.Unlock()
			}
			s.writeManifest()
			enforceBudget(s.config.Budget, s)
			if err != nil && report.Recorder == "go" {
				// Go is the only muxer, so nothing would be recorded.
				s.finish(Failed, "storage", "The recording could not be created: "+err.Error())
				return
			}
		}
	}
	changed()
}

func openSidecar(directory string, index int) *os.File {
	file, err := os.OpenFile(filepath.Join(directory, segmentFile(index, ".jsonl")), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil
	}
	return file
}

// cutRecorder starts the next segment of a Go-written recording when one is
// due and media is the keyframe that can open it. It returns the finished
// writer and its index for closeSegment, or the error that kept it from
// creating the next file. Hold mu.
func (s *Session) cutRecorder(media Media, directory string) (*mp4Writer, int, error) {
	recorder := s.recorder
	if recorder == nil || media.Track != Video {
		return nil, 0, nil
	}
	start, _, ok := recorder.span()
	if !ok {
		return nil, 0, nil
	}
	if s.config.Segment > 0 && !s.cutting && media.Clock-start >= int64(s.config.Segment) {
		s.cutting = true
		if !media.Key {
			s.keyframe = true
		}
	}
	if !s.cutting || !media.Key {
		return nil, 0, nil
	}
	next, err := newMP4Writer(filepath.Join(directory, segmentFile(s.segment+1, ".mp4")), s.info)
	if err != nil {
		return nil, 0, err
	}
	s.recorder = next
	return recorder, s.segment, nil
}

// closeSegment finishes a Go-written segment file and records it. A file
// with no video, or whose index could not be written, is deleted.
func (s *Session) closeSegment(recorder *mp4Writer, index int) {
	start, end, ok := recorder.span()
	if err := recorder.close(); err != nil {
		ok = false
	}
	s.segmentDone(index, start, end, ok)
}

// segmentDone records a segment file, moves telemetry to the next segment's
// sidecar and applies the loop recording budget. A segment that did not
// finish is deleted, and recording still moves on to the next.
func (s *Session) segmentDone(index int, start, end int64, finished bool) {
	directory := s.directory()
	if directory == "" || index < 1 {
		return
	}
	size := int64(-1)
	if finished {
		if info, err := os.Stat(filepath.Join(directory, segmentFile(index, ".mp4"))); err == nil {
			size = info.Size()
		}
	}
	s.mu.Lock()
	if slices.ContainsFunc(s.segments, func(segment Segment) bool { return segment.Index == index }) {
		s.mu.Unlock()
		return
	}
	if size >= 0 {
		s.segments = append(s.segments, Segment{Index: index, Start: start, End: end, Bytes: size, Lock: s.locks[index], dir: directory})
	}
	var previous *os.File
	if index >= s.segment {
		s.segment, s.segmentStarted, s.cutting = index+1, time.Now(), false
		if s.state == Running {
			previous = s.sidecar
			s.sidecar = openSidecar(directory, s.segment)
		}
	}
	s.mu.Unlock()
	if previous != nil {
		previous.Close()
	}
	if size < 0 {
		// It has no index to play from.
		removeSegmentFiles(directory, index)
	}
	s.writeManifest()
	enforceBudget(s.config.Budget, s)
	changed()
}

// telemetry fans a sample out to the sidecar and sinks, and refreshes
// watching screens at most once a second.
func (s *Session) telemetry(sample Telemetry) {
	s.mu.Lock()
	sidecar := s.sidecar
	if sidecar != nil {
		if line, err := json.Marshal(sample); err == nil {
			sidecar.Write(append(line, '\n'))
		}
	}
	s.mu.Unlock()
	s.publish(sinkItem{telemetry: &sample})
	s.tick()
}

// tick refreshes watching screens at most once a second, so a status
// screen's counts and duration move even when no telemetry arrives.
func (s *Session) tick() {
	s.mu.Lock()
	due := time.Since(s.lastTick) >= time.Second
	if due {
		s.lastTick = time.Now()
	}
	s.mu.Unlock()
	if due {
		changed()
	}
}
