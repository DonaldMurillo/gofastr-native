package capture

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-native/extension"
)

// reset gives a test a fresh capture hub rooted in its own directory.
func reset(t *testing.T) string {
	t.Helper()
	hub.mu.Lock()
	previous := hub.current
	hub.current, hub.root = nil, ""
	hub.mu.Unlock()
	if previous != nil {
		previous.finish(Stopped, "test", "")
	}
	root := t.TempDir()
	if err := Attach(root); err != nil {
		t.Fatal(err)
	}
	return root
}

// started plays the device's start report for session.
func started(t *testing.T, session *Session) {
	t.Helper()
	info := Info{Width: 1280, Height: 720, Rotation: 90, Location: true, Motion: true, Clock: 5_000_000_000, Wall: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	payload, _ := json.Marshal(info)
	if directives := Event(session.ID(), "started", string(payload)); strings.Contains(directives, "stop") {
		t.Fatalf("started got directives %q", directives)
	}
	if session.State() != Running {
		t.Fatalf("state = %s, want running", session.State())
	}
	// Close the sidecar before the temporary directory goes.
	t.Cleanup(func() { session.finish(Stopped, "test", "") })
}

func TestConfigDefaultsAndValidation(t *testing.T) {
	if err := (Config{Record: true}).Validate(); err != nil {
		t.Fatalf("zero values with Record must be valid: %v", err)
	}
	config := Config{Stream: true, Motion: true}.withDefaults()
	if config.Camera != Back || config.Width != 1280 || config.Height != 720 || config.FrameRate != 30 || config.Bitrate != 4_000_000 || config.MotionRate != 30 {
		t.Fatalf("defaults = %+v", config)
	}
	for name, bad := range map[string]Config{
		"no destination": {},
		"camera":         {Record: true, Camera: "side"},
		"size":           {Record: true, Width: 8000, Height: 4000},
		"frame rate":     {Record: true, FrameRate: 240},
		"bitrate":        {Record: true, Bitrate: 10},
		"motion rate":    {Record: true, Motion: true, MotionRate: 500},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("%s: accepted %+v", name, bad)
		}
		if _, err := Start(bad); err == nil {
			t.Errorf("%s: Start accepted %+v", name, bad)
		}
	}
}

func TestDefinitionHasAndroidAndWebViewAdapters(t *testing.T) {
	definition := Definition()
	manifest := definition.Manifest
	if manifest.Name != Name || manifest.Kind != extension.ComponentKind || manifest.Android == nil {
		t.Fatalf("manifest = %+v", manifest)
	}
	if manifest.HasIOS() || !manifest.HasWebView() {
		t.Fatal("capture has Android and WebView adapters and no iOS adapter")
	}
	if got := strings.Join(manifest.WebView.Permissions, ","); got != "camera,geolocation" {
		t.Fatalf("WebView permissions = %s", got)
	}
	if _, err := definition.Assets.Open(manifest.WebView.Script); err != nil {
		t.Fatalf("WebView script is not embedded: %v", err)
	}
	if len(manifest.Android.Bind) != 1 || manifest.Android.Bind[0] != "capturebind" {
		t.Fatalf("bind = %v", manifest.Android.Bind)
	}
	if _, err := extension.New(definition); err != nil {
		t.Fatal(err)
	}
	for _, source := range manifest.Android.Sources {
		if _, err := definition.Assets.Open(source); err != nil {
			t.Errorf("source %s is not embedded: %v", source, err)
		}
	}
}

func TestBlockCarriesActiveSessionOnly(t *testing.T) {
	reset(t)
	preview, err := Block("camera", View{Preview: true})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Payload != `{"preview":true,"camera":"back"}` {
		t.Fatalf("preview payload = %s", preview.Payload)
	}
	session, err := Start(Config{Record: true, Camera: Front, Location: true})
	if err != nil {
		t.Fatal(err)
	}
	block, err := Block("camera", View{Session: session})
	if err != nil {
		t.Fatal(err)
	}
	var wire wireView
	if err := json.Unmarshal([]byte(block.Payload), &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Session == nil || wire.Session.ID != session.ID() || wire.Camera != Front || !wire.Session.Config.Location || wire.Session.Config.Width != 1280 {
		t.Fatalf("session payload = %s", block.Payload)
	}
	if err := validate(block); err != nil {
		t.Fatal(err)
	}
	session.Stop()
	ended, err := Block("camera", View{Session: session})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ended.Payload, "session") {
		t.Fatalf("an ended session must not reach the device again: %s", ended.Payload)
	}
	for name, bad := range map[string]extension.Component{
		"name":         {ID: "camera", Name: "other/v1", Payload: `{"camera":"back"}`},
		"destinations": {ID: "camera", Name: Name, Payload: `{"camera":"back"}`, Destinations: map[string]string{"x": "/x"}},
		"camera":       {ID: "camera", Name: Name, Payload: `{"camera":"up"}`},
		"session id":   {ID: "camera", Name: Name, Payload: `{"camera":"back","session":{"id":"../x","config":{"camera":"back","record":true}}}`},
		"config":       {ID: "camera", Name: Name, Payload: `{"camera":"back","session":{"id":"0123456789abcdef0123456789abcdef","config":{"camera":"back"}}}`},
	} {
		if err := validate(bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestStartReplacesPendingButNotRunning(t *testing.T) {
	reset(t)
	first, err := Start(Config{Record: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Start(Config{Record: true})
	if err != nil {
		t.Fatal(err)
	}
	if first.State() != Stopped || first.Status().Reason != "replaced" || Current() != second {
		t.Fatalf("pending session was not replaced: %s %s", first.State(), first.Status().Reason)
	}
	if Event(first.ID(), "started", `{}`) != "stop" {
		t.Fatal("the device must stop a replaced session")
	}
	started(t, second)
	if _, err := Start(Config{Record: true}); err == nil {
		t.Fatal("Start replaced a running session")
	}
}

// One recorded session from the device's point of view: start, telemetry,
// a stop request answered by the stopped report, then the recording.
func TestRecordedSessionLifecycle(t *testing.T) {
	root := reset(t)
	version := Version()
	session, err := Start(Config{Record: true, Location: true, Motion: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if next, err := Watch(ctx, version); err != nil || next == version {
		t.Fatalf("Watch after Start = %q, %v", next, err)
	}
	started(t, session)
	directory := filepath.Join(root, session.ID())
	if _, err := os.Stat(filepath.Join(directory, manifestFile)); err != nil {
		t.Fatalf("no manifest while running: %v", err)
	}

	Event(session.ID(), "location", `{"clock":6000000000,"time":"2026-01-02T03:04:06Z","provider":"gps","latitude":51.5,"longitude":-0.12,"accuracy":4.5,"altitude":11}`)
	Event(session.ID(), "orientation", `{"clock":6000000001,"azimuth":271.5,"heading":270.25,"pitch":-3,"roll":1.5,"quaternion":[0,0,0.7071,0.7071]}`)
	Event(session.ID(), "rotation", `{"clock":6000000002,"x":0.1,"y":0,"z":-0.2}`)
	Event(session.ID(), "orientation", `not json`)
	if directives := Event(session.ID(), "heartbeat", `{}`); directives != "" {
		t.Fatalf("heartbeat directives = %q", directives)
	}
	if directives := Sample(session.ID(), int(Video), 1, 6_000_000_000, []byte{0, 0, 0, 1, 0x65}); directives != "" {
		t.Fatalf("sample directives = %q", directives)
	}

	status := session.Status()
	if status.Location == nil || status.Location.Latitude != 51.5 || *status.Location.Altitude != 11 {
		t.Fatalf("location = %+v", status.Location)
	}
	if status.Orientation == nil || status.Orientation.Azimuth != 271.5 || *status.Orientation.Heading != 270.25 {
		t.Fatalf("orientation = %+v", status.Orientation)
	}
	if status.Frames != 1 || status.Bytes != 5 {
		t.Fatalf("frames %d bytes %d", status.Frames, status.Bytes)
	}
	if got := status.Info.WallTime(6_000_000_000); !got.Equal(time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC)) {
		t.Fatalf("WallTime = %v", got)
	}

	session.Stop()
	if session.State() != Stopping {
		t.Fatalf("state after Stop = %s", session.State())
	}
	if directives := Event(session.ID(), "heartbeat", `{}`); directives != "stop" {
		t.Fatalf("directives while stopping = %q", directives)
	}
	// The device muxer finishes its file and reports it before stopping.
	if err := os.WriteFile(filepath.Join(directory, segmentFile(1, ".mp4")), []byte("mp4"), 0o600); err != nil {
		t.Fatal(err)
	}
	Event(session.ID(), "segment", `{"index":1,"start":6000000000,"end":7000000000}`)
	Event(session.ID(), "stopped", `{"reason":"requested"}`)
	if session.State() != Stopped || session.Status().Reason != "requested" {
		t.Fatalf("final = %s %s", session.State(), session.Status().Reason)
	}

	file, err := os.Open(filepath.Join(directory, segmentFile(1, ".jsonl")))
	if err != nil {
		t.Fatal(err)
	}
	var lines []Telemetry
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var sample Telemetry
		if err := json.Unmarshal(scanner.Bytes(), &sample); err != nil {
			t.Fatalf("sidecar line %q: %v", scanner.Text(), err)
		}
		lines = append(lines, sample)
	}
	file.Close()
	if len(lines) != 3 || lines[0].Location == nil || lines[1].Orientation == nil || lines[2].Rotation == nil || lines[2].Rotation.Z != -0.2 {
		t.Fatalf("sidecar = %+v", lines)
	}

	recordings, err := Recordings()
	if err != nil || len(recordings) != 1 {
		t.Fatalf("Recordings = %v, %v", recordings, err)
	}
	recording := recordings[0]
	if recording.ID != session.ID() || recording.State != Stopped || recording.Ended.IsZero() || recording.Info.Rotation != 90 || !recording.Complete() {
		t.Fatalf("recording = %+v", recording)
	}
	if len(recording.Segments) != 1 || recording.Segments[0].Bytes != 3 || recording.Segments[0].Duration() != time.Second || recording.Bytes() != 3 {
		t.Fatalf("segments = %+v", recording.Segments)
	}
	if err := recording.Remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("Remove left the directory")
	}
}

func TestRecordingsMarksOrphansAndRefusesActiveRemove(t *testing.T) {
	root := reset(t)
	session, err := Start(Config{Record: true})
	if err != nil {
		t.Fatal(err)
	}
	started(t, session)
	recordings, err := Recordings()
	if err != nil || len(recordings) != 1 || recordings[0].State != Running {
		t.Fatalf("Recordings = %+v, %v", recordings, err)
	}
	if err := recordings[0].Remove(); err == nil {
		t.Fatal("removed an active recording")
	}
	// A new process does not know the session: it was interrupted.
	hub.mu.Lock()
	hub.current = nil
	hub.mu.Unlock()
	recordings, err = Recordings()
	if err != nil || len(recordings) != 1 || recordings[0].State != Stopped || recordings[0].Reason != "interrupted" {
		t.Fatalf("orphan = %+v, %v", recordings, err)
	}
	if err := os.MkdirAll(filepath.Join(root, "not-a-session"), 0o700); err != nil {
		t.Fatal(err)
	}
	if recordings, _ := Recordings(); len(recordings) != 1 {
		t.Fatalf("listed a foreign directory: %+v", recordings)
	}
}

func TestDeviceFailureAndUnknownSessions(t *testing.T) {
	reset(t)
	if Event("0123456789abcdef0123456789abcdef", "location", `{}`) != "stop" {
		t.Fatal("unknown session must be told to stop")
	}
	if Sample("0123456789abcdef0123456789abcdef", 0, 1, 0, nil) != "stop" {
		t.Fatal("unknown session sample must be told to stop")
	}
	session, err := Start(Config{Record: true})
	if err != nil {
		t.Fatal(err)
	}
	Event(session.ID(), "failed", `{"reason":"denied","message":"Camera permission was denied."}`)
	status := session.Status()
	if status.State != Failed || status.Reason != "denied" || status.Message == "" {
		t.Fatalf("status = %+v", status)
	}
	other, _ := Start(Config{Record: true})
	Event(other.ID(), "started", `not json`)
	if other.State() != Failed || other.Status().Reason != "protocol" {
		t.Fatalf("garbled start = %s %s", other.State(), other.Status().Reason)
	}
	if err := Attach("relative/dir"); err == nil {
		t.Fatal("Attach accepted a relative root")
	}
}

func TestDirectives(t *testing.T) {
	reset(t)
	session, _ := Start(Config{Stream: true})
	started(t, session)
	session.RequestKeyframe()
	session.SetBitrate(2_000_000)
	session.SetBitrate(1) // out of range: ignored
	if got := Event(session.ID(), "heartbeat", ""); got != "keyframe,bitrate:2000000" {
		t.Fatalf("directives = %q", got)
	}
	if got := Event(session.ID(), "heartbeat", ""); got != "" {
		t.Fatalf("directives repeat: %q", got)
	}
}

type recordingSink struct {
	mu        sync.Mutex
	media     []Media
	telemetry []Telemetry
	closed    bool
	block     chan struct{}
	fail      error
}

func (s *recordingSink) WriteMedia(media Media) error {
	if s.block != nil {
		<-s.block
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	s.media = append(s.media, media)
	return nil
}

func (s *recordingSink) WriteTelemetry(sample Telemetry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.telemetry = append(s.telemetry, sample)
	return nil
}

func (s *recordingSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func TestSinksStartAtKeyframeAndCloseWithSession(t *testing.T) {
	reset(t)
	session, _ := Start(Config{Stream: true, Motion: true})
	started(t, session)
	sink := &recordingSink{}
	if err := session.AddSink(sink); err != nil {
		t.Fatal(err)
	}
	if got := Event(session.ID(), "heartbeat", ""); got != "keyframe" {
		t.Fatalf("a new sink must request a keyframe, got %q", got)
	}
	Sample(session.ID(), int(Video), 0, 1, []byte{1}) // delta before any keyframe: skipped
	Sample(session.ID(), int(Video), 1, 2, []byte{2})
	Sample(session.ID(), int(Audio), 0, 3, []byte{3})
	Sample(session.ID(), int(Video), 0, 4, []byte{4})
	Sample(session.ID(), 9, 0, 5, []byte{5}) // unknown track: ignored
	Event(session.ID(), "orientation", `{"clock":6,"azimuth":10,"pitch":0,"roll":0,"quaternion":[0,0,0,1]}`)
	session.Stop()
	Event(session.ID(), "stopped", `{}`)

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if !sink.closed {
		t.Fatal("sink not closed when the session ended")
	}
	if len(sink.media) != 3 || sink.media[0].Clock != 2 || !sink.media[0].Key || sink.media[0].Codec != "h264" || sink.media[1].Codec != "opus" || sink.media[2].Key {
		t.Fatalf("media = %+v", sink.media)
	}
	if len(sink.telemetry) != 1 || sink.telemetry[0].Orientation.Azimuth != 10 {
		t.Fatalf("telemetry = %+v", sink.telemetry)
	}
	if err := session.AddSink(&recordingSink{}); err == nil {
		t.Fatal("AddSink accepted an ended session")
	}
}

func TestSlowSinkDropsUntilKeyframe(t *testing.T) {
	reset(t)
	session, _ := Start(Config{Stream: true})
	started(t, session)
	Event(session.ID(), "heartbeat", "")
	sink := &recordingSink{block: make(chan struct{})}
	if err := session.AddSink(sink); err != nil {
		t.Fatal(err)
	}
	Event(session.ID(), "heartbeat", "") // consume the join keyframe request
	Sample(session.ID(), int(Video), 1, 0, []byte{0})
	defer func() {
		select {
		case <-sink.block:
		default:
			close(sink.block)
		}
	}()
	// The first item may already be in WriteMedia; overfill the queue. The
	// device learns of the keyframe request from Sample's return value.
	requested := false
	for i := 1; i <= sinkQueueLength+10; i++ {
		if strings.Contains(Sample(session.ID(), int(Video), 0, int64(i), []byte{1}), "keyframe") {
			requested = true
		}
	}
	if session.Status().Dropped == 0 {
		t.Fatal("overflow was not counted")
	}
	if !requested {
		t.Fatal("overflow must request a keyframe")
	}
	Sample(session.ID(), int(Video), 0, 9999, []byte{2}) // still waiting for a key
	close(sink.block)
	// Let the sink catch up so the recovery keyframe finds room.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		sink.mu.Lock()
		n := len(sink.media)
		sink.mu.Unlock()
		if n == sinkQueueLength+1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	Sample(session.ID(), int(Video), 1, 10000, []byte{3})
	session.Stop()
	Event(session.ID(), "stopped", `{}`)
	sink.mu.Lock()
	defer sink.mu.Unlock()
	last := sink.media[len(sink.media)-1]
	if last.Clock != 10000 || !last.Key {
		t.Fatalf("after overflow the sink resumes at a keyframe, got %+v", last)
	}
	for _, media := range sink.media {
		if media.Clock == 9999 {
			t.Fatal("a delta frame reached the sink before the recovery keyframe")
		}
	}
}

func TestFailingSinkIsDetached(t *testing.T) {
	reset(t)
	session, _ := Start(Config{Stream: true})
	started(t, session)
	sink := &recordingSink{fail: errors.New("peer gone")}
	if err := session.AddSink(sink); err != nil {
		t.Fatal(err)
	}
	Sample(session.ID(), int(Video), 1, 1, []byte{1})
	deadline := time.Now().Add(2 * time.Second)
	for session.Status().SinkError == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if session.Status().SinkError != "peer gone" {
		t.Fatalf("sink error = %q", session.Status().SinkError)
	}
	sink.mu.Lock()
	closed := sink.closed
	sink.mu.Unlock()
	if !closed {
		t.Fatal("failed sink not closed")
	}
	if session.State() != Running {
		t.Fatal("a failed sink must not end the session")
	}
}

func TestWatchHonorsCancellation(t *testing.T) {
	reset(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Watch(ctx, Version()); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestStuckSinkDoesNotHoldTheDevice(t *testing.T) {
	reset(t)
	previous := sinkCloseTimeout
	sinkCloseTimeout = 50 * time.Millisecond
	defer func() { sinkCloseTimeout = previous }()
	session, _ := Start(Config{Stream: true})
	started(t, session)
	sink := &recordingSink{block: make(chan struct{})}
	defer close(sink.block)
	if err := session.AddSink(sink); err != nil {
		t.Fatal(err)
	}
	Sample(session.ID(), int(Video), 1, 1, []byte{1})
	done := make(chan struct{})
	go func() {
		Event(session.ID(), "stopped", `{}`)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a stuck sink held the device's stopped report")
	}
	if session.State() != Stopped {
		t.Fatalf("state = %s", session.State())
	}
	// It still gets Close, so it can hang up and end the stuck write.
	deadline := time.Now().Add(2 * time.Second)
	for {
		sink.mu.Lock()
		closed := sink.closed
		sink.mu.Unlock()
		if closed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a stuck sink was never closed")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A session stays active until its last segment is closed and listed, so a
// new session cannot replace it and Remove cannot delete its files first.
func TestFinishKeepsTheSessionActiveUntilFilesClose(t *testing.T) {
	reset(t)
	previous := sinkCloseTimeout
	sinkCloseTimeout = 200 * time.Millisecond
	defer func() { sinkCloseTimeout = previous }()
	session, _ := Start(Config{Stream: true, Record: true})
	started(t, session)
	sink := &recordingSink{block: make(chan struct{})}
	defer close(sink.block)
	if err := session.AddSink(sink); err != nil {
		t.Fatal(err)
	}
	Sample(session.ID(), int(Video), 1, 1, []byte{1})
	reported := make(chan struct{})
	go func() {
		Event(session.ID(), "stopped", `{}`)
		close(reported)
	}()
	time.Sleep(50 * time.Millisecond)
	if state := session.State(); state != Stopping {
		t.Fatalf("while sinks close, state = %s", state)
	}
	if _, err := Start(Config{Record: true}); err == nil {
		t.Fatal("a new session replaced one still closing its files")
	}
	if err := (Recording{ID: session.ID(), dir: session.directory()}).Remove(); err == nil {
		t.Fatal("removed a recording still closing its files")
	}
	// Let it finish before the temporary directory goes.
	<-reported
}

func TestFinishPublishesFinalManifestBeforeStopped(t *testing.T) {
	for _, withSegment := range []bool{false, true} {
		name := "empty"
		if withSegment {
			name = "recorded"
		}
		t.Run(name, func(t *testing.T) {
			reset(t)
			session, err := Start(Config{Record: true})
			if err != nil {
				t.Fatal(err)
			}
			started(t, session)
			if withSegment {
				writeSegment(t, session, 1, 100)
			}
			session.Stop()
			session.writeManifest()
			store.Lock()
			var unlock sync.Once
			release := func() { unlock.Do(store.Unlock) }
			done := make(chan struct{})
			go func() {
				Event(session.ID(), "stopped", `{"reason":"requested"}`)
				close(done)
			}()
			defer func() { release(); <-done }()
			timer := time.NewTimer(100 * time.Millisecond)
			defer timer.Stop()
		waiting:
			for {
				if status := session.Status(); status.State != Stopping {
					recordings, err := Recordings()
					t.Fatalf("while the manifest writer is blocked: status = %+v, recordings = %+v, %v", status, recordings, err)
				}
				select {
				case <-timer.C:
					break waiting
				default:
					runtime.Gosched()
				}
			}
			release()
			<-done
			status := session.Status()
			recordings, err := Recordings()
			if status.State != Stopped || err != nil || len(recordings) != 1 {
				t.Fatalf("status = %+v, recordings = %+v, %v", status, recordings, err)
			}
			recording := recordings[0]
			if recording.State != status.State || recording.Reason != "requested" || recording.Ended.IsZero() || recording.Complete() != withSegment {
				t.Fatalf("stopped before the manifest was finalized: %+v", recording)
			}
		})
	}
}

func TestFinishReportsFinalManifestFailure(t *testing.T) {
	reset(t)
	session, err := Start(Config{Record: true})
	if err != nil {
		t.Fatal(err)
	}
	started(t, session)
	writeSegment(t, session, 1, 100)
	if err := os.Mkdir(filepath.Join(session.directory(), manifestFile+".tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	Event(session.ID(), "stopped", `{"reason":"requested"}`)
	if status := session.Status(); status.State != Failed || status.Reason != "storage" || status.Message == "" {
		t.Fatalf("manifest write failure was published as a successful stop: %+v", status)
	}
}

func TestHeartbeatReportsRecordedVideoAndRefreshes(t *testing.T) {
	reset(t)
	session, err := Start(Config{Record: true})
	if err != nil {
		t.Fatal(err)
	}
	started(t, session)
	// Recording without streaming hands Go no samples, so progress
	// arrives with the heartbeat.
	session.mu.Lock()
	session.lastTick = time.Time{}
	session.mu.Unlock()
	before := Version()
	Event(session.ID(), "heartbeat", `{"frames":90,"bytes":450000}`)
	if status := session.Status(); status.Frames != 90 || status.Bytes != 450000 {
		t.Fatalf("frames, bytes = %d, %d", status.Frames, status.Bytes)
	}
	if Version() == before {
		t.Fatal("a heartbeat must refresh watching screens")
	}
	// Streamed samples and the heartbeat count the same frames.
	Sample(session.ID(), int(Video), 1, 1, make([]byte, 10))
	Event(session.ID(), "heartbeat", `{}`)
	if status := session.Status(); status.Frames != 91 || status.Bytes != 450010 {
		t.Fatalf("after a sample: frames, bytes = %d, %d", status.Frames, status.Bytes)
	}
}

func TestVideoSamplesRefreshWatchingScreens(t *testing.T) {
	reset(t)
	session, err := Start(Config{Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Stop()
	started(t, session)
	session.mu.Lock()
	session.lastTick = time.Time{}
	session.mu.Unlock()
	before := Version()
	Sample(session.ID(), int(Video), 1, 1, make([]byte, 10))
	after := Version()
	if after == before {
		t.Fatal("a video sample must refresh watching screens when no telemetry arrives")
	}
	Sample(session.ID(), int(Video), 0, 2, make([]byte, 10))
	if Version() != after {
		t.Fatal("samples must refresh watching screens at most once a second")
	}
}
