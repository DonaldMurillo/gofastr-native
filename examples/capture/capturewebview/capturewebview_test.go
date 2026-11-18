package capturewebview

import (
	"bytes"
	"context"
	"encoding/binary"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/owner"

	"github.com/DonaldMurillo/gofastr-native/extension/capture"
	"github.com/DonaldMurillo/gofastr-native/mobilehost"
	"github.com/DonaldMurillo/gofastr-native/render/webview/mobileui"
)

func TestServesCaptureScreensAndDeviceReports(t *testing.T) {
	t.Cleanup(func() { owner.SetExtractor(nil) })
	if _, err := Build("relative", "", mobileui.Android); err == nil {
		t.Fatal("relative data directory accepted")
	}
	h, err := Build(t.TempDir(), "", mobileui.Android)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := h.Stop(ctx); err != nil {
			t.Fatal(err)
		}
	}()
	page, err := h.Do(context.Background(), &mobilehost.Request{Method: http.MethodGet, URL: mobilehost.DefaultOrigin + "/", Header: http.Header{}})
	if err != nil {
		t.Fatal(err)
	}
	body := string(page.Body)
	if page.Status != http.StatusOK || !strings.Contains(body, "data-gfw-screen") || !strings.Contains(body, capture.Name) {
		t.Fatalf("root page: %d %.600s", page.Status, page.Body)
	}
	if policy := page.Header.Get("Permissions-Policy"); !strings.Contains(policy, "camera=(self)") || !strings.Contains(policy, "geolocation=(self)") {
		t.Fatalf("Permissions-Policy = %q", policy)
	}

	session, err := capture.Start(capture.Config{Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Stop()
	header := []byte(`{"session":"` + session.ID() + `","items":[{"event":"started","payload":"{\"width\":64,\"height\":48}"}]}`)
	report := append(binary.BigEndian.AppendUint32(nil, uint32(len(header))), header...)
	answer, err := h.Do(context.Background(), &mobilehost.Request{Method: http.MethodPost, URL: mobilehost.DefaultOrigin + capture.WebViewPath,
		Header: http.Header{"Content-Type": {"application/octet-stream"}}, Body: report})
	if err != nil {
		t.Fatal(err)
	}
	if answer.Status != http.StatusOK || len(bytes.TrimSpace(answer.Body)) != 0 || session.State() != capture.Running {
		t.Fatalf("device report: %d %q, session %s", answer.Status, answer.Body, session.State())
	}
}

// Closing the host takes the page with it, so the page never reports that
// capture stopped. Stop must end the session itself, or the next host in
// the same process cannot start one.
func TestStopEndsTheSession(t *testing.T) {
	t.Cleanup(func() { owner.SetExtractor(nil) })
	// The test before leaves its session waiting for a page report.
	capture.WebViewClosed()
	if err := Start(t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Stop() })
	session, err := capture.Start(capture.Config{Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	capture.Event(session.ID(), "started", `{"width":64,"height":48}`)
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	if state := session.State(); state != capture.Stopped {
		t.Fatalf("after Stop, state = %s", state)
	}
	next, err := capture.Start(capture.Config{Stream: true})
	if err != nil {
		t.Fatalf("a new session after Stop: %v", err)
	}
	next.Stop()
}
