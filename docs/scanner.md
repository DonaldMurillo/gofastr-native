# Barcode scanner

`device/scanner` defines a one-shot `scanner/v1` device service. The native iOS
renderer presents a live camera sheet. Native Android reads one still photo or
selected image through the opt-in `extension/barcode` adapter. WebView does not
implement it and reports the service as unsupported.

```go
request := nativeui.ServiceRequest{
    ID: "scan",
    Scan: &scanner.Request{Title: "Scan code", Symbologies: []scanner.Symbology{scanner.QR}},
}
```

Return the request from an explicit action and handle its result in
`Route.Services["scan"]`. The same request and result types work on both
platforms. A completed result requires `ServiceResult.Scan`, with
`Value` and `Symbology`; cancelled, denied, unsupported and failed outcomes carry
no scanned content. Do not automatically open URLs or execute scanned text.

The title is required, plain UTF-8 text without control characters, at most 200
bytes. Empty symbologies means all supported formats: `qr`, `ean13`, `ean8`,
`upce`, `code39`, `code93`, `code128`, `itf14`, `pdf417`, `aztec`, `datamatrix`.
Unknown and duplicate formats are rejected. `Request.Validate` checks requests;
`Request.Accept` requires a nonempty valid UTF-8 value of at most 4096 bytes and
a requested known format. Scanned values may contain line breaks and other
control characters; treat them as untrusted data.

## iOS permissions and lifecycle

`scanner/v1` is advertised only when a camera exists and the app includes a
nonblank `NSCameraUsageDescription`. Permission is requested only after the
sheet appears, never during capability discovery. Denial reports `denied`;
missing hardware or purpose text reports `unsupported`. The first accepted code
stops the capture session before dismissal and settles once. Cancel,
backgrounding and capture interruption settle as cancelled; capture errors
report failed. No image or video is saved.

The CLI already includes camera purpose text by default. Prefer an accurate
scanner purpose with `--camera-usage 'Scan a code when you choose Scan code.'`.
An empty `--camera-usage` disables camera-backed scanning. No additional
entitlement, extension or App Group is needed.

## Android adapter

Register the adapter so Android builds compile it and bind its decoder:

```go
app, err := nativeui.NewWithExtensions(root, []extension.Definition{barcode.Definition()}, routes...)
```

`barcode` is `github.com/DonaldMurillo/gofastr-native/extension/barcode`. iOS
builds ignore it because it has no UIKit adapter. Without it, Android reports
`scanner/v1` unavailable and a scan settles as `unsupported`. The adapter
accepts no file operations; scans always go through `ServiceRequest.Scan`.

A scan opens a dialog titled with `Request.Title`. **Take photo** hands off to
the system camera app; it appears only when the device has a camera and
`--camera-usage` is nonempty. Without a camera app it reports `failed`. **Choose image** opens the
Storage Access Framework picker for one image and needs no storage permission.
The adapter delegates camera access instead of declaring
`android.permission.CAMERA`. If another feature declares that permission,
Android requires it to be granted before the camera app opens.

Decoding runs offline in Go with [gozxing](https://github.com/makiuchi-d/gozxing).
Android reads `qr`, `datamatrix`, `ean13`, `ean8`, `upce`, `code128`,
`code93`, `code39` and `itf14`. It cannot read `pdf417` or `aztec`. A request
limited to those two settles as `unsupported` before any dialog appears. A
mixed request searches only for the formats Android reads. UPC-A codes report
`ean13` with a leading zero, the value iOS reports. GS1-128 values start with
the `]C1` symbology identifier and separate fields with GS (0x1d).

Closing the dialog, the camera app or the picker reports `cancelled`. A photo
or image with no code in a requested format reports `failed` with the message
"No supported code was found in the image." An unreadable or oversized image
also reports `failed`, and revoked image access reports `denied`. The adapter
reads up to 16 MiB, rejects sources over 100 megapixels and samples images to
at most 2048 pixels per side. It deletes the private camera photo when the
task settles. There is no live preview, cloud upload, automatic link handling
or result history.

Go tests encode each supported format with gozxing writers and decode it
through the same path the adapter uses. The adapter's Java compiles against
the Android SDK and the gomobile bindings. On-device Android behavior:
**NOT RUN**. No emulator or hardware run has checked the dialog, the camera
handoff, the picker, real photos or TalkBack.

### Dependency and license

The Android CLI builds with gomobile, javac and Android SDK tools, with no
Gradle or Maven dependency resolution. gozxing is an MIT-licensed Go port of
ZXing core (Apache-2.0). It fits that build and needs no Play services or
model download. Distribute the adapter with the
[gozxing and ZXing notice](../extension/barcode/LICENSE-gozxing) and the
[xerrors notice](../extension/barcode/LICENSE-xerrors).

## Simulator UI checks

The device-features example has a **Scan code** action and displays the value
and symbology or the honest terminal status. Run on a Mac with an installed
simulator runtime:

```sh
SIMULATOR_UDID=<simulator-uuid> bash scripts/ios-ui-tests.sh
```

The script builds the native example, installs it with `simctl`, and runs the
standalone XCUITest target against `dev.example.devicefeatures`. Set `OUT` to
choose the build and test-output directory. The two tests cover completion and
cancellation. No third-party test dependencies are required.

Only simulator builds recognize launch environment `GOFASTR_SCANNER_FIXTURE`.
When set, the sheet offers **Scan test code**, renders the environment text
using `CIQRCodeGenerator`, and decodes the image using Vision's
`VNDetectBarcodesRequest` before returning a normal completed QR result. The
fixture bypasses camera permission and hardware discovery, but not Go result
validation. Non-QR-only requests cannot complete from this QR fixture. The
environment hook and test button are compiled out of physical-device builds.

iOS hardware camera acceptance: **NOT RUN**. Simulator fixture checks do not verify
camera optics, focus, permission prompts, physical barcode formats, interruption
behavior or accessibility on hardware.
