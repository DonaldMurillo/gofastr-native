# Barcode scanner

`device/scanner` defines a one-shot `scanner/v1` device service. The native iOS
adapter presents a camera sheet; Android and WebView do not implement it.

```go
request := nativeui.ServiceRequest{
    ID: "scan",
    Scan: &scanner.Request{Title: "Scan code", Symbologies: []scanner.Symbology{scanner.QR}},
}
```

Return the request from an explicit action and handle its result in
`Route.Services["scan"]`. A completed result requires `ServiceResult.Scan`, with
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

Hardware camera acceptance: **NOT RUN**. Simulator fixture checks do not verify
camera optics, focus, permission prompts, physical barcode formats, interruption
behavior or accessibility on hardware.
