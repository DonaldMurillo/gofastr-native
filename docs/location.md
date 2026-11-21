# Foreground location on iOS

`device/location` defines typed CoreLocation requests through
`nativeui.ServiceRequest.Location`. The native iOS adapter is opt-in and
advertises `location/v1` only when its build flag and purpose text are present.
Android and WebView do not implement this service. A capability is not a
permission grant or a promise that system location services are enabled.

## Build and try the demo

```sh
GOWORK=off go run ./cmd/gofastr-native build --platform ios-sim --renderer native \
  --pkg ./examples/devicefeatures/featuresnative --id dev.gofastr.locationdemo \
  --name "Location Demo" --location \
  --location-usage "Show your position in the foreground location demo." \
  --out dist/mobile/location-demo
```

`--location` requires the native renderer and an iOS platform.
`--location-usage` is required, must be nonblank, and is limited to 512 UTF-8
bytes without control characters. It writes
`NSLocationWhenInUseUsageDescription`; it never requests always authorization
or adds background modes. Supplying purpose text without `--location` fails.

Open **Location** in the device-features demo. **Current position** is the
primary action; **More actions** contains **Location status**, **Authorize
location**, and **Track 5 samples**. Results remain visible, and tapping a
result opens its full text. Tracking stops at five samples or five seconds,
whichever comes first. A stationary simulator may return fewer than five
samples at the duration bound. Results live only in process memory.

## Request contract

```go
nativeui.ServiceRequest{
    ID: "nearby",
    Location: &location.Request{
        Operation: location.Track,
        Accuracy: location.Balanced,
        MaxSamples: 5,
        DurationMillis: 5000,
        DistanceFilterMeters: 10,
    },
}
```

Register a matching `nativeui.Route.Services["nearby"]` continuation to consume
the observed `nativeui.ServiceResult`. Declare `nativeui.CapabilityLocation`
as a screen requirement when the screen cannot work without location.

| Operation | Behavior | Accepted arguments |
| --- | --- | --- |
| `status` | No prompt; observes permission, services enabled, and precise/reduced accuracy | None |
| `authorize` | Requests when-in-use only if permission is not determined | Optional `Reason` |
| `current` | Prompts if needed, then returns one fix | Optional `Accuracy`, `TimeoutMillis`, `Reason` |
| `track` | Prompts if needed, then collects a bounded list of updates | Required `MaxSamples` and `DurationMillis`; optional `Accuracy`, `DistanceFilterMeters`, `Reason` |

`Validate()` rejects unknown operations, invalid bounds, nonfinite distance
filters, and arguments unrelated to the operation. `Reason` is at most 200
UTF-8 bytes without control characters. It is application context, not a
replacement for the build-time iOS purpose string: CoreLocation displays the
Info.plist purpose, not this per-request reason.

- `Accuracy`: `best` (default), `balanced`, or `coarse`, mapped to
  `kCLLocationAccuracyBest`, `kCLLocationAccuracyHundredMeters`, and
  `kCLLocationAccuracyKilometer`. This is a requested accuracy, not a guarantee.
- `TimeoutMillis`: current only; zero/omitted means 15000, maximum 60000.
  The current-position deadline includes time spent waiting for authorization.
- `MaxSamples`: track only; 1–100, required.
- `DurationMillis`: track only; 1000–120000, required. Its timer begins when
  authorized updates start. There is no separate tracking authorization timer.
- `DistanceFilterMeters`: track only; finite and nonnegative. Omission means
  no distance filter; zero is accepted. It does not guarantee a sample cadence.

Each operation owns a new location manager on the main thread. Cancellation
and entering the background stop updates, invalidate the timer, detach the
delegate and settle once. Late callbacks cannot settle a subsequent operation.
`authorize` waits for a permission outcome unless cancelled or backgrounded.

## Permissions and outcomes

Permission values are `not_determined`, `denied`, `restricted`, `when_in_use`,
and `always`. The adapter can observe an existing always grant but never asks
for one. Reduced accuracy is accepted; requesting `best` does not override
the person's approximate-location setting or request temporary full accuracy.

| Service status | Location content |
| --- | --- |
| `completed` | `Location` result required; status/authorize have no fixes, current exactly one, track 1–`MaxSamples` |
| `denied` | Permission denied or restricted; permission in top-level `ServiceResult.Permission`, no `Location` |
| `unsupported` | System services off (with observed permission), or capability/build opt-in absent; no `Location` |
| `failed` | Deadline without a valid fix or another OS failure; no `Location` |
| `cancelled` | Explicit cancellation or backgrounding; no `Location` |

The denied/restricted and services-off rules also apply to `status` and
`authorize`. A status observation does not prompt. When access is denied, the
app must explain how to change system settings rather than repeatedly prompt.
Only `completed` carries location content. Failed operations are not replayed.

## Results and fix units

`location.Result` contains `Permission`, `ServicesEnabled`, `Precise`, and
`Fixes`. Required wire booleans `servicesEnabled` and `precise` serialize even
when false; omitting either is a decoding error in Swift.

Each `Fix` contains:

- `Latitude` and `Longitude`: decimal degrees, within [-90, 90] and [-180, 180].
- `HorizontalAccuracyMeters`: nonnegative estimated accuracy in meters.
- `AltitudeMeters`: meters; meaningful only with known vertical accuracy.
- `VerticalAccuracyMeters`: nonnegative meters, or `-1` for unknown.
- `SpeedMetersPerSecond`: nonnegative meters per second, or `-1` for unknown.
- `CourseDegrees`: [0, 360), or `-1` for unknown.
- `TimestampMillis`: nonnegative Unix milliseconds from CoreLocation.

`Accept()` validates permission, operation-specific fix counts, coordinate
ranges, finite numbers, accuracy and unknown-value sentinels. Current/track
results require enabled services and an authorized permission. The adapter
skips invalid fixes but does not promise a freshly measured fix: inspect
timestamps and accuracy before using coordinates for an application decision.

## Privacy and limits

Initiate location only after an explicit person-facing action and describe its
purpose accurately. Coordinates are sensitive application data: do not put
them in route URLs, telemetry, logs, or shared storage by default. The demo
does not save or upload them. Apps consuming results own their retention,
consent and disclosure policy.

This API provides bounded foreground collection, not an ongoing subscription.
It has no background location, geofencing, heading, significant-change
monitoring, always-permission request, or automatic retries. Authorization,
availability and fix accuracy can change between requests.

## Checks and acceptance

Go tests cover request validation, result acceptance, capability negotiation
and service wire round-trips. macOS tests execute Swift wire fixtures and
compile the binding-independent library against the simulator SDK.

```sh
GOFASTR_IOS_UI=1 GOWORK=off go test -v -count=1 -timeout=20m ./internal/iosuitest
```

The gated UI test builds the real demo with `--location`, creates and boots a
fresh simulator, installs the app, grants location using `simctl privacy`,
sets `37.3349,-122.0090` using `simctl location`, launches the app and drives
all four Location actions through XCUITest. It asserts the current and
tracking coordinates in accessible on-screen rows and exports a screenshot
attachment. It shuts down and deletes its own simulator afterward. Build
logs, the result bundle and screenshots remain under
`~/artifacts/location-ui/run-*`; override the parent with
`GOFASTR_IOS_UI_ARTIFACTS`.

iOS 26.5 simulator acceptance passed with Xcode 26.6: status and authorize
reported the pregranted permission, and current and duration-bounded tracking
displayed `37.3349, -122.0090`. This does not exercise the first-use prompt.

NOT RUN: first-use permission dialogs, denial/restriction and services-off UI,
approximate-accuracy settings, cancellation/background behavior through the
UI, VoiceOver, real GPS hardware, physical-device installation, and store
distribution. See [physical-device acceptance](ios-device-acceptance.md) and
[release signing](release.md). The existing device path supports development
and ad hoc IPAs, not TestFlight/App Store distribution.
