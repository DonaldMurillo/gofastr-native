# Status

What the project covers today, what has been checked and on what, and what
is left. Updated with the work; the dates are in the [implementation log](agent-notes.md).

## External presentation prototype

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

[Incoming shares, secure storage with biometrics, and home-screen widgets](../device-features.md)
also use shared Go requests with Android and iOS adapters. The Go-only
`examples/devicefeatures/featuresnative` demo exercises their opt-in APIs.

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
  levels, TalkBack, and physical camera and microphone. The emulator runs
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
