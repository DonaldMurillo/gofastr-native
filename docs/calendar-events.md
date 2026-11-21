# Calendar events

`device/calendar.Request` is one contract for every platform. Each request
names an operation and is sent as `nativeui.ServiceRequest{Calendar: &request}`
from an explicit action with a registered service continuation.

| Operation | Effect | iOS (EventKit, `--calendar`) | Android | WebView |
| --- | --- | --- | --- | --- |
| `status` | Report authorization without a prompt | Yes | Unsupported | Unsupported |
| `add` | Save `Event` to the default calendar; result has its identifier | Yes | Unsupported | Unsupported |
| `read` | Return up to `Limit` events in `[From, To)` | Yes | Unsupported | Unsupported |
| `edit` | Open the system event editor prefilled with `Event` | Unsupported | Yes | Unsupported |

`edit` exists because Android writes events through the user's calendar app,
not a direct API call. The person may save or discard the event in that editor
and Android does not report which. A completed `edit` therefore means only
that the editor closed: its result carries no identifier, authorization or
events, and a renderer may omit the `calendar` result field entirely.
**Closing the editor does not confirm that the event was saved.**

`calendar/v1` availability does not say which operations a renderer
implements. iOS advertises it when built with `--calendar`; Android advertises
it on every native build because the editor intent needs no permission.
An operation the renderer does not implement returns `unsupported`, so apps
that target both platforms should offer `add` on iOS and `edit` on Android, or
handle `unsupported` with a message.

## Event bounds

Titles are required plain text of at most 200 UTF-8 bytes; locations at most
200 bytes and notes at most 2000 bytes, without control characters. `Start`
and `End` are Go `time.Time` values sent as RFC 3339 timestamps; end may equal
start, and years must be 1 to 9999. Request events must not carry an
identifier. For `edit`, all-day events need `Start` and `End` at midnight in
their own offsets with `End` on a later date; the end date is exclusive.

## Android

Android opens an `ACTION_INSERT` intent for `CalendarContract.Events` with the
title, location, notes, begin and end times and the all-day flag. Timed events
pass the instant Go sent. All-day events pass UTC midnight of each wall-clock
date, which is how Android stores all-day events. No calendar permission is
requested and nothing is written directly. With no calendar app installed the
result is unsupported. Returning from the editor reports completed regardless
of its result code, because calendar apps return the same code after save and
discard.

The gallery Device tab, the Notes editor, and the device-features demo
(**Open calendar editor**) send `edit` requests.

## Acceptance

`go test ./cmd/gofastr-native` compiles the renderer against the Android SDK
and runs its RFC 3339 parser on the JDK against times Go marshals.

An earlier revision of the Android adapter, which received epoch-millisecond
start/end values instead of RFC 3339 timestamps, passed Android API 34
emulator acceptance with Etar 1.0.57: the editor showed the Gallery event title
and a one-hour slot starting at the next full hour, saving to an Etar offline
calendar produced a provider row with the expected title and start/end, and
returning to Gallery showed completed. That emulator run has NOT RUN again on
the current RFC 3339 wire. Google Calendar required account sign-in; its
editor/save acceptance is NOT RUN. Physical-device and accessibility acceptance
are NOT RUN. SDK compilation does not establish those outcomes.

iOS acceptance for `status`, `add` and `read` is described in the
[UIKit service notes](../render/ios/docs/services.md#calendar-events).
