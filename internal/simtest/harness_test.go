package simtest

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesapp"
	"github.com/DonaldMurillo/gofastr-native/mobilehost"
	"github.com/DonaldMurillo/gofastr-native/render/webview/mobileui"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

func screensHost(t *testing.T, platform mobileui.Platform) (*framework.App, string, *mobilehost.Host) {
	t.Helper()
	t.Cleanup(func() { owner.SetExtractor(nil) })
	directory := t.TempDir()
	app, host, err := notesapp.BuildScreensWithPlatform(directory, origin, platform)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := host.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	return app, directory, host
}
