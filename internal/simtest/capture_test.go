package simtest

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/emulation"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/framework/owner"

	"github.com/DonaldMurillo/gofastr-native/examples/capture/capturewebview"
	"github.com/DonaldMurillo/gofastr-native/extension/capture"
	"github.com/DonaldMurillo/gofastr-native/render/webview/mobileui"
)

func captureStatus() any {
	if session := capture.Current(); session != nil {
		status := session.Status()
		return struct {
			State                  capture.State
			Reason                 string
			Frames, Bytes, Dropped int64
		}{status.State, status.Reason, status.Frames, status.Bytes, status.Dropped}
	}
	return "no current session"
}

// TestCaptureWebViewRecords drives the capture app's WebView adapter in
// Chromium with its synthetic camera and microphone. The capture screen has
// no collection, so its live refresh must swap the markup in place: the
// screen root and the playing preview survive every refresh.
func TestCaptureWebViewRecords(t *testing.T) {
	t.Cleanup(func() { owner.SetExtractor(nil) })
	directory := t.TempDir()
	host, err := capturewebview.Build(directory, origin, mobileui.Android)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if session := capture.Current(); session != nil {
			session.Stop()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := host.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	ctx, traffic := mobileScreensBrowser(t, host, true,
		chromedp.Flag("use-fake-ui-for-media-stream", true), chromedp.Flag("use-fake-device-for-media-stream", true))
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		traffic.mu.Lock()
		defer traffic.mu.Unlock()
		paths := traffic.paths[max(0, len(traffic.paths)-12):]
		t.Logf("bridge notes: %q\nlast requests: %q\nstatus: %+v", traffic.errs, paths, captureStatus())
	})
	check := deviceBrowserCheck(t, ctx)
	check(browser.SetPermission(&browser.PermissionDescriptor{Name: "geolocation"}, browser.PermissionSettingGranted).WithOrigin(origin),
		emulation.SetGeolocationOverride().WithLatitude(48.8584).WithLongitude(2.2945).WithAccuracy(4),
		chromedp.Navigate(origin+"/capture"),
		chromedp.WaitVisible(`[data-gfw-screen][data-gfw-instance]`, chromedp.ByQuery))
	var support string
	check(chromedp.Poll(`JSON.parse(document.querySelector('[data-gfw-screen]').dataset.gfwCapabilities || '{}')['gofastr.capture/v1'] || ''`, &support))
	var encodes bool
	check(chromedp.Evaluate(`typeof VideoEncoder === 'function' ? VideoEncoder.isConfigSupported({ codec: 'avc1.42e01f', width: 640, height: 480, avc: { format: 'annexb' } }).then(r => r.supported) : false`, &encodes,
		func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams { return p.WithAwaitPromise(true) }))
	if support != "available" || !encodes {
		t.Skipf("this Chromium cannot encode H.264 with WebCodecs (capability %q)", support)
	}

	check(chromedp.Poll(`!!document.querySelector('[data-gfw-capture] video')?.srcObject`, nil),
		clickMobileAction("Start"),
		chromedp.Poll(`document.querySelector('[data-gfw-screen]').innerText.includes('Recording for')`, nil, chromedp.WithPollingTimeout(15*time.Second)),
		chromedp.Evaluate(`window.__capture = { root: document.querySelector('[data-gfw-screen]'), video: document.querySelector('[data-gfw-capture] video'),
			text: document.querySelector('[data-gfw-screen]').innerText }`, nil),
		chromedp.Poll(`document.querySelector('[data-gfw-screen]').innerText !== window.__capture.text &&
			document.querySelector('[data-gfw-screen]').innerText.includes('Location 48.858400, 2.294500')`, nil, chromedp.WithPollingTimeout(15*time.Second)))
	var kept struct {
		Root, Video, Playing bool
		Components           int
	}
	check(chromedp.Evaluate(`({ root: window.__capture.root === document.querySelector('[data-gfw-screen]'),
		video: window.__capture.video === document.querySelector('[data-gfw-capture] video'),
		playing: !window.__capture.video.paused, components: document.querySelectorAll('[data-gfw-component]').length })`, &kept))
	if !kept.Root || !kept.Video || !kept.Playing || kept.Components != 1 {
		t.Fatalf("live refresh replaced the screen or its preview: %+v", kept)
	}

	check(clickMobileAction("Stop"),
		chromedp.Poll(`document.querySelector('[data-gfw-screen]').innerText.includes('Stopped.')`, nil, chromedp.WithPollingTimeout(15*time.Second)))
	assertDeviceTraffic(t, traffic)
	recordings, err := capture.Recordings()
	if err != nil || len(recordings) != 1 || !recordings[0].Complete() {
		t.Fatalf("recordings = %+v, %v", recordings, err)
	}
	if info := recordings[0].Info; info.Width == 0 || !info.Location || !info.Audio {
		t.Fatalf("recording info = %+v", info)
	}
	file, err := recordings[0].Segments[0].OpenVideo()
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(file)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Logf("ffprobe not on PATH; %d byte recording not decoded", len(content))
		return
	}
	path := filepath.Join(t.TempDir(), "video.mp4")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(ffprobe, "-v", "error", "-count_frames", "-show_entries", "stream=codec_name,nb_read_frames", "-of", "json", path).CombinedOutput()
	if err != nil {
		t.Fatalf("ffprobe: %v\n%s", err, out)
	}
	var probe struct {
		Streams []struct {
			Codec string `json:"codec_name"`
			Read  string `json:"nb_read_frames"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &probe); err != nil || len(probe.Streams) != 2 || probe.Streams[0].Codec != "h264" || probe.Streams[1].Codec != "opus" ||
		probe.Streams[0].Read == "0" || probe.Streams[1].Read == "0" {
		t.Fatalf("ffprobe = %s", out)
	}
}
