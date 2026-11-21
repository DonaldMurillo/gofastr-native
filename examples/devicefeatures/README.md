# Native device features

`featuresnative` demonstrates incoming shares, secure storage, widgets, and
calendar events using shared Go service contracts. Enable each service with
its build flag; disabled services return unsupported rather than requesting
permission. Calendar status, add and read are iOS-only. On Android, **Open
calendar editor** opens the system calendar editor; closing it does not
confirm a save, and the other calendar actions return unsupported.

## Calendar simulator build

```sh
GOWORK=off go run ./cmd/gofastr-native build --platform ios-sim \
  --renderer native --pkg ./examples/devicefeatures/featuresnative \
  --calendar --out "$HOME/calendar-build"
```

`--calendar-usage` optionally sets the calendar purpose text. Install
`Featuresnative.app` on a compatible simulator, open **Calendar events**, then:

- **Add sample event** saves a one-hour `GoFastr sample event` tomorrow.
- **Calendar status** (under More actions) reports authorization without prompting.
- **Read next 7 days** (under More actions) shows returned titles and RFC3339
  start timestamps in the on-screen status area, or an explicit empty result.

Action IDs are `calendar-add`, `calendar-status`, and `calendar-read`;
the native result accessibility ID remains `native.status`.

## Opt-in simulator UI acceptance

There is no existing iOS simulator interaction runner. The calendar smoke
script follows the Android smoke-script convention, using an isolated
standalone XCUITest runner against the unmodified app:

```sh
GOFASTR_IOS_CALENDAR_UI=1 python3 scripts/ios_calendar_smoke.py \
  --app "$HOME/calendar-build/Featuresnative.app" \
  --device <booted-simulator-UDID> --out "$HOME/calendar-build/ui-tests"
```

Select a dedicated simulator using `xcrun simctl list devices booted`.
The script skips without opt-in, Xcode, or an available booted selected device.
It installs the app, runs `simctl privacy <udid> grant calendar <bundle-id>`,
then taps Add, verifies full authorization, and reads the sample event back.
It does not erase app data or remove calendar events; each run adds a sample.
Xcode result bundles retain the read-result screenshot. This checks pregranted
access, not the OS permission prompt or hardware behavior.
