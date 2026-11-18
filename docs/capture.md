# Video capture with location and camera direction

`extension/capture` records camera video, streams it live, or both. While it
runs it also reports GPS fixes and where the camera points. It has two
adapters, and both hand Go the same session events and encoded samples:

- The Android adapter, for the native renderer. A foreground service owns the
  camera, so capture keeps running with the screen off or another app in
  front.
- The WebView adapter, for the WebView renderer on Android. The page reads
  the camera with `getUserMedia` and WebCodecs. Capture pauses while the app
  is in the background. See [the WebView adapter](#the-webview-adapter).

There is no UIKit adapter, so under the iOS native renderer the capability is
reported as missing and screens should say so.

`examples/capture/capturenative` is a working app on the native renderer:

```sh
go run ./cmd/gofastr-native build --platform android-emu --renderer native \
  --pkg ./examples/capture/capturenative --name Capture \
  --microphone-usage "Record sound with your video." --out dist/capture
```

Without `--microphone-usage` the app has no `RECORD_AUDIO` permission and
sessions run without sound.

## Starting a session

Register the definition, start a session from an action, and show it in a
screen. A session reaches the device when a rendered screen carries a `Block`
with it; until then it is `Pending`.

```go
screens, err := nativeui.NewWithExtensions(root,
    []extension.Definition{capture.Definition()}, routes...)

func start(context.Context, nativeui.Request) (nativeui.Result, error) {
    _, err := capture.Start(capture.Config{
        Record: true, Audio: true, Location: true, Motion: true,
    })
    return nativeui.Result{}, err
}

func captureScreen(context.Context, nativeui.Request) (nativeui.Screen, error) {
    view := capture.View{Preview: true}
    if session := capture.Current(); session != nil && session.State().Active() {
        view.Session = session
    }
    block, err := capture.Block("camera", view)
    if err != nil {
        return nativeui.Screen{}, err
    }
    return nativeui.Screen{
        Title:      "Capture",
        Live:       &nativeui.Live{Version: capture.Version()},
        Components: []extension.Component{block},
        Text:       "...",
    }, nil
}
```

Give the route `Watch: func(ctx context.Context, _ nativeui.Request, seen string) (string, error) { return capture.Watch(ctx, seen) }`.
State changes refresh at once. Telemetry and heartbeats refresh at most once a
second.

Two renderer rules shape a capture screen:

- The renderer never live-refreshes a screen with a `Form`, which protects
  edits in progress. Put settings on their own screen. The example keeps a
  form-free capture screen and a separate Settings screen.
- `Prompt` is built once per screen and does not change on a live refresh.
  Put text that changes in `Text`.

Zero `Config` values take defaults: back camera, 1280x720, 30 fps, 4 Mbit/s,
motion at 30 Hz. The encoder picks the largest supported size within the
request; `Status().Info` reports what it chose. One session runs per process.
`Start` replaces a pending session and refuses while another is running. If
the device already began the replaced session, it starts the new one as soon
as the old one finishes.

The first Start asks for camera, location, microphone and notification
permission, as the config needs. A denied camera fails the session. A denied
location or microphone runs without it, and `Info.Location` or `Info.Audio` is
false. The answer still reaches the session when Android recreates the activity
while the dialog is open, for example after a font size or language change.

`Session.Stop` ends capture. The notification's Stop button does the same.

Showing or hiding the preview during a recording rebuilds the camera session,
and the recording gets no frames while that happens. On the emulator each
change cost 170 to 250 ms. Leaving the app and coming back hides and then shows
the preview, so it costs two of those pauses. Playback holds the last frame
across each one.

## Telemetry

| Sample | Rate | Fields |
| --- | --- | --- |
| `Location` | about once a second | latitude, longitude, accuracy, altitude, speed, bearing of travel, provider, fix time |
| `Orientation` | `MotionRate` | `Azimuth` from magnetic north, `Heading` from true north once a fix gives the declination, `Pitch`, `Roll`, quaternion |
| `Rotation` | `MotionRate`, with `Gyroscope` | raw angular velocity in rad/s |
| `Acceleration` | `MotionRate`, with `Acceleration` or `Impact` | linear acceleration in m/s² with gravity removed; `G()` gives its size in g |

Direction comes from Android's fused rotation-vector sensor. `Location.Bearing`
is the direction of travel, not where the camera points. Acceleration comes
from the linear acceleration sensor, or from the accelerometer minus a
low-pass estimate of gravity on phones without one. The WebView adapter reads
`devicemotion` the same way.

Every sample carries `Clock`, the device clock in nanoseconds: Android's
`elapsedRealtimeNanos` from the native adapter, and the page's
`performance.now()` from the WebView adapter. Video and audio presentation
times use the same clock as telemetry, so `Info.WallTime(clock)` places them
on one timeline. Clock values from different sessions do not compare.

## Recording and upload

With `Record`, each session gets a directory under the app's private capture
root:

| File | Contents |
| --- | --- |
| `segment-00001.mp4`, ... | H.264 with AAC (Android adapter) or Opus (WebView adapter), with a rotation matrix for upright playback |
| `segment-00001.jsonl`, ... | one `Telemetry` object per line, as Go received them while that segment was written |
| `session.json` | config, start report, start and end times, final state, and the finished segments with their locks |

`Config.Segment` splits the recording into files of that length, from 5
seconds to an hour. Zero keeps one segment for the whole session. Go asks for
each cut. The Android adapter closes its muxer at the next keyframe and opens
the next file with the same tracks. The WebView adapter's MP4 writer in Go
does the same. A segment is listed only once its file is complete, so a
killed process loses at most the segment being written. With `Segment` zero
that segment is the whole recording. A file that fails to finish, for example
on a full disk, is deleted, and recording carries on in the next one.
Telemetry near a cut can land in the neighbouring sidecar; its `Clock` places
it.

`capture.Recordings()` lists recordings newest first. Each `Recording` has
`Segments`, oldest first, and each `Segment` has `OpenVideo` and
`OpenTelemetry` for an upload, its `Start` and `End` on the device clock, and
its size. `Recording.Remove` deletes a recording and refuses while it is
still recording. `RemoveSegment` deletes one segment.

A recording whose process died is listed as `Stopped` with reason
`interrupted`. Its finished segments play. The next `Attach` or session start
settles it. A segment file the manifest does not list yet is kept when it has
its index (the muxer finished it just before the process died), with the lock
the session held for it and its start estimated from the segment before. The
file it was writing has no index and is deleted.

### Loop recording and locks

`Config.Budget` is the loop size in bytes. When unlocked segments across all
recordings exceed it, the oldest go first, by recording start and then by
segment. A segment counts its MP4 and its telemetry sidecar. Locked segments
do not count toward the budget and are never deleted by it, so many locks can
still fill the disk. A file that cannot be deleted, such as one open in a
player, stays listed and counted, and the next oldest goes instead. A
recording left with no segments is removed. The budget is applied at start,
after each segment and at the end. The segment being written is not counted
until it finishes.

`Session.Lock(reason)` locks the segments before, during and after the
current one, and cuts the current segment early so the footage up to the lock
is on disk. `Recording.Lock(index, reason)` locks or, with an empty reason,
unlocks a segment later. `Status()` reports the last lock's time and reason.

`Config.Impact` is a threshold in g, from 0.5 to 20. It turns on
`Acceleration`, and any sample past it calls `Lock(capture.LockImpact)`.
`Status().Impact` holds the sample. A phone in a car mount reads about 1 g
over a pothole; the dash cam example uses 2 g by default.

The Android notification has a Lock button beside Stop while recording. It
calls `Lock(capture.LockManual)`.

## Live streaming over WHIP

With `Stream`, the session hands encoded media to sinks. `whip.Publish` is one
sink: it negotiates WebRTC with any WHIP endpoint and sends H.264 constrained
baseline video, Opus audio on Android 10 and newer, and telemetry as JSON on an
unordered `telemetry` data channel.

```go
session, err := capture.Start(capture.Config{Stream: true, Record: true, Location: true, Motion: true})
if err != nil {
    return err
}
_, err = whip.Publish(ctx, session, whip.Options{
    Endpoint:   "https://media.example.com/whip/live",
    Token:      token,
    Telemetry:  true,
    ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.example.com:3478"}}},
})
```

Publish before the camera starts so a bad endpoint fails the action instead of
a running session. Viewer loss reports (PLI, FIR) request a keyframe. REMB
estimates lower the encoder bitrate at once and raise it at most every two
seconds, never above the configured bitrate. Stopping the session sends the
WHIP `DELETE`.

A sink that falls behind drops video until the next keyframe and asks for one,
so it never stalls the encoder or the recording. A sink that returns an error
is detached and reported in `Status().SinkError`.

Live video carries no rotation metadata. A phone held upright sends frames
rotated by `Info.Rotation` degrees; the receiver must rotate them. Recordings
are not affected.

`Session.AddSink` takes any other `capture.Sink`, such as an upload of
segments to your own server.

## The WebView adapter

`examples/capture/capturewebview` is the same app on the WebView renderer:

```sh
go run ./cmd/gofastr-native build --platform android-emu \
  --pkg ./examples/capture/capturewebview --name CaptureWeb \
  --microphone-usage "Record sound with your video." --out dist/capture-web
```

It reuses the `capturenative` screens through `capturenative.NewForeground`,
which drops the promise that capture survives the background. The host mounts
`capture.WebViewHandler()` at `capture.WebViewPath`; the page posts batches of
events and samples there and reads directives (stop, keyframe, bitrate) from
the answer. Recording and `whip.Publish` work unchanged, because Go receives
the same calls the Android adapter makes.

How it differs from the Android adapter:

- The app must stay in the foreground. In the background the WebView stops
  delivering frames. The session stays open and resumes when the app returns,
  so a recording holds its last frame across the gap.
- Go writes the MP4 segments itself from the encoded samples, with Opus audio. The
  frames arrive upright, so `Info.Rotation` is 0.
- If Go cannot create a file or write a sample, the session fails with reason
  `storage`. The file being written still gets its index, so what came before
  the failure plays.
- Orientation comes from `deviceorientationabsolute` when the WebView offers
  it, else `deviceorientation`. There is no declination, so `Heading` is never
  set. `Location.Provider` is `webview`.
- The capability is available only in a secure context with `getUserMedia`,
  WebCodecs `VideoEncoder` and `MediaStreamTrackProcessor`. A WebView that
  cannot encode H.264 fails the session with reason `unsupported`.
- A page reload during a session ends it as `Stopped` with reason
  `interrupted`.
- A host that shuts down its WebView calls `capture.WebViewClosed()`. The
  page cannot report that it stopped, so the session ends there as `Stopped`
  with reason `closed`, keeping what Go recorded. `examples/capture` calls it
  from its `Stop`.

The manifest's `webview` entry lists `permissions: ["camera", "geolocation"]`.
For Android WebView builds the CLI turns those into `CAMERA` and location
permissions in the generated manifest, and the shell grants the page's camera,
microphone and location requests once Android grants the app the matching
permission. The microphone permission follows `--microphone-usage`, as on the
native renderer.

## A dash cam

`examples/dashcam` is a dash cam on the native renderer:

```sh
go run ./cmd/gofastr-native build --platform android-emu --renderer native   --pkg ./examples/dashcam --name DashCam   --microphone-usage "Record sound in the car when you turn it on." --out dist/dashcam
```

It records 1080p in one-minute files into a 4 GB loop and locks the footage
around impacts over 2 g. The main screen has Start, then Lock and Stop while
recording, with the time, file number, position, speed and facing. Locked
clips lists what loop recording keeps, with Unlock and Delete. Settings
change quality, file length, loop size, impact sensitivity, sound, location
and starting on open. They persist in `dashcam.json` in the app's data
directory. Sound is off by default, because it records conversations in the
car.

Start on open begins recording the first time the main screen renders in a
process. Android does not let a camera foreground service start from the
background on Android 11 and newer, so the app cannot start itself on boot or
when the phone joins the car's Bluetooth. Once started, it records with the
screen off or another app in front until Stop.

## Build notes

The library declares its Android adapter in `gofastr-native.extension.json`:

- `bind` lists library Go packages gomobile binds beside the app.
  `capturebind` lets the library's Java call its Go without app glue.
- `permissions` are merged into the generated manifest.
- `services` declares `GofastrCaptureService` with
  `foregroundServiceType="camera|location|microphone"`.

pion's Android network support comes from `github.com/wlynxg/anet`, which uses
`//go:linkname` into the standard library. Go 1.23 and newer refuse that link
unless the linker gets `-checklinkname=0`. The CLI adds the flag only when the
bound packages depend on that module.

Go on Android runs in UTC, because apps get no `TZ` variable. Show the zone
when formatting times, as the example does.

## Verification

Checked on an Android 14 x86-64 emulator with its virtual camera scene:

- Preview, then Start with permission dialogs for location, microphone and
  notifications. Capture started and the foreground notification appeared.
- 30 seconds on the home screen, then with the screen off. Frames kept
  arriving at about 26 per second throughout.
- Stop, then the recording in the list. The pulled MP4, then one segment for
  the whole session, probed as 66.5 s
  of 1280x720 H.264 (1,632 frames) and 66.2 s of AAC, with a rotation matrix,
  and a frame from 30 s in played upright. The telemetry sidecar held 47 GPS
  fixes and orientation samples at the configured rate.
- Record and stream to a local WHIP receiver through the emulator's NAT, with
  a Bearer token. The receiver got H.264, Opus and telemetry messages; 818
  received frames decoded at 1280x720. Stop sent the `DELETE`.

The WebView adapter ran on the same emulator, whose Android System WebView is
version 113. The emulator ran with its audio device turned off:

- The page's camera and microphone requests raised Android's permission
  dialogs. Start recorded, and the status text refreshed every second while
  the preview kept playing.
- Stop, then the recording. The pulled MP4 probed as 24.7 s of
  720x1280 constrained-baseline H.264 (209 frames). The telemetry sidecar held
  a GPS fix and orientation samples. With no audio device the microphone could
  not start, so the session ran without sound and `Info.Audio` was false.
- Record and stream to the local WHIP receiver. The receiver got H.264 and
  telemetry messages, and 639 received frames decoded at 720x1280. About 12 s
  on the home screen stopped the frames; they resumed on return, and the
  recording showed one 11.7 s frame across the gap. Stop sent the `DELETE`.

The dash cam ran on the same Android 14 emulator with 10-second files and a
40 MB loop:

- Files rotated about every 10 s. Each finished file had its index and
  probed as about 10.4 s of 1280x720 H.264 (265 frames in one). The
  emulator's camera tops out at 1280x720, so the 1080p setting recorded 720p.
- Across six recordings in 25 minutes, the longest about five minutes, loop
  recording deleted the oldest unlocked files and kept every locked one.
- The emulator's acceleration sensor set to 30 m/s² locked the files around
  it as an impact of 3.1 g. Lock on the screen and Lock in the notification
  each locked by hand. Unlock and Delete in Locked clips worked, and Delete
  removed the MP4 and its telemetry.
- Reinstalling the app killed two recordings mid-file. On the next start both
  were listed as interrupted, their finished files played, and the unfinished
  file was deleted.
- With Start on open saved, a cold start began recording. Recording continued
  for 30 s with the screen off.
- The camera preview stayed live while recording. Before a renderer fix, each
  live refresh detached the screen and recreated the preview surface.

Go tests cover the session lifecycle, recordings, sinks, directives and
heartbeats, segment cuts on both adapters, locks, impacts, the loop budget
across recordings, recovery after a killed process (including a finished file
the manifest had not listed), segments that fail to finish, a manifest that
cannot be written, a Go-written recording that cannot create its next file or
write a sample, and a sink stuck in a write
(`go test ./extension/capture/...`), plus a loopback WHIP exchange with
keyframe requests, a redirected endpoint and an HTTPS endpoint that hands out
a plain HTTP session URL. The WebView endpoint has tests for bad reports and a
reload, and a recording from a real x264 clip with Opus that ffprobe decodes.
`go test ./cmd/gofastr-native` compiles both Android shells against the
Android SDK and generated bindings and checks the merged manifests.
`go test ./internal/simtest -run TestCaptureWebViewRecords` drives the WebView
adapter in headless Chromium with its synthetic camera and microphone: it
records H.264 and Opus, and checks that live refreshes keep the screen and the
playing preview.

NOT RUN: physical devices and ARM64, Android versions other than 14, the front
camera, 480p and 1080p, raw gyroscope output, TalkBack, and streaming over the
public internet with STUN or TURN. For the dash cam also: a real car mount and
real impacts, a recording longer than about five minutes, heat and battery drain, a
full storage device, and a crash of the phone itself rather than the app. For the
WebView adapter also: microphone audio on Android, iOS WKWebView, a WebView
without H.264 encoding, and segment cuts and impact locks on a device.
