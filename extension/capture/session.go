package capture

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// State is a session's lifecycle position.
type State string

const (
	// Pending: Go started the session; no screen has shown it to the device yet.
	Pending State = "pending"
	// Running: the device reported that capture started.
	Running State = "running"
	// Stopping: Go asked the device to stop and waits for its report.
	Stopping State = "stopping"
	Stopped  State = "stopped"
	Failed   State = "failed"
)

// Active reports whether the session still holds, or will hold, the camera.
func (state State) Active() bool { return state == Pending || state == Running || state == Stopping }

// Info is what the device reported when capture started.
type Info struct {
	// Width and Height are the encoded size, which can differ from the request.
	Width  int `json:"width"`
	Height int `json:"height"`
	// Rotation is the clockwise rotation, in degrees, a player applies to
	// show the picture upright. MP4 recordings carry it; live streams do not.
	Rotation int  `json:"rotation"`
	Audio    bool `json:"audio"`
	// Location and Motion are false when permission or hardware was missing.
	Location     bool `json:"location"`
	Motion       bool `json:"motion"`
	Acceleration bool `json:"acceleration"`
	// Clock and Wall were read together at start. Use WallTime to convert
	// device clock values.
	Clock int64     `json:"clock"`
	Wall  time.Time `json:"wall"`
}

// WallTime converts a device clock value (Android elapsedRealtimeNanos) to
// wall time using the pair read at start.
func (info Info) WallTime(clock int64) time.Time {
	return info.Wall.Add(time.Duration(clock - info.Clock))
}

// Status is a snapshot for screens.
type Status struct {
	ID      string
	State   State
	Reason  string
	Message string
	Started time.Time
	Info    Info
	// Frames and Bytes count encoded video, recorded or streamed, as of the
	// device's last report (about once a second). Dropped counts samples
	// sinks could not keep up with.
	Frames, Bytes, Dropped int64
	Location               *Location
	Orientation            *Orientation
	Acceleration           *Acceleration
	SinkError              string
	// Segment is the index of the segment being recorded, from 1.
	Segment int
	// Locked and LockReason describe the last Lock, and Impact the last
	// acceleration that passed Config.Impact.
	Locked     time.Time
	LockReason string
	Impact     *Acceleration
}

// Session is one capture run. At most one session is active per process.
type Session struct {
	id      string
	config  Config
	created time.Time

	mu        sync.Mutex
	state     State
	finishing bool
	reason    string
	message   string
	started   time.Time
	info      Info
	keyframe  bool
	bitrate   int
	sinks     []*sinkQueue
	sinkError string
	frames    int64
	bytes     int64
	dropped   int64
	location  *Location
	motion    *Orientation
	accel     *Acceleration
	impact    *Acceleration
	sidecar   *os.File
	recorder  *mp4Writer
	lastTick  time.Time
	ended     time.Time

	// segment is the index being recorded and segmentStarted when Go learnt
	// it began. cutting is true from asking for a new segment until one
	// starts; cutDirective sends that request to a device muxer.
	segment        int
	segmentStarted time.Time
	cutting        bool
	cutDirective   bool
	segments       []Segment
	// locks holds lock reasons by segment index, including the segment
	// after the current one.
	locks      map[int]string
	lastLock   time.Time
	lockReason string
}

var hub struct {
	mu      sync.Mutex
	root    string
	current *Session
	version uint64
	changed chan struct{}
}

// sinkCloseTimeout bounds how long a finishing session waits for sinks.
var sinkCloseTimeout = 2 * time.Second

var idPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func validID(id string) bool { return idPattern.MatchString(id) }

// Start registers a session. It reaches the device when a screen shows a
// Block carrying it. A pending session that never reached the device is
// replaced; a running one must be stopped first.
func Start(config Config) (*Session, error) {
	config = config.withDefaults()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, err
	}
	session := &Session{id: hex.EncodeToString(raw[:]), config: config, created: time.Now(), state: Pending, locks: map[int]string{}}
	hub.mu.Lock()
	previous := hub.current
	if previous != nil {
		previous.mu.Lock()
		state := previous.state
		previous.mu.Unlock()
		if state == Running || state == Stopping {
			hub.mu.Unlock()
			return nil, errors.New("capture: a session is already running; stop it first")
		}
	}
	hub.current = session
	hub.mu.Unlock()
	if previous != nil {
		previous.finish(Stopped, "replaced", "")
	}
	changed()
	return session, nil
}

// Current returns the most recent session, which may have ended, or nil.
func Current() *Session {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	return hub.current
}

func lookup(id string) *Session {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.current != nil && hub.current.id == id {
		return hub.current
	}
	return nil
}

func (s *Session) ID() string     { return s.id }
func (s *Session) Config() Config { return s.config }

func (s *Session) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Status returns a snapshot.
func (s *Session) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{ID: s.id, State: s.state, Reason: s.reason, Message: s.message, Started: s.started, Info: s.info,
		Frames: s.frames, Bytes: s.bytes, Dropped: s.dropped, Location: s.location, Orientation: s.motion, Acceleration: s.accel,
		SinkError: s.sinkError, Segment: s.segment, Locked: s.lastLock, LockReason: s.lockReason, Impact: s.impact}
}

// Stop asks the device to stop. A pending session stops at once. A running
// one moves to Stopping and finishes when the device reports it stopped.
func (s *Session) Stop() {
	s.mu.Lock()
	switch s.state {
	case Pending:
		s.mu.Unlock()
		s.finish(Stopped, "requested", "")
		return
	case Running:
		s.state = Stopping
	}
	s.mu.Unlock()
	changed()
}

// RequestKeyframe asks the encoder for an IDR frame, for example after a
// viewer joins or reports loss.
func (s *Session) RequestKeyframe() {
	s.mu.Lock()
	s.keyframe = true
	s.mu.Unlock()
}

// SetBitrate changes the video encoder's target bitrate while running.
func (s *Session) SetBitrate(bitsPerSecond int) {
	if bitsPerSecond < 100_000 || bitsPerSecond > 50_000_000 {
		return
	}
	s.mu.Lock()
	s.bitrate = bitsPerSecond
	s.mu.Unlock()
}

// Lock keeps the segment being recorded, the one before it and the one
// after it from loop recording, giving the reason (LockManual when empty).
// When segments are on and the current one was not locked yet, it also
// ends that segment at the next keyframe, so the footage before the lock is
// on disk within about a second. Impact detection calls it with LockImpact.
func (s *Session) Lock(reason string) {
	if reason == "" {
		reason = LockManual
	}
	store.Lock()
	defer store.Unlock()
	s.mu.Lock()
	if !s.config.Record || s.state != Running || s.segment == 0 {
		s.mu.Unlock()
		return
	}
	current := s.segment
	s.lastLock, s.lockReason = time.Now(), reason
	fresh := s.locks[current] == ""
	added := false
	for _, index := range []int{current - 1, current, current + 1} {
		if index >= 1 && s.locks[index] == "" {
			s.locks[index] = reason
			added = true
		}
	}
	for i := range s.segments {
		if s.segments[i].Lock == "" {
			s.segments[i].Lock = s.locks[s.segments[i].Index]
		}
	}
	if fresh && s.config.Segment > 0 && !s.cutting {
		s.requestCutLocked()
	}
	s.mu.Unlock()
	if !added {
		s.tick()
		return
	}
	s.saveManifest()
	changed()
}

// requestCutLocked asks for a new segment at the next keyframe. Hold mu.
func (s *Session) requestCutLocked() {
	s.cutting = true
	if s.recorder != nil {
		s.keyframe = true
	} else {
		s.cutDirective = true
	}
}

// checkCut asks a device muxer for a new segment once the current one has
// run Config.Segment. Heartbeats call it about once a second.
func (s *Session) checkCut() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.config.Segment > 0 && s.config.Record && s.state == Running && s.recorder == nil && !s.cutting &&
		!s.segmentStarted.IsZero() && time.Since(s.segmentStarted) >= s.config.Segment {
		s.requestCutLocked()
	}
}

// directives tells the device what Go wants next, as a comma-separated list.
func (s *Session) directives() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	if s.state == Stopping || !s.state.Active() {
		out = append(out, "stop")
	}
	if s.keyframe {
		s.keyframe = false
		out = append(out, "keyframe")
	}
	if s.cutDirective && s.state == Running {
		s.cutDirective = false
		out = append(out, "segment")
	}
	if s.bitrate != 0 {
		out = append(out, "bitrate:"+strconv.Itoa(s.bitrate))
		s.bitrate = 0
	}
	return strings.Join(out, ",")
}

// finish moves the session to a final state once and releases sinks. The
// session stays Stopping until its files are closed and listed, so neither a
// new session nor Recording.Remove can reach them while they are open.
func (s *Session) finish(state State, reason, message string) {
	s.mu.Lock()
	if !s.state.Active() || s.finishing {
		s.mu.Unlock()
		return
	}
	s.finishing, s.state = true, Stopping
	sinks, sidecar, recorder, last := s.sinks, s.sidecar, s.recorder, s.segment
	s.sinks, s.sidecar, s.recorder = nil, nil, nil
	s.mu.Unlock()
	// Sinks get a moment to flush and hang up. The caller may be the device's
	// event thread, so a stuck sink must not hold it.
	closed := make(chan struct{})
	go func() {
		for _, sink := range sinks {
			sink.close()
		}
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(sinkCloseTimeout):
		// A sink stuck in a write never reaches Close. Closing it anyway
		// lets it hang up, which usually ends the stuck write.
		for _, sink := range sinks {
			go sink.once.Do(func() { sink.sink.Close() })
		}
	}
	if sidecar != nil {
		sidecar.Close()
	}
	if recorder != nil {
		s.closeSegment(recorder, last)
	}
	s.mu.Lock()
	s.state, s.reason, s.message, s.ended = state, reason, message, time.Now()
	s.mu.Unlock()
	if s.config.Record {
		s.writeManifest()
		enforceBudget(s.config.Budget, s)
	}
	changed()
}

func (s *Session) directory() string {
	hub.mu.Lock()
	root := hub.root
	hub.mu.Unlock()
	if root == "" {
		return ""
	}
	return filepath.Join(root, s.id)
}

// Version identifies the current capture state for nativeui live screens.
func Version() string {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	return "capture-" + strconv.FormatUint(hub.version, 10)
}

// Watch waits until the capture state differs from version, then returns the
// new version. Assign it to a route's Watch with a screen Live version from
// Version. State changes are immediate; telemetry changes at most once a second.
func Watch(ctx context.Context, version string) (string, error) {
	for {
		hub.mu.Lock()
		current := "capture-" + strconv.FormatUint(hub.version, 10)
		if hub.changed == nil {
			hub.changed = make(chan struct{})
		}
		wait := hub.changed
		hub.mu.Unlock()
		if current != version {
			return current, nil
		}
		select {
		case <-ctx.Done():
			return version, ctx.Err()
		case <-wait:
		}
	}
}

func changed() {
	hub.mu.Lock()
	hub.version++
	if hub.changed != nil {
		close(hub.changed)
		hub.changed = nil
	}
	hub.mu.Unlock()
}
