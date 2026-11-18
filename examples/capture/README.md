# Capture

An Android app that records camera video with GPS and camera direction, streams
it live over WHIP, or does both. The library and its limits are described in
[video capture](../../docs/capture.md).

`capturenative` runs on the native renderer. Capture keeps running with the
screen off or another app open:

```sh
go run ./cmd/gofastr-native run --platform android-emu --renderer native \
  --pkg ./examples/capture/capturenative --name Capture \
  --microphone-usage "Record sound with your video." --out dist/capture
```

`capturewebview` is the same app on the WebView renderer. The page captures
with WebCodecs, so capture pauses while the app is in the background:

```sh
go run ./cmd/gofastr-native run --platform android-emu \
  --pkg ./examples/capture/capturewebview --name CaptureWeb \
  --microphone-usage "Record sound with your video." --out dist/capture-web
```

The capture screen shows the preview, what Start will do, and while running:
elapsed time, frames, size, the last location fix and where the camera points.
Settings picks the camera, quality, recording, streaming, the WHIP endpoint and
token, and which sensors to use. Settings live in memory and reset when the
app restarts. Recordings lists finished sessions with their segment count and
size, and deletes them.

For loop recording and impact locks, see [the dash cam](../dashcam).

To stream from an emulator to a receiver on the same computer, use
`http://10.0.2.2:<port>/...` as the endpoint. Streaming from a phone to a
server behind NAT needs a STUN or TURN server in `capturenative/stream.go`.
