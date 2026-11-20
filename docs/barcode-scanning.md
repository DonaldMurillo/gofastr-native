# Barcode and QR scanning on Android

The native Android scanner reads one QR, Code 128, EAN-13, EAN-8, UPC-A, or
UPC-E code from a camera photo or a user-selected image. It does not scan a
live preview. Decoding is offline. Application code receives the format and
decoded text, not the image or a file path. Treat the text as untrusted data.

## Try the demo

```sh
go run ./cmd/gofastr-native build --platform android-emu --renderer native --pkg ./examples/devicefeatures/featuresnative --out dist/mobile/device-features
adb install -r dist/mobile/device-features/Featuresnative.apk
```

Open **Barcode and QR scanner**, then choose **Scan with camera** to take and
confirm a system camera photo, or choose **Scan from image** in the overflow
menu to select an image.
Keep a single code in focus with room around its edges. If the image contains
no supported code, the demo says **No barcode found.** An unreadable image
still fails the task.

## Go application API

Register `extension/barcode.Definition()` with `nativeui.NewWithExtensions`.
In an explicit action, call `extension/barcode.Scan` with a
`device/barcode.Request{Source: barcode.Camera}` (or `barcode.Image`). Set
the returned operation as the `Extension` of a `nativeui.FileRequest`. In the
matching `Route.Files` continuation, pass the completed document to
`extension/barcode.ReadResult` for the typed `device/barcode.Result`. A
completed scan with no supported code sets `Result.NotFound`. The
[device-features example](../examples/devicefeatures/featuresnative/featuresnative.go)
shows the full wiring.

UIKit and WebView do not advertise either scanner capability; the runtime
rejects a scan as unsupported there. Camera scans also need camera hardware,
an installed Android camera app, and nonempty `--camera-usage`. That switch
does not affect image scanning. The scanner delegates camera access to the
system camera app instead of declaring `android.permission.CAMERA`. If another
feature declares that permission, Android requires the app to have it granted
before capture. Image scanning uses the Storage Access Framework's selected
URI grant and needs no broad storage permission. Both sources respect system
cancellation and denied access; no result continuation runs on those outcomes.

The adapter reads up to 16 MiB from the selected URI, bounds the source to
100 megapixels, and samples long images to at most 2048 pixels per side before
decoding. The private camera photo is removed after the task. There is no cloud
upload, automatic link handling, or result history.

## Dependency and license

[ML Kit Barcode Scanning](https://developers.google.com/ml-kit/vision/barcode-scanning/android)
offers a larger bundled model linked at build time and an unbundled model
downloaded through Google Play services. The SDK is subject to the
[ML Kit and Google API terms](https://developers.google.com/ml-kit/terms);
the examples' Apache-2.0 license is not a license for the SDK itself.

The Android CLI builds with gomobile, javac and Android SDK tools without a
Gradle or Maven dependency resolver. The scanner uses the Go module
[gozxing](https://github.com/makiuchi-d/gozxing), an MIT-licensed Go port of
ZXing core (Apache-2.0). This fits the existing build and works without Play
services or a first-use download. Distribute the scanner with the
[gozxing and ZXing notice](../extension/barcode/LICENSE-gozxing) and
[xerrors notice](../extension/barcode/LICENSE-xerrors).
