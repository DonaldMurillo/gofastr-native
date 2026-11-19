package dashcam

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-native/extension/capture"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func TestEveryChoiceIsAValidConfig(t *testing.T) {
	for _, quality := range []string{"720", "1080"} {
		for _, segment := range []int{60, 180, 300} {
			for _, budget := range []int{2048, 4096, 8192, 16384, 32768} {
				for _, impact := range []string{"off", "low", "medium", "high"} {
					settings := Settings{Quality: quality, Segment: segment, Budget: budget, Impact: impact}
					if err := settings.valid(); err != nil {
						t.Fatalf("%+v: %v", settings, err)
					}
				}
			}
		}
	}
	// 1080p at 8 Mbps fills a 10-second file with 10 MB.
	small := defaults()
	small.Segment, small.Budget = 10, 19
	if err := small.valid(); err == nil {
		t.Fatal("accepted a loop that cannot hold two files")
	}
	small.Budget = 20
	if err := small.valid(); err != nil {
		t.Fatal(err)
	}
	config := defaults().Config()
	if config.Audio || !config.Record || config.Height != 1080 || config.Impact != 2 || config.Segment != time.Minute || config.Budget != 4<<30 {
		t.Fatalf("defaults = %+v", config)
	}
	if got := defaults().Summary(); got != "1080p in 1-minute files, 4 GB loop, locks on impacts over 2 g, no sound." {
		t.Fatalf("summary = %q", got)
	}
}

func TestSettingsPersist(t *testing.T) {
	if err := capture.Attach(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	app, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, route := range []string{root, clipsRoute, settingsRoute} {
		if _, err := app.RenderJSON(ctx, route, "{}"); err != nil {
			t.Fatalf("%s: %v", route, err)
		}
	}
	if session := capture.Current(); session != nil && session.State().Active() {
		t.Fatal("without AutoStart, opening the app must not record")
	}
	if _, err := app.DispatchJSON(ctx, settingsRoute, "save", `{"quality":"720","segment":"180","budget":"8192","impact":"high","audio":"true","location":"false","autoStart":"true"}`); err != nil {
		t.Fatal(err)
	}
	reopened := openStore(dir).get()
	want := Settings{Quality: "720", Segment: 180, Budget: 8192, Impact: "high", Audio: true, AutoStart: true}
	if reopened != want {
		t.Fatalf("reopened = %+v", reopened)
	}
	// The form refuses a value outside its options before save runs.
	app.DispatchJSON(ctx, settingsRoute, "save", `{"quality":"4k"}`)
	if openStore(dir).get() != want {
		t.Fatal("a rejected save changed the file")
	}
	if err := os.WriteFile(filepath.Join(dir, "dashcam.json"), []byte(`{"quality":"8k"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if openStore(dir).get() != defaults() {
		t.Fatal("an invalid file must give the defaults")
	}

	// Valid values the form does not offer still open, as an extra option.
	odd := defaults()
	odd.Segment, odd.Budget = 10, 40
	if err := openStore(dir).set(odd); err != nil {
		t.Fatal(err)
	}
	if app, err = New(dir); err != nil {
		t.Fatal(err)
	}
	screen, err := app.RenderJSON(ctx, settingsRoute, "{}")
	if err != nil || !strings.Contains(screen, `"10 seconds"`) || !strings.Contains(screen, `"40 MB"`) {
		t.Fatalf("settings with values outside the options = %s %v", screen, err)
	}
}

// A live screen's version must be the one its watch compares against, or
// the watch reports a change every time and the screen reloads nonstop.
func TestLiveScreensSettle(t *testing.T) {
	if err := capture.Attach(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	a := &app{settings: openStore("")}
	for _, route := range a.routes() {
		if route.Watch == nil {
			continue
		}
		screen, err := route.Screen(context.Background(), nativeui.Request{})
		if err != nil || screen.Live == nil {
			t.Fatalf("%s: %+v %v", route.Path, screen.Live, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		version, err := route.Watch(ctx, nativeui.Request{}, screen.Live.Version)
		cancel()
		if err == nil {
			t.Fatalf("%s: nothing changed, but its watch returned %q for %q", route.Path, version, screen.Live.Version)
		}
	}
}

// device plays the Android adapter: start, then finish one segment file.
func device(t *testing.T, session *capture.Session, captureRoot string) {
	t.Helper()
	info, _ := json.Marshal(capture.Info{Width: 1920, Height: 1080, Location: true, Motion: true, Acceleration: true, Clock: 1e9, Wall: time.Now()})
	capture.Event(session.ID(), "started", string(info))
	if err := os.WriteFile(filepath.Join(captureRoot, session.ID(), "segment-00001.mp4"), make([]byte, 2048), 0o600); err != nil {
		t.Fatal(err)
	}
	capture.Event(session.ID(), "segment", `{"index":1,"start":1000000000,"end":61000000000}`)
}

func TestAutoStartLockAndClips(t *testing.T) {
	captureRoot := t.TempDir()
	if err := capture.Attach(captureRoot); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	saved := defaults()
	saved.AutoStart = true
	if err := openStore(dir).set(saved); err != nil {
		t.Fatal(err)
	}
	app, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	screen, err := app.RenderJSON(ctx, root, "{}")
	if err != nil {
		t.Fatal(err)
	}
	session := capture.Current()
	if session == nil || session.State() != capture.Pending || !strings.Contains(screen, session.ID()) {
		t.Fatalf("opening the app must start recording: %s", screen)
	}
	t.Cleanup(func() {
		session.Stop()
		capture.Event(session.ID(), "stopped", `{}`)
	})
	device(t, session, captureRoot)
	if _, err := app.RenderJSON(ctx, root, "{}"); err != nil || capture.Current() != session {
		t.Fatalf("a second render must not start again: %v", err)
	}
	screen, _ = app.RenderJSON(ctx, root, "{}")
	if !strings.Contains(screen, `"lock"`) || !strings.Contains(screen, "file 2") {
		t.Fatalf("recording screen = %s", screen)
	}

	// 25 m/s² is past the 2 g default: segment 1 locks.
	capture.Event(session.ID(), "acceleration", `{"clock":62000000000,"x":25,"y":0,"z":0}`)
	screen, _ = app.RenderJSON(ctx, root, "{}")
	if !strings.Contains(screen, "after a 2.5 g impact") || !strings.Contains(screen, "1 locked clip") {
		t.Fatalf("after impact = %s", screen)
	}
	clips, err := app.RenderJSON(ctx, clipsRoute, "{}")
	if err != nil || !strings.Contains(clips, "Impact, 1m0s") {
		t.Fatalf("clips = %s %v", clips, err)
	}
	clipRoute := clipsRoute + "/" + session.ID() + "/1"
	if detail, err := app.RenderJSON(ctx, clipRoute, "{}"); err != nil || !strings.Contains(detail, "segment-00001.mp4") {
		t.Fatalf("clip = %s %v", detail, err)
	}
	if _, err := app.DispatchJSON(ctx, clipRoute, "unlock", "{}"); err != nil {
		t.Fatal(err)
	}
	if clips, _ := app.RenderJSON(ctx, clipsRoute, "{}"); !strings.Contains(clips, "No locked clips") {
		t.Fatalf("after unlock = %s", clips)
	}
	if _, err := app.DispatchJSON(ctx, root, "stop", "{}"); err != nil || session.State() != capture.Stopping {
		t.Fatalf("stop: %v %s", err, session.State())
	}
}
