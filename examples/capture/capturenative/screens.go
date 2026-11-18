package capturenative

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr-native/extension"
	"github.com/DonaldMurillo/gofastr-native/extension/capture"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

const (
	root          = "/capture"
	settingsRoute = "/capture/settings"
	recordings    = "/capture/recordings"
)

// settings are the last values submitted from the capture form.
var settings = struct {
	sync.Mutex
	values map[string]string
}{values: map[string]string{
	"camera": "back", "quality": "720", "record": "true", "stream": "false",
	"audio": "true", "location": "true", "motion": "true", "endpoint": "", "token": "",
}}

// routes builds the app. background says whether capture survives the app
// leaving the foreground, which changes what the capture screen promises.
func routes(background bool) []nativeui.Route {
	screen := func(ctx context.Context, request nativeui.Request) (nativeui.Screen, error) {
		return captureScreen(ctx, request, background)
	}
	return []nativeui.Route{
		{Path: root, Screen: screen, Watch: watch, Actions: map[string]nativeui.Handle{
			"start":      start,
			"stop":       stop,
			"settings":   navigate(settingsRoute),
			"recordings": navigate(recordings),
		}},
		{Path: settingsRoute, Screen: settingsScreen, Actions: map[string]nativeui.Handle{
			"save": save,
		}},
		{Path: recordings, Screen: recordingsScreen, Watch: watch},
		{Path: recordings + "/{id}", Screen: recordingScreen, Actions: map[string]nativeui.Handle{
			"remove": remove,
		}},
	}
}

func navigate(route string) nativeui.Handle {
	return func(context.Context, nativeui.Request) (nativeui.Result, error) {
		return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "push", Route: route}}, nil
	}
}

// edits counts settings saves, so live screens refresh after one.
var edits struct {
	sync.Mutex
	count   uint64
	changed chan struct{}
}

func version() string {
	edits.Lock()
	defer edits.Unlock()
	return capture.Version() + "." + strconv.FormatUint(edits.count, 10)
}

func edited() {
	edits.Lock()
	edits.count++
	if edits.changed != nil {
		close(edits.changed)
		edits.changed = nil
	}
	edits.Unlock()
}

// watch waits for a capture state change or a settings save. Screens with
// a form never refresh live, so capture screens carry none.
func watch(ctx context.Context, _ nativeui.Request, seen string) (string, error) {
	for {
		now := version()
		if now != seen {
			return now, nil
		}
		edits.Lock()
		if edits.changed == nil {
			edits.changed = make(chan struct{})
		}
		saved := edits.changed
		edits.Unlock()
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

func captureScreen(_ context.Context, _ nativeui.Request, background bool) (nativeui.Screen, error) {
	session := capture.Current()
	active := session != nil && session.State().Active()
	values := snapshot()
	view := capture.View{Preview: true, Camera: capture.Facing(values["camera"])}
	if active {
		view.Session = session
	}
	block, err := capture.Block("camera", view)
	if err != nil {
		return nativeui.Screen{}, err
	}
	screen := nativeui.Screen{
		Title:      "Capture",
		Live:       &nativeui.Live{Version: version()},
		Components: []extension.Component{block},
		Text:       describe(session, background),
	}
	if active {
		screen.Actions = []nativeui.Action{{ID: "stop", Label: "Stop", Primary: true}}
		return screen, nil
	}
	// Renderers build Prompt once per screen, so text that changes after a
	// settings save goes in Text.
	screen.Text = summary(values) + "\n" + screen.Text
	screen.Actions = []nativeui.Action{
		{ID: "start", Label: "Start", Primary: true},
		{ID: "settings", Label: "Settings"},
		{ID: "recordings", Label: "Recordings"},
	}
	return screen, nil
}

// summary says what Start will do.
func summary(values map[string]string) string {
	config := configure(values)
	what := "Nothing to capture: turn on recording or streaming in Settings"
	switch {
	case config.Record && config.Stream:
		what = "Record and stream"
	case config.Stream:
		what = "Stream"
	case config.Record:
		what = "Record"
	}
	text := fmt.Sprintf("%s %sp from the %s camera", what, values["quality"], values["camera"])
	var with []string
	if config.Audio {
		with = append(with, "sound")
	}
	if config.Location {
		with = append(with, "location")
	}
	if config.Motion {
		with = append(with, "camera direction")
	}
	if len(with) > 0 {
		text += " with " + strings.Join(with, ", ")
	}
	return text + "."
}

func settingsScreen(context.Context, nativeui.Request) (nativeui.Screen, error) {
	values := snapshot()
	return nativeui.Screen{
		Title:   "Settings",
		Actions: []nativeui.Action{{ID: "save", Label: "Save", Primary: true, Commit: true}},
		Form: &nativeui.Form{Fields: []nativeui.Field{
			{Key: "camera", Label: "Camera", Kind: "select", Value: values["camera"], Options: []nativeui.Option{{Value: "back", Label: "Back"}, {Value: "front", Label: "Front"}}},
			{Key: "quality", Label: "Quality", Kind: "select", Value: values["quality"], Options: []nativeui.Option{{Value: "480", Label: "480p"}, {Value: "720", Label: "720p"}, {Value: "1080", Label: "1080p"}}},
			{Key: "record", Label: "Record to this phone", Kind: "toggle", Value: values["record"]},
			{Key: "stream", Label: "Stream live (WHIP)", Kind: "toggle", Value: values["stream"]},
			{Key: "endpoint", Label: "WHIP endpoint", Kind: "text", Placeholder: "https://example.com/whip/live", Value: values["endpoint"]},
			{Key: "token", Label: "Bearer token", Kind: "text", Value: values["token"]},
			{Key: "audio", Label: "Microphone", Kind: "toggle", Value: values["audio"]},
			{Key: "location", Label: "Location", Kind: "toggle", Value: values["location"]},
			{Key: "motion", Label: "Camera direction", Kind: "toggle", Value: values["motion"]},
		}},
	}, nil
}

func save(_ context.Context, request nativeui.Request) (nativeui.Result, error) {
	settings.Lock()
	for key := range settings.values {
		if submitted, ok := request.Values[key]; ok {
			settings.values[key] = strings.TrimSpace(submitted)
		}
	}
	settings.Unlock()
	edited()
	return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "pop"}}, nil
}

func snapshot() map[string]string {
	settings.Lock()
	defer settings.Unlock()
	values := make(map[string]string, len(settings.values))
	for key, v := range settings.values {
		values[key] = v
	}
	return values
}

// configure turns form values into a capture configuration.
func configure(values map[string]string) capture.Config {
	config := capture.Config{
		Camera:   capture.Facing(values["camera"]),
		Audio:    values["audio"] == "true",
		Location: values["location"] == "true",
		Motion:   values["motion"] == "true",
		Record:   values["record"] == "true",
		Stream:   values["stream"] == "true",
	}
	switch values["quality"] {
	case "480":
		config.Width, config.Height, config.Bitrate = 854, 480, 1_500_000
	case "1080":
		config.Width, config.Height, config.Bitrate = 1920, 1080, 8_000_000
	}
	return config
}

func start(context.Context, nativeui.Request) (nativeui.Result, error) {
	values := snapshot()
	config := configure(values)
	if config.Stream && values["endpoint"] == "" {
		return nativeui.Result{Announcement: "Enter a WHIP endpoint in Settings to stream."}, nil
	}
	if !config.Record && !config.Stream {
		return nativeui.Result{Announcement: "Turn on recording, streaming or both in Settings."}, nil
	}
	session, err := capture.Start(config)
	if err != nil {
		return nativeui.Result{Announcement: err.Error()}, nil
	}
	if config.Stream {
		if err := publish(session, values["endpoint"], values["token"]); err != nil {
			session.Stop()
			return nativeui.Result{Announcement: "Streaming failed: " + err.Error()}, nil
		}
	}
	return nativeui.Result{}, nil
}

func stop(context.Context, nativeui.Request) (nativeui.Result, error) {
	if session := capture.Current(); session != nil {
		session.Stop()
	}
	return nativeui.Result{}, nil
}

// describe is the status text under the preview.
func describe(session *capture.Session, background bool) string {
	if session == nil {
		if background {
			return "Ready. Capture keeps running with the screen off or another app open."
		}
		return "Ready. Capture pauses while the app is in the background."
	}
	status := session.Status()
	var lines []string
	switch status.State {
	case capture.Pending:
		lines = append(lines, "Starting the camera.")
	case capture.Running, capture.Stopping:
		elapsed := time.Since(status.Started).Round(time.Second)
		lines = append(lines, fmt.Sprintf("%s for %s. %dx%d, %d frames, %s.", stateLabel(session), elapsed, status.Info.Width, status.Info.Height, status.Frames, size(status.Bytes)))
		if status.Dropped > 0 {
			lines = append(lines, fmt.Sprintf("Dropped %d samples the stream could not keep up with.", status.Dropped))
		}
	case capture.Stopped:
		lines = append(lines, "Stopped.")
	case capture.Failed:
		lines = append(lines, "Capture failed: "+firstNonEmpty(status.Message, status.Reason))
	}
	if status.SinkError != "" {
		lines = append(lines, "Stream: "+status.SinkError)
	}
	if location := status.Location; location != nil {
		lines = append(lines, fmt.Sprintf("Location %.6f, %.6f (±%.0f m, %s).", location.Latitude, location.Longitude, location.Accuracy, location.Provider))
	} else if status.State == capture.Running && session.Config().Location && !status.Info.Location {
		lines = append(lines, "Location is off: permission denied or no provider.")
	}
	if orientation := status.Orientation; orientation != nil {
		heading := fmt.Sprintf("%.0f° magnetic", degrees(orientation.Azimuth))
		if orientation.Heading != nil {
			heading = fmt.Sprintf("%.0f° true", degrees(*orientation.Heading))
		}
		lines = append(lines, fmt.Sprintf("Camera points %s, %s, tilted %+.0f°, rolled %+.0f°.", compass(orientation.Azimuth), heading, orientation.Pitch, orientation.Roll))
	}
	return strings.Join(lines, "\n")
}

func stateLabel(session *capture.Session) string {
	config := session.Config()
	if session.State() == capture.Stopping {
		return "Stopping"
	}
	switch {
	case config.Record && config.Stream:
		return "Recording and streaming"
	case config.Stream:
		return "Streaming"
	}
	return "Recording"
}

// degrees rounds a bearing to a whole degree in [0, 360), so 359.6 reads 0.
func degrees(bearing float64) float64 {
	return math.Mod(math.Mod(math.Round(bearing), 360)+360, 360)
}

func compass(degrees float64) string {
	names := []string{"north", "northeast", "east", "southeast", "south", "southwest", "west", "northwest"}
	return names[int((degrees+22.5)/45)%8]
}

func size(bytes int64) string {
	if bytes < 1<<20 {
		return fmt.Sprintf("%d KB", bytes>>10)
	}
	return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return "unknown error"
}

func recordingsScreen(context.Context, nativeui.Request) (nativeui.Screen, error) {
	list, err := capture.Recordings()
	if err != nil {
		return nativeui.Screen{}, err
	}
	rows := make([]nativeui.Row, 0, len(list))
	for _, recording := range list {
		subtitle := string(recording.State)
		if !recording.Ended.IsZero() {
			subtitle = recording.Ended.Sub(recording.Started).Round(time.Second).String()
		}
		// The title shows the zone because Go on Android runs in UTC.
		rows = append(rows, nativeui.Row{
			ID:          recording.ID,
			Title:       recording.Started.Local().Format("Jan 2, 15:04:05 MST"),
			Subtitle:    subtitle,
			Metadata:    fmt.Sprintf("%dx%d", recording.Info.Width, recording.Info.Height),
			Destination: recordings + "/" + recording.ID,
		})
	}
	return nativeui.Screen{Title: "Recordings", Live: &nativeui.Live{Version: version()}, List: &nativeui.List{
		Rows:   rows,
		Empty:  "No recordings yet.",
		Footer: "Each recording is an MP4 and a telemetry file with location and camera direction, kept until deleted.",
	}}, nil
}

func find(id string) (capture.Recording, bool, error) {
	list, err := capture.Recordings()
	if err != nil {
		return capture.Recording{}, false, err
	}
	for _, recording := range list {
		if recording.ID == id {
			return recording, true, nil
		}
	}
	return capture.Recording{}, false, nil
}

func recordingScreen(_ context.Context, request nativeui.Request) (nativeui.Screen, error) {
	recording, ok, err := find(request.Params["id"])
	if err != nil {
		return nativeui.Screen{}, err
	}
	if !ok {
		return nativeui.Screen{Title: "Recording", Text: "This recording was deleted."}, nil
	}
	lines := []string{
		"Started " + recording.Started.Local().Format(time.RFC1123),
		strings.TrimSpace(fmt.Sprintf("State: %s %s", recording.State, recording.Reason)),
		fmt.Sprintf("Video: %dx%d, rotation %d°", recording.Info.Width, recording.Info.Height, recording.Info.Rotation),
	}
	switch count := len(recording.Segments); count {
	case 0:
		lines = append(lines, "No finished video.")
	case 1:
		lines = append(lines, fmt.Sprintf("One MP4, %s, %s, with its telemetry file.", recording.Segments[0].Duration().Round(time.Second), size(recording.Bytes())))
	default:
		lines = append(lines, fmt.Sprintf("%d MP4 segments, %s, each with its telemetry file.", count, size(recording.Bytes())))
	}
	screen := nativeui.Screen{Title: "Recording", Text: strings.Join(lines, "\n")}
	if !recording.State.Active() {
		screen.Actions = []nativeui.Action{{ID: "remove", Label: "Delete"}}
	}
	return screen, nil
}

func remove(_ context.Context, request nativeui.Request) (nativeui.Result, error) {
	recording, ok, err := find(request.Params["id"])
	if err != nil {
		return nativeui.Result{}, err
	}
	if ok {
		if err := recording.Remove(); err != nil {
			return nativeui.Result{Announcement: err.Error()}, nil
		}
	}
	return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "pop"}, Announcement: "Recording deleted."}, nil
}
