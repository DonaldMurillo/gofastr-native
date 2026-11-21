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

## External presentation prototype

The iOS-only [maps component](../maps.md) adds retained MapKit maps, keyed pins,
route-only callout navigation and purpose-gated when-in-use location. Go validation,
Foundation decoding and simulator compilation are automated; physical location,
device permission UX and VoiceOver acceptance remain NOT RUN.
Simulator XCUITest acceptance passed on iPhone 17/iOS 27.0: three labelled pins,
callout-to-detail navigation and a preauthorized simulated user-location annotation
plus first-time when-in-use authorization across renderer suspension
(four tests, zero failures). Physical-device gates remain.

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

[Incoming shares, secure storage with biometrics, and home-screen widgets](../device-features.md)
also use shared Go requests with Android and iOS adapters. The Go-only
`examples/devicefeatures/featuresnative` demo exercises their opt-in APIs.

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
through `BGTaskScheduler` on iOS. It has no Android adapter. Simulator builds
and contract tests pass; a physical-device run is NOT RUN.
[In-app purchases and subscriptions](../iap.md) use StoreKit 2 in the native
iOS renderer. Android and WebView explicitly report the service as unsupported.
The storefront demo includes a local StoreKit catalog and simulator UI tests.

[Network status](../network-status.md) has an opt-in Android native adapter and
a device-features demo action. It reports one snapshot, with no subscriptions.
iOS and WebView are not implemented.

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

Calendar events: bounded `device/calendar` add/read/status contract and opt-in
iOS EventKit adapter; Android unavailable. The device-features example passed
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

The iOS [contact picker](../contacts-services.md) supports explicit single or multiple
selection with requested fields only, through `contacts/v1`. The native and
WebView hosts reuse the service runner. No Contacts authorization or
`NSContactsUsageDescription` is needed for the system picker. Android does
not advertise this capability. Native picker GUI acceptance: NOT RUN.
