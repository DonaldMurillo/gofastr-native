# Changelog

This repository holds two Go modules, tagged separately:

- `github.com/DonaldMurillo/gofastr-native`, tagged `vX.Y.Z`
- `github.com/DonaldMurillo/gofastr-native/tui`, tagged `tui/vX.Y.Z`


## Unreleased

### Added

- iOS-only `extension/maps` MapKit component with keyed pins, callout navigation,
  optional user location, a gallery screen and `--location --location-usage`
  opt-in purpose text.
- Typed foreground iOS location services in `device/location`: permission
  status, when-in-use authorization, current position and bounded tracking.
  Native builds opt in with `--location --location-usage`; no background
  modes or always-permission prompts are added. The device-features demo
  displays the results. Automated iOS 26.5 simulator UI acceptance covers
  pregranted permission and simulated current/tracking coordinates.
  First-use prompts, physical GPS and physical-device acceptance are NOT RUN.
- Native Android implements the same `device/location` contract and
  `location/v1` capability: status, authorize, current position and bounded
  tracking through `LocationManager`, opted in with `--location`
  (`--location-usage` is optional on Android). The manifest gains coarse and
  fine location only, never background location. Reads are cancelled when the
  activity pauses, except while the permission dialog is showing. SDK compile
  tests cover the adapter; emulator and physical-device runs are NOT RUN.
- Android adapter for `device/refresh` background refresh: `--background-refresh`
  now builds for the native Android renderer too. The bound package's
  `BackgroundRefresh` runs from one persisted periodic JobScheduler job, in a
  process with or without an activity. SDK compile and CLI tests pass; an
  Android emulator run of this adapter is NOT RUN.

### Internal

- De-duplicated production and test code with no intended behavior change:
  one atomic private-file write and bounded-text predicates shared via
  `internal/privatefile` and `internal/uitext`; one WAV test fixture via
  `internal/fixtures`; one foreground-presented guard in `nativeui`;
  shared Swift helpers in the UIKit renderer (stack-view reconcile,
  stale-instance close, unsupported-capability failure, foreground
  coordinator); a shared bounded stream read and action menu in the
  Android renderer; shared modal-dialog and base64 helpers across the
  WebView renderer's modules; and consolidated Swift-compile, simulator
  and app-boot test harnesses in `render/ios`, `internal/simtest`,
  `examples/notes/notesnative` and `cmd/gofastr-native`.
  The tui module exports `widget.SurfaceBox`, previously private.
  Two small deviations: `notesbackup.Stage` now discards its staging
  token when the final directory sync fails (callers already ignored
  it on error), and every executable Swift check in `render/ios` now
  shares one 90-second compile-and-run deadline instead of only the
  reliability check having one.

## v0.1.0 (2026-11-18)

### Phone apps (`gofastr-native`)

- `extension/capture` records and streams Android camera video with GPS,
  camera direction and acceleration. The native renderer's adapter runs in a
  foreground service and keeps recording in the background; the WebView
  adapter uses WebCodecs and pauses there. `whip.Publish` streams over WHIP.
  Recordings are split into segment files, with loop recording under a
  storage budget, locks by hand, from the notification or on an impact, and
  recovery of recordings whose process died. Checked on one Android 14
  emulator only. See [video capture](docs/capture.md).
- `examples/capture` exercises capture on both renderers, and
  `examples/dashcam` is a dash cam with loop recording, impact locks and a
  locked clips list.
- Showing or hiding the camera preview during a recording rebuilds the
  camera session, which leaves the recording without frames for 170 to
  250 ms on the emulator. Leaving the app and coming back costs two such
  pauses.
- Compiled extensions can ask for runtime permissions through
  `GofastrActivity.requestExtensionPermissions`. The answer reaches the
  extension even when Android recreates the activity while the dialog is
  open.
- `whip.Publish` adds a dependency on Pion WebRTC (MIT).
- The Android renderer no longer detaches the visible screen on each live
  refresh. Before, a camera preview on a live screen went black because its
  surface was recreated every update.
- `examples/capture` no longer reloads its Recordings screen nonstop: the
  screen's live version now matches the one its watch compares.
- `examples/chordharp` plays on iOS. The UIKit view draws the same grid,
  strumplate and rhythm bar as the Android view, follows the same touch
  rules (momentary pads, Latch, strums that pluck every zone crossed,
  repeating tempo buttons), and plays the Go synthesizer through an audio
  queue. VoiceOver reaches the bar's controls by name and can play the
  pads and strumplate directly. Checked on the iOS 26.5 simulator in
  portrait with one finger. Not yet checked: landscape, multi-touch,
  VoiceOver itself, the sound by ear, and a physical iPhone.


Capture ran on one Android 14 x86-64 emulator with its virtual scene camera
and simulated GPS and sensors, and the WebView adapter also ran in headless
Chromium. Nothing in this release ran on a physical phone or a real camera.
Paths covered by Go tests or code review only: a second Start while the
device still runs a replaced session, a permission answer after the activity
is recreated, storage write failures on the device, and late audio or camera
callbacks. See [video capture](docs/capture.md) for the full list.

### Terminal apps (`tui`)

No changes. Tagged with the phone module, as every release is.

## v0.0.1 (2026-11-08)

The first tagged release of both modules.

### Phone apps (`gofastr-native`)

- `nativeui`: screens, routes and actions defined in Go, with a runtime that
  owns drafts, saves, presentations and paging. Apps write no Swift, Java,
  HTML or CSS.
- Renderers for UIKit (`render/ios`), Android views (`render/android`) and a
  WebView (`render/webview`), all drawing the same definitions.
- Typed device services: audio, files, notifications, photos, secure storage,
  incoming shares, sharing and widgets.
- `extension`: an API for libraries that add a screen component or device
  adapter, with the built-in `media` and `photos` extensions.
- `mobilehost`: runs an existing GoFastr web app inside the phone app over an
  in-memory listener, or as a service-only host with no transport.
- The `gofastr-native` CLI: `doctor`, `init`, `dev`, `build`, `run`,
  `inspect` and `extension`. It builds with the platforms' command-line tools
  (gomobile, aapt2, apksigner, swiftc, codesign), without an Xcode project or
  Gradle. Release builds sign APKs, AABs and IPAs with keys you supply and
  write `release-report.json`.
- Examples: `starter`, `notes`, `gallery`, `devicefeatures` and `chordharp`.
- Requires gofastr v0.96.0.

Tested on the iOS 26.5 simulator, Android emulators and the Chromium phone
simulator. Not yet run on a physical iPhone or Android phone. See
[docs/design/status.md](docs/design/status.md) for what is and is not
verified.

### Terminal apps (`tui`)

- Raw mode and input decoding for Unix and Windows terminals, including
  bracketed paste, mouse, key repeat and a startup capability probe.
- A diffing cell renderer with an image layer (kitty, iTerm2 and Sixel).
- Unicode 17.0.0 grapheme and width tables (`tui/uni`).
- Layout, a retained view tree, focus handling and an app event loop.
- Widgets: text, inputs and text areas, buttons, lists, tables, tabs, scroll
  and split views, dialogs, notifications and images.
- An in-memory VT emulator for tests (`tui/vt`).
- Demos: `tui-demo` and `netmon`.

Tested against the VT emulator and real ptys on macOS. On Windows it builds
and passes vet but has not been run.

### Repository

- `internal/repocheck` runs with `go test ./...` and fails on local paths,
  tool names, credentials, stale release claims, uncredited third-party
  notices and module layout mistakes.
- `go run ./internal/release check vX.Y.Z` checks a release from a fresh clone,
  including dependency licenses. `publish vX.Y.Z` tags both modules from
  `main` after CI passes, pushes the tags, creates the GitHub release and
  checks the module proxy. See [RELEASING.md](RELEASING.md).
- `scripts/hooks/pre-commit` runs the repocheck test before each commit.
- CI on Linux runs tidy, gofmt, vet and tests for each module, and the WebView
  Node tests, on pull requests and pushes to `main`.
