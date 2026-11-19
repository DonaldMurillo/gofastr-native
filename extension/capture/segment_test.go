package capture

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestSegmentImpactAndBudgetValidation(t *testing.T) {
	config := Config{Record: true, Impact: 2}.withDefaults()
	if !config.Acceleration || config.MotionRate != 30 {
		t.Fatalf("Impact must turn on acceleration at the default rate: %+v", config)
	}
	for name, bad := range map[string]Config{
		"impact":  {Record: true, Impact: 0.1},
		"segment": {Record: true, Segment: time.Second},
		"long":    {Record: true, Segment: 2 * time.Hour},
		"budget":  {Record: true, Budget: -1},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("%s: accepted %+v", name, bad)
		}
	}
	if err := (Config{Record: true, Segment: time.Minute, Budget: 1 << 30, Impact: 2.5}).Validate(); err != nil {
		t.Fatal(err)
	}
}

// writeSegment plays a device muxer finishing segment index with size bytes.
func writeSegment(t *testing.T, session *Session, index, size int) {
	t.Helper()
	path := filepath.Join(session.directory(), segmentFile(index, ".mp4"))
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	start := int64(index) * 10_000_000_000
	payload, _ := json.Marshal(map[string]int64{"index": int64(index), "start": start, "end": start + 9_000_000_000})
	Event(session.ID(), "segment", string(payload))
}

func indexes(segments []Segment) []int {
	var out []int
	for _, segment := range segments {
		out = append(out, segment.Index)
	}
	return out
}

func manifest(t *testing.T, session *Session) Recording {
	t.Helper()
	recording, err := readRecording(session.directory())
	if err != nil {
		t.Fatal(err)
	}
	return recording
}

// A device muxer's segments: Go asks for a cut once a segment has run its
// length, records what the device reports, locks around an impact and
// deletes the oldest unlocked segments past the budget.
func TestDeviceSegmentsLockAndLoop(t *testing.T) {
	reset(t)
	session, err := Start(Config{Record: true, Segment: 5 * time.Second, Budget: 2500, Impact: 2})
	if err != nil {
		t.Fatal(err)
	}
	started(t, session)
	if got := session.Status().Segment; got != 1 {
		t.Fatalf("segment = %d", got)
	}
	if directives := Event(session.ID(), "heartbeat", `{}`); directives != "" {
		t.Fatalf("early heartbeat = %q", directives)
	}
	session.mu.Lock()
	session.segmentStarted = time.Now().Add(-6 * time.Second)
	session.mu.Unlock()
	if directives := Event(session.ID(), "heartbeat", `{}`); directives != "segment" {
		t.Fatalf("due heartbeat = %q", directives)
	}
	if directives := Event(session.ID(), "heartbeat", `{}`); directives != "" {
		t.Fatalf("a pending cut must not repeat: %q", directives)
	}
	writeSegment(t, session, 1, 1000)
	Event(session.ID(), "location", `{"clock":15000000000,"latitude":1,"longitude":2,"accuracy":3}`)
	if content, err := os.ReadFile(filepath.Join(session.directory(), segmentFile(2, ".jsonl"))); err != nil || !strings.Contains(string(content), `"latitude":1`) {
		t.Fatalf("telemetry after a cut goes to the next sidecar: %q %v", content, err)
	}
	writeSegment(t, session, 2, 1000)
	writeSegment(t, session, 3, 1000)
	// 3000 bytes and a sidecar against a 2500 byte budget: segment 1 goes.
	if got := indexes(segmentsOf(session)); !slices.Equal(got, []int{2, 3}) {
		t.Fatalf("after the budget: %v", got)
	}
	if _, err := os.Stat(filepath.Join(session.directory(), segmentFile(1, ".mp4"))); !os.IsNotExist(err) {
		t.Fatal("loop recording left the oldest segment's file")
	}
	if _, err := os.Stat(filepath.Join(session.directory(), segmentFile(1, ".jsonl"))); !os.IsNotExist(err) {
		t.Fatal("loop recording left the oldest segment's sidecar")
	}

	// 25 m/s² is 2.5 g: segments 3, 4 (current) and 5 lock, and 4 ends now.
	if directives := Event(session.ID(), "acceleration", `{"clock":41000000000,"x":0,"y":25,"z":0}`); directives != "segment" {
		t.Fatalf("an impact must cut the current segment: %q", directives)
	}
	status := session.Status()
	if status.Impact == nil || status.Impact.Y != 25 || status.LockReason != LockImpact || status.Locked.IsZero() {
		t.Fatalf("status after impact = %+v", status)
	}
	if got := manifest(t, session).Segments; got[1].Index != 3 || got[1].Lock != LockImpact || got[0].Locked() {
		t.Fatalf("finished segments after impact = %+v", got)
	}
	// A second impact in a locked segment neither cuts nor writes.
	if directives := Event(session.ID(), "acceleration", `{"clock":41100000000,"x":0,"y":30,"z":0}`); directives != "" {
		t.Fatalf("second impact = %q", directives)
	}
	if directives := Event(session.ID(), "acceleration", `{"clock":41200000000,"x":0,"y":5,"z":0}`); directives != "" {
		t.Fatalf("half a g = %q", directives)
	}
	writeSegment(t, session, 4, 1000)
	writeSegment(t, session, 5, 1000)
	writeSegment(t, session, 6, 1000)
	writeSegment(t, session, 7, 1000)
	// Unlocked: 2, 6, 7 = 3000 bytes and a sidecar; 2 goes. Locked 3, 4, 5 stay.
	recording := manifest(t, session)
	if got := indexes(recording.Segments); !slices.Equal(got, []int{3, 4, 5, 6, 7}) {
		t.Fatalf("segments = %v", got)
	}
	for _, segment := range recording.Segments {
		if segment.Locked() != (segment.Index <= 5) {
			t.Fatalf("segment %d lock %q", segment.Index, segment.Lock)
		}
	}

	// The notification's Lock action, then an unlock from the app.
	Event(session.ID(), "lock", `{"reason":""}`)
	if status := session.Status(); status.LockReason != LockManual {
		t.Fatalf("manual lock = %+v", status)
	}
	if got := manifest(t, session).Segments; !got[len(got)-1].Locked() {
		t.Fatalf("manual lock must lock the previous segment: %+v", got)
	}
	recordings, err := Recordings()
	if err != nil || len(recordings) != 1 {
		t.Fatal(recordings, err)
	}
	if err := recordings[0].Lock(3, ""); err != nil {
		t.Fatal(err)
	}
	if err := recordings[0].Lock(99, LockManual); err == nil {
		t.Fatal("locked a segment that does not exist")
	}
	if got := segmentsOf(session)[0]; got.Index != 3 || got.Locked() {
		t.Fatalf("unlock while running = %+v", got)
	}

	session.Stop()
	Event(session.ID(), "stopped", `{"reason":"requested"}`)
	recordings, err = Recordings()
	if err != nil || len(recordings) != 1 || !recordings[0].Complete() {
		t.Fatal(recordings, err)
	}
	if err := recordings[0].RemoveSegment(4); err != nil {
		t.Fatal(err)
	}
	if got := indexes(manifest(t, session).Segments); slices.Contains(got, 4) {
		t.Fatalf("RemoveSegment kept 4: %v", got)
	}
}

// oldRecording writes a finished recording's manifest and segment files.
func oldRecording(t *testing.T, root, id string, started time.Time, locks ...string) {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	recording := Recording{ID: id, Config: Config{Record: true}, Started: started, Ended: started.Add(time.Minute), State: Stopped}
	for i, lock := range locks {
		recording.Segments = append(recording.Segments, Segment{Index: i + 1, Bytes: 100, Lock: lock})
		if err := os.WriteFile(filepath.Join(dir, segmentFile(i+1, ".mp4")), make([]byte, 100), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := saveManifest(dir, recording, nil); err != nil {
		t.Fatal(err)
	}
}

func TestBudgetDeletesTheOldestRecordingsFirst(t *testing.T) {
	root := reset(t)
	week := time.Now().Add(-7 * 24 * time.Hour)
	const unlockedOnly, mixed = "0123456789abcdef0123456789abcd01", "0123456789abcdef0123456789abcd02"
	oldRecording(t, root, unlockedOnly, week, "", "")
	oldRecording(t, root, mixed, week.Add(time.Hour), "", LockImpact, "")
	session, err := Start(Config{Record: true, Segment: time.Minute, Budget: 250})
	if err != nil {
		t.Fatal(err)
	}
	// Starting applies the budget: 400 unlocked bytes, so the oldest
	// recording goes whole.
	started(t, session)
	if _, err := os.Stat(filepath.Join(root, unlockedOnly)); !os.IsNotExist(err) {
		t.Fatal("an emptied recording must be removed")
	}
	recordings, err := Recordings()
	if err != nil || len(recordings) != 2 {
		t.Fatal(recordings, err)
	}
	if got := indexes(recordings[1].Segments); recordings[1].ID != mixed || !slices.Equal(got, []int{1, 2, 3}) {
		t.Fatalf("mixed recording = %s %v", recordings[1].ID, got)
	}
	writeSegment(t, session, 1, 100)
	writeSegment(t, session, 2, 100)
	// Each new segment pushes out the oldest unlocked one: mixed 1, then 3.
	// The locked segment stays.
	recordings, _ = Recordings()
	if got := indexes(recordings[1].Segments); !slices.Equal(got, []int{2}) {
		t.Fatalf("mixed recording = %v", got)
	}
	if got := indexes(recordings[0].Segments); !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("session = %v", got)
	}
}

// A process killed mid-recording leaves its manifest Running and the
// segment it was writing on disk, unlisted and without an index. Attach
// deletes that file and settles the recording as interrupted.
func TestAttachSettlesInterruptedRecordings(t *testing.T) {
	root := reset(t)
	const killed, empty = "0123456789abcdef0123456789abcd03", "0123456789abcdef0123456789abcd04"
	oldRecording(t, root, killed, time.Now().Add(-time.Hour), "", LockImpact)
	oldRecording(t, root, empty, time.Now().Add(-2*time.Hour))
	for _, id := range []string{killed, empty} {
		dir := filepath.Join(root, id)
		recording, err := readRecording(dir)
		if err != nil {
			t.Fatal(err)
		}
		recording.State, recording.Ended = Running, time.Time{}
		if err := saveManifest(dir, recording, nil); err != nil {
			t.Fatal(err)
		}
		next := len(recording.Segments) + 1
		for _, extension := range []string{".mp4", ".jsonl"} {
			if err := os.WriteFile(filepath.Join(dir, segmentFile(next, extension)), []byte("partial"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := Attach(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, empty)); !os.IsNotExist(err) {
		t.Fatal("an interrupted recording with no finished segment must go")
	}
	dir := filepath.Join(root, killed)
	for _, extension := range []string{".mp4", ".jsonl"} {
		if _, err := os.Stat(filepath.Join(dir, segmentFile(3, extension))); !os.IsNotExist(err) {
			t.Fatalf("the unfinished %s stayed", extension)
		}
	}
	recording, err := readRecording(dir)
	if err != nil {
		t.Fatal(err)
	}
	if recording.State != Stopped || recording.Reason != "interrupted" || recording.Ended.IsZero() || !slices.Equal(indexes(recording.Segments), []int{1, 2}) {
		t.Fatalf("settled recording = %+v", recording)
	}
	if !recording.Complete() || recording.Segments[1].Lock != LockImpact {
		t.Fatalf("finished segments and locks must survive: %+v", recording.Segments)
	}
}

// The WebView adapter has no muxer: Go cuts its own MP4 at the first
// keyframe once a segment has run its length, asking for one if needed.
func TestGoRecorderCutsAtKeyframes(t *testing.T) {
	reset(t)
	session, err := Start(Config{Record: true, Segment: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	const second = int64(1e9)
	code, _ := post(t, report(t, session.ID(), event("started", map[string]any{"width": 64, "height": 48, "clock": 0,
		"wall": "2026-01-02T03:04:05Z", "recorder": "go"})))
	if code != 200 {
		t.Fatal(code)
	}
	frames := tinyH264(t)
	for _, step := range []struct {
		frame      int
		key        bool
		clock      int64
		directives string
	}{
		{0, true, 0, ""},
		{1, false, 1 * second, ""},
		{1, false, 5*second + second/2, "keyframe"},
		{2, true, 6 * second, ""},
		{3, false, 7 * second, ""},
	} {
		if got := Sample(session.ID(), int(Video), map[bool]int{true: 1}[step.key], step.clock, frames[step.frame]); got != step.directives {
			t.Fatalf("frame at %v: directives %q, want %q", time.Duration(step.clock), got, step.directives)
		}
	}
	Event(session.ID(), "location", `{"clock":7000000000,"latitude":1,"longitude":2,"accuracy":3}`)
	session.Stop()
	Event(session.ID(), "stopped", `{"reason":"requested"}`)
	recordings, err := Recordings()
	if err != nil || len(recordings) != 1 || len(recordings[0].Segments) != 2 {
		t.Fatalf("recordings = %+v, %v", recordings, err)
	}
	first, second2 := recordings[0].Segments[0], recordings[0].Segments[1]
	if first.Start != 0 || first.End != 5*second+second/2 || second2.Start != 6*second || second2.End != 7*second {
		t.Fatalf("segments = %+v", recordings[0].Segments)
	}
	for _, segment := range recordings[0].Segments {
		file, err := segment.OpenVideo()
		if err != nil {
			t.Fatal(err)
		}
		info, _ := file.Stat()
		content := make([]byte, info.Size())
		file.Read(content)
		file.Close()
		if boxes := mp4Boxes(t, content); len(boxes["moov/trak"]) != 1 {
			t.Fatalf("segment %d boxes %v", segment.Index, keys(boxes))
		}
	}
	telemetry, err := second2.OpenTelemetry()
	if err != nil {
		t.Fatal(err)
	}
	defer telemetry.Close()
	if info, _ := telemetry.Stat(); info.Size() == 0 {
		t.Fatal("telemetry after the cut must go to the second segment")
	}
}

// finishedFile is the smallest MP4 finishedMP4 accepts: an index giving a
// 9 second movie.
func finishedFile() []byte {
	mvhd := fullBox("mvhd", 0, 0, u32(0), u32(0), u32(1000), u32(9000))
	return append(box("ftyp", []byte("isom")), box("moov", mvhd)...)
}

// A process killed after the muxer finished a segment but before the
// manifest listed it: recovery keeps that file and the lock the session held
// for it, and still deletes the one being written.
func TestRecoveryKeepsFinishedUnlistedSegments(t *testing.T) {
	root := reset(t)
	const killed = "0123456789abcdef0123456789abcd05"
	oldRecording(t, root, killed, time.Now().Add(-time.Hour), "", "")
	dir := filepath.Join(root, killed)
	recording, err := readRecording(dir)
	if err != nil {
		t.Fatal(err)
	}
	recording.State, recording.Ended = Running, time.Time{}
	if err := saveManifest(dir, recording, map[int]string{3: LockImpact, 4: LockImpact}); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{
		segmentFile(3, ".mp4"): finishedFile(), segmentFile(3, ".jsonl"): []byte("{}\n"),
		segmentFile(4, ".mp4"): append(box("ftyp", []byte("isom")), u32(1)...), segmentFile(4, ".jsonl"): []byte("{}\n"),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := Attach(root); err != nil {
		t.Fatal(err)
	}
	recording, err = readRecording(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := indexes(recording.Segments); !slices.Equal(got, []int{1, 2, 3}) {
		t.Fatalf("segments = %v", got)
	}
	adopted := recording.Segments[2]
	if adopted.Lock != LockImpact || adopted.Duration() != 9*time.Second || adopted.Start != recording.Segments[1].End {
		t.Fatalf("adopted segment = %+v", adopted)
	}
	for _, name := range []string{segmentFile(4, ".mp4"), segmentFile(4, ".jsonl")} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("the unfinished %s stayed", name)
		}
	}
}

// A device muxer that could not finish a segment reports it failed: Go
// deletes the file and keeps cutting, instead of waiting for a report that
// never comes.
func TestFailedDeviceSegmentKeepsCutting(t *testing.T) {
	reset(t)
	session, err := Start(Config{Record: true, Segment: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	started(t, session)
	due := func() string {
		session.mu.Lock()
		session.segmentStarted = time.Now().Add(-6 * time.Second)
		session.mu.Unlock()
		return Event(session.ID(), "heartbeat", `{}`)
	}
	if got := due(); got != "segment" {
		t.Fatalf("due heartbeat = %q", got)
	}
	path := filepath.Join(session.directory(), segmentFile(1, ".mp4"))
	os.WriteFile(path, []byte("broken"), 0o600)
	Event(session.ID(), "segment", `{"index":1,"failed":true}`)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the failed segment's file stayed")
	}
	if status := session.Status(); status.Segment != 2 || len(segmentsOf(session)) != 0 {
		t.Fatalf("after a failed segment: %+v %v", status, segmentsOf(session))
	}
	if got := due(); got != "segment" {
		t.Fatalf("the next cut = %q", got)
	}
}

// A manifest that cannot be written falls behind the session. Loop
// recording must not take the session's list from it.
func TestStaleManifestKeepsSegments(t *testing.T) {
	reset(t)
	session, err := Start(Config{Record: true, Segment: 5 * time.Second, Budget: 2500})
	if err != nil {
		t.Fatal(err)
	}
	started(t, session)
	writeSegment(t, session, 1, 1000)
	// A directory where the temporary manifest goes makes every save fail.
	if err := os.Mkdir(filepath.Join(session.directory(), manifestFile+".tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeSegment(t, session, 2, 1000)
	writeSegment(t, session, 3, 1000)
	if got := indexes(segmentsOf(session)); !slices.Equal(got, []int{2, 3}) {
		t.Fatalf("segments with a stale manifest = %v", got)
	}
}

// A Go-written segment whose index cannot be written does not play: it is
// deleted, not listed as complete.
func TestGoRecorderDropsAnUnfinishedSegment(t *testing.T) {
	reset(t)
	session, err := Start(Config{Record: true})
	if err != nil {
		t.Fatal(err)
	}
	code, _ := post(t, report(t, session.ID(), event("started", map[string]any{"width": 64, "height": 48, "clock": 0,
		"wall": "2026-01-02T03:04:05Z", "recorder": "go"})))
	if code != 200 {
		t.Fatal(code)
	}
	Sample(session.ID(), int(Video), 1, 0, tinyH264(t)[0])
	session.mu.Lock()
	path := session.recorder.file.Name()
	// Writing the index now fails, as on a full disk.
	session.recorder.file.Close()
	session.mu.Unlock()
	session.Stop()
	Event(session.ID(), "stopped", `{"reason":"requested"}`)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the segment without an index stayed")
	}
	if recordings, _ := Recordings(); len(recordings) == 1 && len(recordings[0].Segments) != 0 {
		t.Fatalf("listed %+v", recordings[0].Segments)
	}
}

// A Go recording that cannot create its next file fails the session instead
// of writing on past the segment length, and keeps the file it had.
func TestGoRecorderFailsWhenTheNextFileCannotBeCreated(t *testing.T) {
	reset(t)
	session, err := Start(Config{Record: true, Segment: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	code, _ := post(t, report(t, session.ID(), event("started", map[string]any{"width": 64, "height": 48, "clock": 0,
		"wall": "2026-01-02T03:04:05Z", "recorder": "go"})))
	if code != 200 {
		t.Fatal(code)
	}
	const second = int64(1e9)
	frames := tinyH264(t)
	Sample(session.ID(), int(Video), 1, 0, frames[0])
	// A directory where the next file goes makes creating it fail.
	if err := os.Mkdir(filepath.Join(session.directory(), segmentFile(2, ".mp4")), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := Sample(session.ID(), int(Video), 1, 6*second, frames[2]); got != "stop" {
		t.Fatalf("directives %q, want stop", got)
	}
	if status := session.Status(); status.State != Failed || status.Reason != "storage" {
		t.Fatalf("status = %s %q", status.State, status.Reason)
	}
	recordings, err := Recordings()
	if err != nil || len(recordings) != 1 || len(recordings[0].Segments) != 1 || recordings[0].Segments[0].Index != 1 {
		t.Fatalf("recordings = %+v, %v", recordings, err)
	}
}

// A failed write fails the session with "storage". The file still gets its
// index, so the samples before the failure play.
func TestGoRecorderWriteErrorKeepsEarlierSamples(t *testing.T) {
	reset(t)
	session, err := Start(Config{Record: true})
	if err != nil {
		t.Fatal(err)
	}
	code, _ := post(t, report(t, session.ID(), event("started", map[string]any{"width": 64, "height": 48, "clock": 0,
		"wall": "2026-01-02T03:04:05Z", "recorder": "go"})))
	if code != 200 {
		t.Fatal(code)
	}
	frames := tinyH264(t)
	Sample(session.ID(), int(Video), 1, 0, frames[0])
	Sample(session.ID(), int(Video), 0, 1e9, frames[1])
	session.mu.Lock()
	recorder := session.recorder
	session.mu.Unlock()
	recorder.mu.Lock()
	recorder.err = errors.New("no space left on device")
	recorder.mu.Unlock()
	if got := Sample(session.ID(), int(Video), 0, 2e9, frames[1]); got != "stop" {
		t.Fatalf("directives %q, want stop", got)
	}
	if status := session.Status(); status.State != Failed || status.Reason != "storage" {
		t.Fatalf("status = %s %q", status.State, status.Reason)
	}
	recordings, err := Recordings()
	if err != nil || len(recordings) != 1 || len(recordings[0].Segments) != 1 {
		t.Fatalf("recordings = %+v, %v", recordings, err)
	}
	segment := recordings[0].Segments[0]
	if segment.Start != 0 || segment.End != 1e9 {
		t.Fatalf("segment = %+v", segment)
	}
	content, err := os.ReadFile(filepath.Join(session.directory(), segmentFile(1, ".mp4")))
	if err != nil {
		t.Fatal(err)
	}
	if boxes := mp4Boxes(t, content); len(boxes["moov/trak"]) != 1 {
		t.Fatalf("boxes %v", keys(boxes))
	}
}
