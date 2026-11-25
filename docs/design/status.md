# Status

iOS native apps can opt into `push/v1` with `--push` for explicit APNs
registration, token delivery to Go, no-prompt status, and unregistration.
Validated remote notification routes share local reminders' pending/cold-tap
path; foreground pushes do not navigate. Notes exposes push settings and a
push-tap destination. See [iOS services](../../render/ios/docs/services.md).
On an iOS 26.5 simulator, simulated remote delivery displayed a banner and
foreground delivery did not navigate. A warm default tap opened push settings
in the same process after renderer resume. Local reminder taps share that
resume retry; their warm lifecycle is covered by executable Swift checks, not
a simulator reminder tap.
Status displayed authorized/registered state without an observed device token;
Notes labels that as the OS registration flag with no token received.
On an iOS 27.0 simulator, a cold-launch default tap (new process) opened the
routed note, and a payload with a missing or external route opened the Notes
list without navigating to it.
APNs token issuance: **NOT RUN**.
Real APNs delivery on hardware: **NOT RUN**.

What the project covers today, what has been checked and on what, and what
is left. Updated with the work; the dates are in the [implementation log](agent-notes.md).

## Windows desktop renderer prototype

[Log Explorer](../../examples/logexplorer/) runs a Go-authored workspace
through the GoFastr host. The default Windows renderer draws the workspace
through DirectX. Stacks, split panes, text, buttons, select boxes, tables, and
detail panes can be nested in a Go view tree. Keys retain table scroll,
keyboard focus, and splitter positions across updates and reordering. A paint
node provides clipped custom drawing. The host offers Win32 controls for the
original workspace fields.

The DirectX renderer keeps layout in device-independent pixels and rasterizes
glyphs at the monitor's display scale. At 150% Windows scaling, the graphics
check matched the render buffer to the 1680 × 1050 client area. Windows
Graphics Capture showed sharper labels and editor text in Desktop Notes.

The deterministic model test and the Windows control test passed 500
updates with 50,000 appended rows. The Windows test checked the top row
and selected row at every update. The host test opened and closed a
window through the GoFastr lifecycle. The DirectX check drew a real
window with cgo disabled, verified the selected-row indicator by reading
the back buffer, and preserved the paused viewport as 50,000 rows arrived.
It also moved the table to the opposite pane mid-replay and verified a
custom paint node in the back buffer.

The Windows [Notes editor](../../examples/notes/) composes single-line and
multiline text fields and a checkbox. A switch is available to desktop views
through the same toggle API. Text fields support grapheme-aware movement,
selection, and clipboard actions. A Windows window check entered two lines,
toggled the pin, displayed missing-title validation, and saved through the
shared Notes service. The model test verified that edits made during a save
were preserved and recovered from the draft store. The Write and Details tabs,
Note actions menu, and discard confirmation use the shared desktop components.
The desktop command prevents two processes from opening the same data
directory. The draft store checks the expected revision before writing or
discarding a draft within the process.

The Windows amd64 DirectX window exposes its controls through UI Automation. A
separate client queried the Notes Title and Save controls, set the title using
the Value pattern, and saved the note using the Invoke pattern. A separate
cross-process check selected the Details tab and read the selection from its
tab group. It toggled Pin, expanded the Note actions menu, invoked its discard
item, and confirmed the draft discard. The dialog's cancel button received
focus and Save was absent from the modal accessibility tree. Screen-reader
speech acceptance is NOT RUN. Cross-monitor DPI transitions are NOT RUN.

The GPU renderer has a repeatable hidden-window benchmark
([logbench](../../examples/logexplorer/cmd/logbench/), cgo disabled): it
runs the real DirectX device without showing a window, so automated runs
do not disturb other GUI work. On one Windows 10 workstation, the 10 s
hidden replay drew 500 event-driven frames and measured Update+Draw p99 of
1.30 ms and fixture event to drawn frame p95 of 1.25 ms. It drew one frame
and used 0.31% process CPU over a five-second idle check. A visible replay
passed at 1,050,000 rows with an Update+Draw p99 of 3.00 ms and a fixture
event to drawn frame p95 of 2.00 ms. Readback hashes confirmed changes in the
rendered table in both runs. A separate six-second Windows Graphics Capture
sample recorded 302 distinct consecutive images with a 26.7 ms p95 interval
between image changes. Interactive mode times the focused window's input
poll through the end of Draw. A synthetic key message check produced five
input frames. Physical input-to-display latency is NOT RUN.

The opt-in `GOFASTR_DESKTOP_IME_TEST=1` check exercised synthetic Windows
messages through the real DirectX window, including commit, cancellation,
caret movement, and surrogate-pair input. Microsoft Japanese IME and Pinyin
were registered but disabled in the test account. Candidate selection with
an enabled Windows IME is NOT RUN.

## External presentation prototype

The iOS-only [maps component](../maps.md) adds retained MapKit maps, keyed pins,
route-only callout navigation and purpose-gated when-in-use location. Go validation,
Foundation decoding and simulator compilation are automated; physical location,
device permission UX and VoiceOver acceptance remain NOT RUN.
Simulator XCUITest acceptance passed on iPhone 17/iOS 27.0: three labelled pins,
callout-to-detail navigation and a preauthorized simulated user-location annotation
plus first-time when-in-use authorization across renderer suspension
(four tests, zero failures). Physical-device gates remain.

The iOS-only [AR component](../arkit.md) adds ARKit world tracking, plane
detection, coaching, built-in primitives fixed in space or placed on tapped
surfaces, and route-only object navigation. Go validation, Foundation decoding
and simulator compilation are automated. Simulator XCUITest acceptance passed on
iPhone 17/iOS 26.5 (three tests, zero failures): the unsupported-device status,
and with the `GOFASTR_AR_FIXTURE` surface, fixed-object navigation, tap-to-place,
placed-object navigation and reset. Device world tracking, plane detection,
camera permission UX and VoiceOver remain NOT RUN.

The typed `browser/v1` service opens SFSafariViewController in native iOS,
with optional Reader mode and dismiss label. Starter includes About → Open docs
and a returned-status screen. Android and WebView advertise unavailable.
Go validation and Swift wire checks pass. The opt-in XCTest UI test passed three
consecutive runs on iOS 26.5: it verifies Safari presentation, Done dismissal and
the Go completed continuation screen. The test waits for hittable Safari chrome
and taps the Done control's center coordinate. Physical-device and VoiceOver
acceptance: NOT RUN.

The [typed barcode scanner](../scanner.md) adds `scanner/v1` to native iOS and a
**Scan code** action to the device-features example. Simulator XCUITests cover
the QR image/Vision fixture and cancellation through the real service wire.
Hardware camera acceptance: **NOT RUN**; simulator checks do not establish
physical barcode, permission, interruption or accessibility acceptance.
Native Android serves the same `scanner/v1` request through the opt-in
`extension/barcode` adapter, which decodes one camera photo or selected image
offline in Go. Go decoder tests and the Android SDK compile check pass;
emulator and hardware runs are **NOT RUN**. Android cannot read PDF417 or
Aztec. WebView has no scanner adapter.

The mobile Notes bindings now consume one Go screen/action package,
`notesscreens`, through either UIKit or the generic WebView renderer. The latter
uses the external `mobileui` design system with iOS/Android modes, not desktop
entityui screens at phone width. Both renderers support list/editor/About,
search, create/edit, explicit saves, validation, and draft recovery, with
documented platform differences.

Notes is the main application example. Its **Behaviors** screen adds
Go-authored alerts, confirmations, menus, dialogs, sheets, and single/multiple
selection through both renderers. Native/WebView packages are thin launch
adapters, not separate example products. See [Go presentation behaviors](../presentation-behaviors.md).

The same application also has **Files**: import text/Markdown notes, export a
note's current buffer, and add/export/remove private attachments on saved notes.
These use [typed Go file services](../files-services.md), with a 1 MiB transfer
limit, native iOS document pickers, and an explicit mobile-WebView fallback.
Saved-note attachments also offer **Choose photo** and **Take photo** through
[typed photo services](../photos-services.md), normalizing to bounded JPEGs
without original EXIF/GPS metadata. Native camera acceptance requires a real
device; the WebView path is an explicitly disclosed chooser fallback.
Photo acquisition is the first [compiled extension](../compiled-extensions.md):
Notes registers it in Go; its UIKit/WebView adapters belong to the library package.
Photos display inside the note and open a larger attachment preview. Both
renderers share the same Go [image preview definitions](../image-previews.md).

Notes also exposes typed reminder scheduling/status/cancellation, rich note and
attachment sharing, explicit clipboard writes, and private voice recordings.
On Android, Notes also opens a user-controlled calendar event editor through
the calendar/v1 `edit` operation; closing it does not confirm saving. iOS and
WebView return unsupported for `edit`. Etar 1.0.57 on an Android API 34 emulator
accepted an earlier epoch-millisecond revision of this request (expected title
and next-hour slot, offline-calendar save with matching provider values,
completed outcome); the current RFC 3339 wire has NOT RUN on an emulator.
Google Calendar editor/save and accessibility acceptance are NOT RUN.
Android builds can opt into FCM [push notifications](../push.md) with
`--push-config` and `--push-libraries`. They share the iOS `device/push`
contract (`State.Provider` is `fcm`) and add a private message inbox read with
`next` and settled with `acknowledge`. Token issue and delivery through
Firebase servers are NOT RUN.
Collections now page in the owner-scoped repository beyond 200 records and
watch confirmed changes from the shared service. See [device services and
acceptance limits](mobile-services.md). Native Android uses the same Go
protocol and screens. See [Windows and Android builds and acceptance](../android.md).

[Video capture](../capture.md) records and streams Android camera video with GPS
and camera direction. Under the native renderer it keeps running in the
background through a foreground service. Under the WebView renderer the page
captures with WebCodecs and pauses in the background. Live streaming uses WHIP.
Recordings are split into segment files, with loop recording under a storage
budget and locks by hand or on an impact. A killed process loses only the
segment being written, which without segments is the whole recording.
`examples/capture` exercises both renderers, and
`examples/dashcam` is a dash cam on the native renderer. All have run on one
Android 14 emulator only. The WebView adapter has not recorded microphone
audio or cut segments on Android. There is no iOS adapter.

The WebView renderer live-refreshes screens without a collection too: a data
change reads the screen's markup again and swaps it into place, keeping
mounted components, focus and scroll.

Native Android implements the same [location](../location.md) contract with
`LocationManager`, opted in with `--location`: status without a prompt,
when-in-use authorization, one current fix with a deadline, and bounded
tracking. It declares coarse and fine location only and cancels when the
activity pauses, except during the permission dialog. SDK compilation against
the generated bindings is automated. Emulator runs, permission dialogs,
real providers and physical-device acceptance for Android location are NOT RUN.

[Incoming shares, secure storage with biometrics, and home-screen widgets](../device-features.md)
also use shared Go requests with Android and iOS adapters. The Go-only
`examples/devicefeatures/featuresnative` demo exercises their opt-in APIs.
Its Android build also exercises [opening web URLs and receiving Android App
Links](../android.md#opening-web-urls-and-receiving-android-app-links).

[Device and battery snapshots](../deviceinfo.md) use typed `deviceinfo/v1`
requests on native Android without permissions or opt-in flags. The devicefeatures
demo displays both snapshots. iOS and WebView return unsupported and do not
advertise support. Android 14 emulator acceptance compared model/API with system
properties and displayed an injected 42% AC-charging battery snapshot.
Physical-device acceptance is NOT RUN.

[Location](../location.md) (`device/location`) defines typed permission, current-position and bounded
foreground tracking requests, with `location/v1` protocol negotiation. Native
CoreLocation support is opt-in through `--location --location-usage`, with
when-in-use authorization only and no background updates. Swift wire and
simulator SDK compilation checks cover the adapter. The device-features demo
shows status, authorization and position results with stable accessibility IDs.
`GOFASTR_IOS_UI=1 GOWORK=off go test -v -count=1 -timeout=20m ./internal/iosuitest`
builds and installs the demo, boots a fresh simulator, grants when-in-use
location, sets simulated coordinates and exercises all four actions through
the UI. iOS 26.5 simulator acceptance passed, including visible coordinates
for current position and duration-bounded tracking. First-use permission
prompts, denial UI and physical-device checks are NOT RUN.

The same example demonstrates native UIKit
[Sign in with Apple and OAuth](../authentication.md). Android and WebView
do not provide these sign-in services. Successful login requires app and
provider configuration; the simulator demo only exercises system UI and
graceful cancellation or failure.

[Background refresh](../background-refresh.md) schedules periodic Go work
through `BGTaskScheduler` on iOS and a persisted periodic `JobScheduler` job on
Android. iOS simulator builds and contract tests pass; a physical-device run is
NOT RUN. The Android adapter compiles against the SDK and the generated gobind
classes, and CLI tests cover its manifest and generated service. An Android
emulator run of this adapter is NOT RUN. An earlier version, with per-job
identifiers and its own Go API, passed forced runs on an Android 14 x86-64
emulator, including one in a service-only process after `am kill` and schedule
persistence across reboot. Unforced timing, Doze, API 24 runtime behavior,
physical devices and accessibility are NOT RUN. WebView is unsupported.
[In-app purchases and subscriptions](../iap.md) use StoreKit 2 in the native
iOS renderer. Android and WebView explicitly report the service as unsupported.
The storefront demo includes a local StoreKit catalog and simulator UI tests.

[Network status](../network-status.md) has an opt-in Android native adapter and
a device-features demo action. It reports one snapshot, with no subscriptions.
iOS and WebView are not implemented.

[Speech services](../android.md#speech) provide foreground text-to-speech and
single-utterance dictation through the native Android renderer. UIKit and
WebView do not have these adapters.

The [Notes integrations](../../examples/notes/docs/device-features.md) add reviewed shared
imports, explicitly selected note widgets, and biometric-protected text notes.

[Portable Notes workflows](../../examples/notes/docs/portable-workflows.md) add reviewed backup
and restore, matching search passages, bulk selection, and guarded editor saves.

The same Notes app now exercises [mobile components](mobile-components.md):
typed toggles/selects/date/time, filters and accumulated Load more, stable-ID
multi-selection and bulk actions with undo, Notes/Files/Settings tabs, and row
swipes with labelled menu alternatives. UIKit retains a navigation stack per tab;
WebView retains each tab's last route/scroll/focus within GoFastr's shared history.
The [mobile document design](../../examples/notes/docs/design.md) keeps writing first, puts
photos/files inside the note, and gives the Files tab a real attachment library
with readable metadata and reusable previews. Styling stays in renderer libraries;
the example app still authors only Go screen definitions.
Reminder fields are metadata; scheduling is an explicit device-service action.
Notes paging is repository-backed; the Files library currently covers the 200
most recent active notes. Optional native iOS custom links use `--url-scheme notes-demo`.

Existing whole-GoFastr-web-app hosting remains independent and supported.
`notesweb` preserves that handwritten web presentation; the terminal adapter
reuses the same typed service without implementing the mobile screen contract.

The native prototype uses Go-authored `nativeui` screens and a reusable, bundled
UIKit or Android renderer selected by `--renderer native`. Apps do not write platform templates
or wire JSON. No GoFastr code or production dependencies changed.
UIKit now composes services/installation data without mounting unused web
screens. It uses `App.Run(ctx, framework.ServicesHost())` for the complete
listener-free lifecycle and waits on `App.ServicesReady()`. Shared editor
policy, durable draft storage, capabilities and observed effect receipts live in [mobile runtime services](../mobile-runtime.md).
See [the architecture, public APIs, validation, and limitations](presentation-prototype.md)
and [the native host contract](../native-presentation.md), plus
[implementation notes](agent-notes.md).

The [shared-screen implementation](../shared-mobile-screens.md) documents the
working APIs and limits. The [mobile presentation roadmap](mobile-roadmap.md)
tracks remaining shared headless task behaviors, async correctness, and explicit
platform capabilities. Sharing definitions is not yet behavior parity.

The [shared reliability layer](mobile-reliability.md) gates actions/departure
on durable drafts, guards app history Back/Forward, checkpoints on backgrounding,
and recovers retired live reads without replaying writes or replacing inputs.

## What has been verified

- **Shared Go definitions in real WKWebView (iOS 26.5 simulator).** Fresh bind,
  build, install, and launch; New, invalid/corrected save, retained validation
  fields, reopen, search, and About dismissal use `notesscreens` over the bridge.
  A window marker survives the flow, proving same-document client navigation.
- **Shared-screen browser acceptance, both modes.** Real in-memory host/bridge:
  validation and persistence, editable pending saves/caret retention, canonical
  create identity without duplicate records, durable Go draft reload, unsaved
  share fallback, keep/discard drafts, search, About, light/dark, and 320px/200% text. The independent
  legacy web presentation still has its existing regression coverage.
- **Real UIKit Notes (iOS 26.5 simulator).** Native list/search, validation,
  create/save/reopen, durable local draft restoration, dirty Back, system
  keyboard avoidance, user-initiated share sheet, and medium/large native sheet.
  Go command tests also open native-created data in web and terminal presenters.
- **iOS simulator (iOS 26.5, Xcode on macOS).** The CLI builds and launches
  the notes app. Driven through the eval hook in the real `WKWebView`: the
  transport is installed and finds the `gofastr` handler, client navigation
  to the create form keeps window state, a note save goes over the bridge and
  the list shows it, and a native multipart form posts over the bridge and
  its answer becomes the next document.
- **Chromium phone sim, both modes.** Handwritten mobile screens, client-side
  navigation and saves (`POST /notes/save`), editing, validation, session-draft
  restoration, search, light/dark, and 200% text at 320px. A native multipart
  form's `303` is followed in Go. In Android mode every intercepted body is
  stripped; saves and forms still work through the bridge. This is not an
  Android-device or screen-reader test.
- **Android shell compiles.** `MainActivity.java` compiles against gobind's
  generated bindings and the stubs.
- **Single-user identity.** Rows carry a per-install owner id that survives a
  restart on the same data directory.
- **Streaming.** `Host.Open` returns headers before the body ends and hands
  each flushed chunk over as it is written (Go test).

## Not verified yet

- Android beyond one Android 14 x86-64 emulator: ARM64 devices, other API
  levels, TalkBack, and physical camera and microphone. That includes video
  capture and WHIP streaming over the public internet. The emulator runs
  that did happen are in [Android builds and acceptance](../android.md).
- A physical iPhone. `--platform ios-device` builds a signed app (see
  [release builds](../release.md)), but install and launch on a registered
  iPhone has not been run.
- On device: whether `gofastr://app` is a secure context (Web Crypto,
  service workers) and whether `localStorage` persists.

## Known gaps

- **The desktop contract does not fit a phone.** `desktop.Shell` assumes a
  process-owned event loop, windows, a tray and menus. What mobile wants is
  the OS-neutral half (the capability registry, grants, the bridge chokepoint,
  app state), and that lives inside `battery/desktop`.
- **Two internal helpers are copied.** `cmd/gofastr-native` copies
  `internal/fileperm.WriteOwnerOnly`, and `internal/simtest/` spells out the Chrome start
  timeouts that `internal/chromedptest` holds.
- **The adaptive theme is opt-in.** The external Notes presentation selects
  `mobileui.Theme(platform)`; a scaffold must also choose its presentation.
- **The framework's 404 page has no styles.** On the phone it renders as bare
  HTML.
- **Service-only hosts have no HTTP transport.** UIKit mounts no unused web
  screens and uses the listener-free lifecycle. Framework HTTP routes stay
  configured but unused. WebView hosts still use in-memory `App.Serve`. See
  [Gate A and Gate B](../mobile-lifecycle.md).
- **Some async acceptance gaps remain.** App-tagged browser history and explicit
  app departure now await Go acknowledgement. Untagged history fails closed;
  immediate termination can still outrun checkpoints. Keyed list updates,
  real-device lifecycle/IME/accessibility runs and crash-safe mutation
  reconciliation remain work.

## Next steps

Calendar events: bounded `device/calendar` add/read/status/edit contract. The
opt-in iOS EventKit adapter implements status/add/read; Android implements only
`edit`, which opens the system calendar editor without confirming a save (see
[calendar events](../calendar-events.md)). The device-features example passed
an iPhone 17 / iOS 26.5 simulator UI test with pregranted calendar access:
add a sample event, report full authorization, and show its title/start after
reading the next seven days. Permission prompts/hardware acceptance: NOT RUN.

Follow the [milestones and acceptance gates](mobile-roadmap.md#build-order-and-acceptance-gates):

1. Finish the remaining M2 Back/IME/accessibility gates over the shared editor/runtime.
2. Preserve the tested listener-free lifecycle and WebView shutdown boundaries; do not copy framework startup.
3. Run the Android gates [Android builds and acceptance](../android.md) leaves open; preserve WebView support.
4. Expand task-shaped components/capabilities, then extensions, scaffolding, device
   signing, and measured optimization. Re-pin to framework releases as required
   public seams become available.

The [component slice](mobile-components.md) implements the first controls,
collection, tab, and swipe contracts; uncapped repository paging, richer sheets,
and complete device/accessibility acceptance remain roadmap work.

## Contact picker

The [contact picker](../contacts-services.md) works through `contacts/v1` on iOS and the
native Android renderer. iOS supports explicit single or multiple selection
with requested fields only; its native and WebView hosts reuse the service
runner. No Contacts authorization or `NSContactsUsageDescription` is needed
for the system picker. iOS native picker GUI acceptance: NOT RUN.

Android uses the system phone or email row picker without `READ_CONTACTS` and
serves single selection of one `phone` or `email` field; other requests,
including Notes' **Pick contacts**, settle as `unsupported`. The display name
arrives in `GivenName`. Gallery's Device tab has a **Choose contact** demo.
An Android 15 arm64 emulator run passed phone selection and display, Back
cancellation and reopening, using a local Gallery build with the iOS-only map
screen stubbed out. Email selection, denied access, physical devices and
accessibility: NOT RUN. The Android WebView renderer does not advertise
`contacts/v1`.
