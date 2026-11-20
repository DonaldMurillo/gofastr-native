# Calendar events

`device/calendar.Event` supplies defaults for a user-controlled calendar editor.
Use `calendar.New(title, start, end)` with Go `time.Time` values; the wire
format uses `startMillis` and `endMillis` epoch milliseconds.

Titles are required. Titles and locations allow at most 200 UTF-8 bytes without
control characters. Notes allow 4 KiB of UTF-8 without NUL. Start must be
nonzero, end must follow start, and the span cannot exceed 366 days. All-day
events require UTC-midnight boundaries; the end boundary is exclusive.

Return a `nativeui.ServiceRequest{ID: "calendar", CalendarEvent: &event}`
from an explicit action and register its service continuation.

Android advertises `calendar/v1` as available and opens an ACTION_INSERT intent
for CalendarContract.Events. No calendar permission is requested and no event
is written directly. With no calendar app installed the result is unsupported.
Returning from the editor reports completed regardless of its result code:
**closing the editor does not confirm that the event was saved**.
Results contain only status and an explanatory message, never calendar content.
iOS and WebView advertise unavailable and return unsupported.

The gallery Device tab and Notes editor include Add to calendar actions.
Android API 34 emulator acceptance with Etar 1.0.57 verified the Gallery event
title and a one-hour slot starting at the next full hour. Saving to an Etar
offline calendar produced a calendar-provider row with the expected title and
epoch-millisecond start/end values. Returning to Gallery showed completed.
Google Calendar required account sign-in; its editor/save acceptance is NOT RUN.
Physical-device and accessibility acceptance are NOT RUN. SDK compilation does
not establish those outcomes.
