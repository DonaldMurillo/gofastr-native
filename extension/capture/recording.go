package capture

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"
)

const manifestFile = "session.json"

// Lock reasons the package writes. Apps may lock with their own.
const (
	LockManual = "manual"
	LockImpact = "impact"
)

// Recording is a session recorded on this device, for upload after capture.
// Each lives in its own directory under the capture root until Remove.
type Recording struct {
	ID      string    `json:"id"`
	Config  Config    `json:"config"`
	Info    Info      `json:"info"`
	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended,omitzero"`
	// State is Running while capture continues. Recordings reports one whose
	// process died as Stopped with Reason "interrupted". Its finished
	// segments play. The segment it was writing is not listed, and the next
	// Attach or session start deletes its file.
	State  State  `json:"state"`
	Reason string `json:"reason,omitempty"`
	// Segments are the finished MP4 files, oldest first. Loop recording and
	// RemoveSegment leave gaps in the numbering.
	Segments []Segment `json:"segments,omitempty"`
	dir      string
}

// Segment is one finished MP4 file of a recording and the telemetry received
// while it was written.
type Segment struct {
	Index int `json:"index"`
	// Start and End are the device clock values of the first and last video
	// frame. Info.WallTime converts them.
	Start int64 `json:"start"`
	End   int64 `json:"end"`
	// Bytes is the MP4's size.
	Bytes int64 `json:"bytes"`
	// Lock is why the segment is kept from loop recording, such as
	// LockImpact or LockManual. Empty means unlocked.
	Lock string `json:"lock,omitempty"`
	dir  string
}

// Locked reports whether loop recording keeps the segment.
func (s Segment) Locked() bool { return s.Lock != "" }

// Duration is the time from the first to the last video frame.
func (s Segment) Duration() time.Duration { return time.Duration(s.End - s.Start) }

// OpenVideo opens the MP4: H.264 video, with AAC audio from the Android
// adapter or Opus audio from the WebView adapter when audio was recorded.
func (s Segment) OpenVideo() (*os.File, error) {
	return os.Open(filepath.Join(s.dir, segmentFile(s.Index, ".mp4")))
}

// OpenTelemetry opens the sidecar: one JSON Telemetry object per line, as
// Go received them while the segment was written. Samples near a boundary
// can land in the neighbouring segment's file; their Clock places them.
func (s Segment) OpenTelemetry() (*os.File, error) {
	return os.Open(filepath.Join(s.dir, segmentFile(s.Index, ".jsonl")))
}

func segmentFile(index int, extension string) string {
	return fmt.Sprintf("segment-%05d%s", index, extension)
}

// removeSegmentFiles deletes a segment's MP4 and sidecar. It reports an
// error only when the MP4 is still there.
func removeSegmentFiles(dir string, index int) error {
	err := os.Remove(filepath.Join(dir, segmentFile(index, ".mp4")))
	os.Remove(filepath.Join(dir, segmentFile(index, ".jsonl")))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// diskBytes is what a segment's files take: the MP4 and its sidecar, which
// at a low bitrate and high sensor rate can rival the video.
func (s Segment) diskBytes() int64 {
	total := s.Bytes
	if info, err := os.Stat(filepath.Join(s.dir, segmentFile(s.Index, ".jsonl"))); err == nil {
		total += info.Size()
	}
	return total
}

// Complete reports whether capture ended with at least one segment.
func (r Recording) Complete() bool { return !r.State.Active() && len(r.Segments) > 0 }

// Bytes is the size of the recording's segments.
func (r Recording) Bytes() int64 {
	var total int64
	for _, segment := range r.Segments {
		total += segment.Bytes
	}
	return total
}

// Remove deletes the recording, for example after a confirmed upload. It
// refuses while the session is still active.
func (r Recording) Remove() error {
	if session := lookup(r.ID); session != nil && session.State().Active() {
		return errors.New("capture: the recording is still in progress")
	}
	store.Lock()
	defer store.Unlock()
	return os.RemoveAll(r.dir)
}

// Lock keeps a segment from loop recording, giving the reason. An empty
// reason unlocks it. It works while the session runs and after.
func (r Recording) Lock(index int, reason string) error {
	return editSegments(r, func(segments []Segment) ([]Segment, error) {
		at := slices.IndexFunc(segments, func(s Segment) bool { return s.Index == index })
		if at < 0 {
			return nil, errors.New("capture: no such segment")
		}
		segments[at].Lock = reason
		return segments, nil
	})
}

// RemoveSegment deletes one finished segment, locked or not. A stopped
// recording left with no segments is removed.
func (r Recording) RemoveSegment(index int) error {
	return editSegments(r, func(segments []Segment) ([]Segment, error) {
		at := slices.IndexFunc(segments, func(s Segment) bool { return s.Index == index })
		if at < 0 {
			return nil, errors.New("capture: no such segment")
		}
		if err := removeSegmentFiles(r.dir, index); err != nil {
			return nil, err
		}
		return slices.Delete(segments, at, at+1), nil
	})
}

// store serializes manifest writes, segment edits and loop deletion. Take
// it before a session's mu, never after.
var store sync.Mutex

// editSegments changes a recording's segment list: the live session's when
// it is still active, else the manifest on disk.
func editSegments(r Recording, edit func([]Segment) ([]Segment, error)) error {
	store.Lock()
	defer store.Unlock()
	if session := lookup(r.ID); session != nil {
		session.mu.Lock()
		if session.state.Active() {
			segments, err := edit(session.segments)
			if err == nil {
				session.segments = segments
				for _, segment := range segments {
					session.locks[segment.Index] = segment.Lock
				}
			}
			session.mu.Unlock()
			if err == nil {
				session.saveManifest()
				changed()
			}
			return err
		}
		session.mu.Unlock()
	}
	recording, pending, err := readManifest(r.dir)
	if err != nil {
		return err
	}
	segments, err := edit(recording.Segments)
	if err != nil {
		return err
	}
	recording.Segments = segments
	if len(segments) == 0 {
		err = os.RemoveAll(r.dir)
	} else {
		err = saveManifest(r.dir, recording, pending)
	}
	changed()
	return err
}

// Recordings lists recordings on the device, newest first.
func Recordings() ([]Recording, error) {
	hub.mu.Lock()
	root := hub.root
	hub.mu.Unlock()
	if root == "" {
		return nil, errors.New("capture: no device storage attached")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var recordings []Recording
	for _, entry := range entries {
		if !entry.IsDir() || !validID(entry.Name()) {
			continue
		}
		recording, err := readRecording(filepath.Join(root, entry.Name()))
		if err != nil || recording.ID != entry.Name() {
			continue
		}
		if session := lookup(recording.ID); session == nil && recording.State.Active() {
			// The process that ran it is gone.
			recording.State, recording.Reason = Stopped, "interrupted"
		}
		recordings = append(recordings, recording)
	}
	sort.Slice(recordings, func(i, j int) bool { return recordings[i].Started.After(recordings[j].Started) })
	return recordings, nil
}

// savedManifest is session.json: the recording, plus the locks a running
// session holds on segments it has not finished, so a crash right after the
// muxer finished one keeps its lock.
type savedManifest struct {
	Recording
	Pending map[int]string `json:"pending,omitempty"`
}

func readRecording(dir string) (Recording, error) {
	recording, _, err := readManifest(dir)
	return recording, err
}

func readManifest(dir string) (Recording, map[int]string, error) {
	content, err := os.ReadFile(filepath.Join(dir, manifestFile))
	if err != nil {
		return Recording{}, nil, err
	}
	var saved savedManifest
	if err := json.Unmarshal(content, &saved); err != nil {
		return Recording{}, nil, err
	}
	recording := saved.Recording
	recording.dir = dir
	for i := range recording.Segments {
		recording.Segments[i].dir = dir
	}
	return recording, saved.Pending, nil
}

// saveManifest replaces a recording's manifest atomically. Hold store.
func saveManifest(dir string, recording Recording, pending map[int]string) error {
	content, err := json.MarshalIndent(savedManifest{recording, pending}, "", "  ")
	if err != nil {
		return err
	}
	temporary := filepath.Join(dir, manifestFile+".tmp")
	if err := os.WriteFile(temporary, content, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, filepath.Join(dir, manifestFile))
}

// recording snapshots the session as its manifest describes it, with the
// locks on segments it has not finished.
func (s *Session) recording() (Recording, map[int]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	recording := Recording{ID: s.id, Config: s.config, Info: s.info, Started: s.started, State: s.state, Reason: s.reason,
		Segments: slices.Clone(s.segments)}
	if !s.state.Active() {
		recording.Ended = s.ended
	}
	var pending map[int]string
	for index, reason := range s.locks {
		if reason != "" && !slices.ContainsFunc(s.segments, func(segment Segment) bool { return segment.Index == index }) {
			if pending == nil {
				pending = map[int]string{}
			}
			pending[index] = reason
		}
	}
	return recording, pending
}

// segmentsOf copies a session's finished segments.
func segmentsOf(session *Session) []Segment {
	session.mu.Lock()
	defer session.mu.Unlock()
	return slices.Clone(session.segments)
}

func (s *Session) writeManifest() {
	store.Lock()
	defer store.Unlock()
	s.saveManifest()
}

// saveManifest writes the session's manifest. Hold store.
func (s *Session) saveManifest() {
	directory := s.directory()
	if directory == "" {
		return
	}
	recording, pending := s.recording()
	if recording.Started.IsZero() {
		return
	}
	// A failed write keeps the previous manifest and the next save retries.
	// Nothing reads a running session's list back from disk.
	saveManifest(directory, recording, pending)
}

// enforceBudget deletes the oldest unlocked segments under the capture root
// until the rest fit in budget bytes. The session's own finished segments
// count too: that is loop recording.
func enforceBudget(budget int64, current *Session) {
	store.Lock()
	defer store.Unlock()
	if current != nil {
		current.saveManifest()
	}
	recordings, err := Recordings()
	if err != nil {
		return
	}
	recordings = recoverInterrupted(recordings)
	if budget <= 0 {
		return
	}
	if current != nil {
		// The session's own list is the truth; its manifest can be behind.
		for i := range recordings {
			if recordings[i].ID == current.id {
				recordings[i].Segments = segmentsOf(current)
			}
		}
	}
	type candidate struct {
		recording int
		segment   Segment
		bytes     int64
	}
	var candidates []candidate
	var total int64
	for i, recording := range recordings {
		for _, segment := range recording.Segments {
			if !segment.Locked() {
				bytes := segment.diskBytes()
				candidates = append(candidates, candidate{i, segment, bytes})
				total += bytes
			}
		}
	}
	if total <= budget {
		return
	}
	// Oldest recording first, then oldest segment.
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := recordings[candidates[i].recording], recordings[candidates[j].recording]
		if !a.Started.Equal(b.Started) {
			return a.Started.Before(b.Started)
		}
		return candidates[i].segment.Index < candidates[j].segment.Index
	})
	deleted := map[int][]int{}
	for _, next := range candidates {
		if total <= budget {
			break
		}
		recording := &recordings[next.recording]
		if removeSegmentFiles(recording.dir, next.segment.Index) != nil {
			// Still on disk, for example open in a player: try the next one.
			continue
		}
		recording.Segments = slices.DeleteFunc(recording.Segments, func(s Segment) bool { return s.Index == next.segment.Index })
		total -= next.bytes
		deleted[next.recording] = append(deleted[next.recording], next.segment.Index)
	}
	for i, indices := range deleted {
		recording := recordings[i]
		if current != nil && recording.ID == current.id {
			// Drop only what was deleted: the list on disk can be behind
			// the session's own.
			current.mu.Lock()
			current.segments = slices.DeleteFunc(current.segments, func(s Segment) bool { return slices.Contains(indices, s.Index) })
			current.mu.Unlock()
			current.saveManifest()
			continue
		}
		if len(recording.Segments) == 0 && !recording.State.Active() {
			os.RemoveAll(recording.dir)
			continue
		}
		_, pending, _ := readManifest(recording.dir)
		saveManifest(recording.dir, recording, pending)
	}
	changed()
}

// recoverInterrupted settles recordings whose process died and saves them as
// stopped. A segment file the manifest does not list is kept when it has its
// index, since the muxer finished it just before the process died, and
// deleted when it does not: it was being written and cannot play. A
// recording left with no segments is removed. Hold store.
func recoverInterrupted(recordings []Recording) []Recording {
	kept := recordings[:0]
	for _, recording := range recordings {
		if recording.Reason != "interrupted" || !recordingRunning(recording.dir) {
			kept = append(kept, recording)
			continue
		}
		_, pending, _ := readManifest(recording.dir)
		listed := func(index int) bool {
			return slices.ContainsFunc(recording.Segments, func(s Segment) bool { return s.Index == index })
		}
		if entries, err := os.ReadDir(recording.dir); err == nil {
			for _, entry := range entries {
				index, ok := segmentIndex(entry.Name(), ".mp4")
				if !ok || listed(index) {
					continue
				}
				path := filepath.Join(recording.dir, entry.Name())
				duration, finished := finishedMP4(path)
				info, err := os.Stat(path)
				if !finished || err != nil {
					removeSegmentFiles(recording.dir, index)
					continue
				}
				// The device clock at its start is not on record: the end of
				// the segment before it is close.
				start := recording.Info.Clock
				for _, segment := range recording.Segments {
					if segment.Index < index {
						start = max(start, segment.End)
					}
				}
				recording.Segments = append(recording.Segments, Segment{Index: index, Start: start, End: start + int64(duration),
					Bytes: info.Size(), Lock: pending[index], dir: recording.dir})
			}
			for _, entry := range entries {
				if index, ok := segmentIndex(entry.Name(), ".jsonl"); ok && !listed(index) {
					os.Remove(filepath.Join(recording.dir, entry.Name()))
				}
			}
		}
		if len(recording.Segments) == 0 {
			os.RemoveAll(recording.dir)
			continue
		}
		slices.SortFunc(recording.Segments, func(a, b Segment) int { return a.Index - b.Index })
		if recording.Ended.IsZero() {
			if info, err := os.Stat(filepath.Join(recording.dir, manifestFile)); err == nil {
				recording.Ended = info.ModTime().UTC()
			}
		}
		saveManifest(recording.dir, recording, nil)
		kept = append(kept, recording)
	}
	return kept
}

// segmentIndex parses a segment file name with the given extension.
func segmentIndex(name, extension string) (int, bool) {
	var index int
	if n, _ := fmt.Sscanf(name, "segment-%05d"+extension, &index); n != 1 || index < 1 || name != segmentFile(index, extension) {
		return 0, false
	}
	return index, true
}

// recordingRunning reports whether a manifest on disk still says Running.
func recordingRunning(dir string) bool {
	recording, err := readRecording(dir)
	return err == nil && recording.State.Active()
}
