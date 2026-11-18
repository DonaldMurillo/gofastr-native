package capturenative

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-native/extension/capture"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func TestScreensRender(t *testing.T) {
	if err := capture.Attach(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	app, err := New()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, route := range []string{root, settingsRoute, recordings} {
		if _, err := app.RenderJSON(ctx, route, "{}"); err != nil {
			t.Fatalf("%s: %v", route, err)
		}
	}
	before := version()
	if _, err := app.DispatchJSON(ctx, settingsRoute, "save", `{"camera":"front","quality":"480","record":"true","stream":"false","endpoint":"","token":"","audio":"true","location":"true","motion":"true"}`); err != nil {
		t.Fatal(err)
	}
	waiting, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if next, err := watch(waiting, nativeui.Request{}, before); err != nil || next == before {
		t.Fatalf("a settings save must refresh live screens: %q %v", next, err)
	}
	result, err := app.DispatchJSON(ctx, root, "start", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	session := capture.Current()
	if session == nil || session.State() != capture.Pending || session.Config().Camera != capture.Front || session.Config().Height != 480 {
		t.Fatalf("start did not register the session: %s", result)
	}
	defer session.Stop()
	screen, err := app.RenderJSON(ctx, root, "{}")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(screen, session.ID()) || !strings.Contains(screen, `"stop"`) {
		t.Fatalf("running screen lacks the session or Stop: %s", screen)
	}
}

func TestReadyTextMatchesTheRenderer(t *testing.T) {
	if text := describe(nil, true); !strings.Contains(text, "keeps running") {
		t.Fatalf("native ready text = %q", text)
	}
	if text := describe(nil, false); strings.Contains(text, "keeps running") || !strings.Contains(text, "pauses") {
		t.Fatalf("foreground ready text = %q", text)
	}
}

func TestBearingsRoundIntoACircle(t *testing.T) {
	for bearing, want := range map[float64]float64{359.6: 0, 0.4: 0, 180.5: 181, -0.2: 0, -10: 350, 720.2: 0} {
		if got := degrees(bearing); got != want {
			t.Errorf("degrees(%v) = %v, want %v", bearing, got, want)
		}
	}
}

// A live screen's version must be the one its watch compares against, or
// the watch reports a change every time and the screen reloads nonstop.
func TestLiveScreensSettle(t *testing.T) {
	if err := capture.Attach(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for _, route := range routes(false) {
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
