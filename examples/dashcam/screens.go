package dashcam

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr-native/extension"
	"github.com/DonaldMurillo/gofastr-native/extension/capture"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

const (
	root          = "/dashcam"
	clipsRoute    = "/dashcam/clips"
	settingsRoute = "/dashcam/settings"
)

// app is one dash cam: its settings and whether it already started
// recording on its own in this process.
type app struct {
	settings *store

	mu          sync.Mutex
	autoStarted bool
	edits       uint64
	changed     chan struct{}
}

func (a *app) routes() []nativeui.Route {
	return []nativeui.Route{
		{Path: root, Screen: a.mainScreen, Watch: a.watch, Actions: map[string]nativeui.Handle{
			"start":    a.start,
			"stop":     stop,
			"lock":     lock,
			"clips":    navigate(clipsRoute),
			"settings": navigate(settingsRoute),
		}},
		{Path: clipsRoute, Screen: a.clipsScreen, Watch: a.watch},
		{Path: clipsRoute + "/{id}/{index}", Screen: clipScreen, Actions: map[string]nativeui.Handle{
			"unlock": unlock,
			"delete": deleteClip,
		}},
		{Path: settingsRoute, Screen: a.settingsScreen, Actions: map[string]nativeui.Handle{
			"save": a.save,
		}},
	}
}

func navigate(route string) nativeui.Handle {
	return func(context.Context, nativeui.Request) (nativeui.Result, error) {
		return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "push", Route: route}}, nil
	}
}

func (a *app) version() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return capture.Version() + "." + strconv.FormatUint(a.edits, 10)
}

func (a *app) edited() {
	a.mu.Lock()
	a.edits++
	if a.changed != nil {
		close(a.changed)
		a.changed = nil
	}
	a.mu.Unlock()
}

// watch waits for a capture change or a settings save.
func (a *app) watch(ctx context.Context, _ nativeui.Request, seen string) (string, error) {
	for {
		now := a.version()
		if now != seen {
			return now, nil
		}
		a.mu.Lock()
		if a.changed == nil {
			a.changed = make(chan struct{})
		}
		saved := a.changed
		a.mu.Unlock()
		captured, _, _ := strings.Cut(now, ".")
		waiting, cancel := context.WithCancel(ctx)
		go func() {
			select {
			case <-saved:
				cancel()
			case <-waiting.Done():
			}
		}()
		capture.Watch(waiting, captured)
		cancel()
		if ctx.Err() != nil {
			return seen, ctx.Err()
		}
	}
}

func running() *capture.Session {
	if session := capture.Current(); session != nil && session.State().Active() {
		return session
	}
	return nil
}

// mainScreen shows the camera, what is being recorded and the controls.
// With AutoStart it starts recording the first time it renders in a
// process: the app is in front then, so Android lets the camera start.
func (a *app) mainScreen(context.Context, nativeui.Request) (nativeui.Screen, error) {
	settings := a.settings.get()
	a.mu.Lock()
	auto := settings.AutoStart && !a.autoStarted
	a.autoStarted = true
	a.mu.Unlock()
	if auto && running() == nil {
		capture.Start(settings.Config())
	}
	session := running()
	view := capture.View{Preview: true, Camera: capture.Back, Session: session}
	block, err := capture.Block("camera", view)
	if err != nil {
		return nativeui.Screen{}, err
	}
	screen := nativeui.Screen{
		Title:      "Dash cam",
		Live:       &nativeui.Live{Version: a.version()},
		Components: []extension.Component{block},
	}
	lines := status(capture.Current(), settings)
	if usage := storage(); usage != "" {
		lines = append(lines, usage)
	}
	screen.Text = strings.Join(lines, "\n")
	if session != nil {
		screen.Actions = []nativeui.Action{{ID: "lock", Label: "Lock", Primary: true}, {ID: "stop", Label: "Stop"}}
		return screen, nil
	}
	screen.Actions = []nativeui.Action{
		{ID: "start", Label: "Start", Primary: true},
		{ID: "clips", Label: "Locked clips"},
		{ID: "settings", Label: "Settings"},
	}
	return screen, nil
}

// status describes the session under the preview.
func status(session *capture.Session, settings Settings) []string {
	if session == nil {
		return []string{"Ready. Recording keeps running with the screen off.", settings.Summary()}
	}
	status := session.Status()
	switch status.State {
	case capture.Pending:
		return []string{"Starting the camera."}
	case capture.Stopped:
		return []string{"Stopped.", settings.Summary()}
	case capture.Failed:
		message := status.Message
		if message == "" {
			message = status.Reason
		}
		return []string{"Recording failed: " + message, settings.Summary()}
	}
	elapsed := time.Since(status.Started).Round(time.Second)
	lines := []string{fmt.Sprintf("Recording for %s, file %d. %dx%d.", elapsed, status.Segment, status.Info.Width, status.Info.Height)}
	if status.State == capture.Stopping {
		lines[0] = "Stopping."
	}
	if location := status.Location; location != nil {
		line := fmt.Sprintf("At %.5f, %.5f", location.Latitude, location.Longitude)
		if location.Speed != nil {
			line += fmt.Sprintf(", %.0f km/h", *location.Speed*3.6)
		}
		lines = append(lines, line+".")
	} else if session.Config().Location && !status.Info.Location {
		lines = append(lines, "No location: permission denied or no provider.")
	}
	if orientation := status.Orientation; orientation != nil {
		heading := orientation.Azimuth
		if orientation.Heading != nil {
			heading = *orientation.Heading
		}
		lines = append(lines, "Facing "+compass(heading)+".")
	}
	if session.Config().Impact > 0 && !status.Info.Acceleration {
		lines = append(lines, "Impact lock is off: this phone reports no acceleration.")
	}
	if !status.Locked.IsZero() {
		why := "by hand"
		if status.LockReason == capture.LockImpact && status.Impact != nil {
			why = fmt.Sprintf("after a %.1f g impact", status.Impact.G())
		}
		// Go on Android runs in UTC, so times show their zone.
		lines = append(lines, fmt.Sprintf("Locked at %s %s.", status.Locked.Local().Format("15:04:05 MST"), why))
	}
	return lines
}

func compass(degrees float64) string {
	names := []string{"north", "northeast", "east", "southeast", "south", "southwest", "west", "northwest"}
	return names[int(math.Mod(math.Mod(degrees, 360)+360+22.5, 360)/45)%8]
}

// storage sums what the loop holds and what is locked.
func storage() string {
	recordings, err := capture.Recordings()
	if err != nil {
		return ""
	}
	var loop, locked int64
	clips := 0
	for _, recording := range recordings {
		for _, segment := range recording.Segments {
			if segment.Locked() {
				locked += segment.Bytes
				clips++
			} else {
				loop += segment.Bytes
			}
		}
	}
	switch clips {
	case 0:
		return fmt.Sprintf("Loop: %s. No locked clips.", size(loop))
	case 1:
		return fmt.Sprintf("Loop: %s. 1 locked clip, %s.", size(loop), size(locked))
	}
	return fmt.Sprintf("Loop: %s. %d locked clips, %s.", size(loop), clips, size(locked))
}

func size(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(bytes)/(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(bytes)/(1<<20))
	}
	return fmt.Sprintf("%d KB", bytes>>10)
}

func (a *app) start(context.Context, nativeui.Request) (nativeui.Result, error) {
	if running() != nil {
		return nativeui.Result{}, nil
	}
	if _, err := capture.Start(a.settings.get().Config()); err != nil {
		return nativeui.Result{Announcement: err.Error()}, nil
	}
	return nativeui.Result{}, nil
}

func stop(context.Context, nativeui.Request) (nativeui.Result, error) {
	if session := running(); session != nil {
		session.Stop()
	}
	return nativeui.Result{}, nil
}

func lock(context.Context, nativeui.Request) (nativeui.Result, error) {
	session := running()
	if session == nil {
		return nativeui.Result{}, nil
	}
	session.Lock(capture.LockManual)
	return nativeui.Result{Announcement: "Locked. This file and the ones before and after it are kept."}, nil
}

// clip is one locked segment and the recording it belongs to.
type clip struct {
	recording capture.Recording
	segment   capture.Segment
}

func (c clip) started() time.Time { return c.recording.Info.WallTime(c.segment.Start) }

func lockedClips() ([]clip, error) {
	recordings, err := capture.Recordings()
	if err != nil {
		return nil, err
	}
	var clips []clip
	for _, recording := range recordings {
		for _, segment := range recording.Segments {
			if segment.Locked() {
				clips = append(clips, clip{recording, segment})
			}
		}
	}
	sort.Slice(clips, func(i, j int) bool { return clips[i].started().After(clips[j].started()) })
	return clips, nil
}

func reasonLabel(reason string) string {
	switch reason {
	case capture.LockImpact:
		return "Impact"
	case capture.LockManual:
		return "Locked by hand"
	}
	return reason
}

func (a *app) clipsScreen(context.Context, nativeui.Request) (nativeui.Screen, error) {
	clips, err := lockedClips()
	if err != nil {
		return nativeui.Screen{}, err
	}
	rows := make([]nativeui.Row, 0, len(clips))
	for _, clip := range clips {
		rows = append(rows, nativeui.Row{
			ID:          clip.recording.ID + "-" + strconv.Itoa(clip.segment.Index),
			Title:       clip.started().Local().Format("Jan 2, 15:04:05 MST"),
			Subtitle:    fmt.Sprintf("%s, %s", reasonLabel(clip.segment.Lock), clip.segment.Duration().Round(time.Second)),
			Metadata:    size(clip.segment.Bytes),
			Destination: fmt.Sprintf("%s/%s/%d", clipsRoute, clip.recording.ID, clip.segment.Index),
		})
	}
	return nativeui.Screen{Title: "Locked clips", Live: &nativeui.Live{Version: a.version()}, List: &nativeui.List{
		Rows:   rows,
		Empty:  "No locked clips. Impacts and the Lock button keep the footage around them here.",
		Footer: "Loop recording never deletes locked clips. Unlock or delete them to free space.",
	}}, nil
}

func findClip(request nativeui.Request) (clip, bool, error) {
	index, err := strconv.Atoi(request.Params["index"])
	if err != nil {
		return clip{}, false, nil
	}
	recordings, err := capture.Recordings()
	if err != nil {
		return clip{}, false, err
	}
	for _, recording := range recordings {
		if recording.ID != request.Params["id"] {
			continue
		}
		for _, segment := range recording.Segments {
			if segment.Index == index {
				return clip{recording, segment}, true, nil
			}
		}
	}
	return clip{}, false, nil
}

// peaks reads a clip's first location fix and strongest acceleration from
// its telemetry file.
func peaks(segment capture.Segment) (first *capture.Location, strongest *capture.Acceleration) {
	file, err := segment.OpenTelemetry()
	if err != nil {
		return nil, nil
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var sample capture.Telemetry
		if json.Unmarshal(scanner.Bytes(), &sample) != nil {
			continue
		}
		if sample.Location != nil && first == nil {
			first = sample.Location
		}
		if a := sample.Acceleration; a != nil && (strongest == nil || a.G() > strongest.G()) {
			strongest = a
		}
	}
	return first, strongest
}

func clipScreen(_ context.Context, request nativeui.Request) (nativeui.Screen, error) {
	clip, ok, err := findClip(request)
	if err != nil {
		return nativeui.Screen{}, err
	}
	if !ok {
		return nativeui.Screen{Title: "Clip", Text: "This clip was deleted."}, nil
	}
	lines := []string{
		clip.started().Local().Format("Monday, Jan 2 2006, 15:04:05 MST"),
		fmt.Sprintf("%s, %s, %s.", reasonLabel(clip.segment.Lock), clip.segment.Duration().Round(time.Second), size(clip.segment.Bytes)),
	}
	location, strongest := peaks(clip.segment)
	if location != nil {
		lines = append(lines, fmt.Sprintf("Started at %.5f, %.5f.", location.Latitude, location.Longitude))
	}
	if strongest != nil {
		lines = append(lines, fmt.Sprintf("Strongest jolt %.1f g.", strongest.G()))
	}
	lines = append(lines, fmt.Sprintf("File segment-%05d.mp4 and its telemetry, in recording %s.", clip.segment.Index, clip.recording.ID[:8]))
	return nativeui.Screen{Title: "Clip", Text: strings.Join(lines, "\n"), Actions: []nativeui.Action{
		{ID: "unlock", Label: "Unlock"},
		{ID: "delete", Label: "Delete"},
	}}, nil
}

func unlock(_ context.Context, request nativeui.Request) (nativeui.Result, error) {
	clip, ok, err := findClip(request)
	if err != nil || !ok {
		return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "pop"}}, err
	}
	if err := clip.recording.Lock(clip.segment.Index, ""); err != nil {
		return nativeui.Result{Announcement: err.Error()}, nil
	}
	return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "pop"}, Announcement: "Unlocked. Loop recording may delete it."}, nil
}

func deleteClip(_ context.Context, request nativeui.Request) (nativeui.Result, error) {
	clip, ok, err := findClip(request)
	if err != nil || !ok {
		return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "pop"}}, err
	}
	if err := clip.recording.RemoveSegment(clip.segment.Index); err != nil {
		return nativeui.Result{Announcement: err.Error()}, nil
	}
	return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "pop"}, Announcement: "Clip deleted."}, nil
}

func choice(value, label string) nativeui.Option { return nativeui.Option{Value: value, Label: label} }

// length names a file length in seconds for the form.
func length(value int) string {
	if value == 60 {
		return "1 minute"
	}
	if value%60 == 0 {
		return strconv.Itoa(value/60) + " minutes"
	}
	return strconv.Itoa(value) + " seconds"
}

// including adds the saved value to a select's options when it is not one
// of them, so settings saved by another version of the app still open.
func including(options []nativeui.Option, value, label string) []nativeui.Option {
	for _, option := range options {
		if option.Value == value {
			return options
		}
	}
	return append(options, choice(value, label))
}

func (a *app) settingsScreen(context.Context, nativeui.Request) (nativeui.Screen, error) {
	s := a.settings.get()
	flag := strconv.FormatBool
	return nativeui.Screen{
		Title:   "Settings",
		Actions: []nativeui.Action{{ID: "save", Label: "Save", Primary: true, Commit: true}},
		Form: &nativeui.Form{Fields: []nativeui.Field{
			{Key: "quality", Label: "Quality", Kind: "select", Value: s.Quality, Options: []nativeui.Option{choice("720", "720p"), choice("1080", "1080p")}},
			{Key: "segment", Label: "File length", Kind: "select", Value: strconv.Itoa(s.Segment), Options: including([]nativeui.Option{
				choice("60", "1 minute"), choice("180", "3 minutes"), choice("300", "5 minutes")},
				strconv.Itoa(s.Segment), length(s.Segment))},
			{Key: "budget", Label: "Loop size", Kind: "select", Value: strconv.Itoa(s.Budget), Options: including([]nativeui.Option{
				choice("2048", "2 GB"), choice("4096", "4 GB"), choice("8192", "8 GB"), choice("16384", "16 GB"), choice("32768", "32 GB")},
				strconv.Itoa(s.Budget), megabytes(s.Budget))},
			{Key: "impact", Label: "Impact lock", Kind: "select", Value: s.Impact, Options: []nativeui.Option{
				choice("off", "Off"), choice("low", "Hard impacts (3 g)"), choice("medium", "Impacts (2 g)"), choice("high", "Jolts (1.5 g)")}},
			{Key: "audio", Label: "Record sound", Kind: "toggle", Value: flag(s.Audio)},
			{Key: "location", Label: "Record location", Kind: "toggle", Value: flag(s.Location)},
			{Key: "autoStart", Label: "Start recording when the app opens", Kind: "toggle", Value: flag(s.AutoStart)},
		}},
	}, nil
}

func (a *app) save(_ context.Context, request nativeui.Request) (nativeui.Result, error) {
	s := a.settings.get()
	values := request.Values
	if v, ok := values["quality"]; ok {
		s.Quality = v
	}
	if v, err := strconv.Atoi(values["segment"]); err == nil {
		s.Segment = v
	}
	if v, err := strconv.Atoi(values["budget"]); err == nil {
		s.Budget = v
	}
	if v, ok := values["impact"]; ok {
		s.Impact = v
	}
	for key, field := range map[string]*bool{"audio": &s.Audio, "location": &s.Location, "autoStart": &s.AutoStart} {
		if v, ok := values[key]; ok {
			*field = v == "true"
		}
	}
	if err := a.settings.set(s); err != nil {
		return nativeui.Result{Announcement: "Not saved: " + err.Error()}, nil
	}
	a.edited()
	result := nativeui.Result{Navigation: &nativeui.Navigation{Kind: "pop"}}
	if running() != nil {
		result.Announcement = "Saved. The next recording uses these settings."
	}
	return result, nil
}
