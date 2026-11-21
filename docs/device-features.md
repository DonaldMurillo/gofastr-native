# Shared native device features

[Network status](network-status.md) is an Android-only, opt-in one-shot service.
The device-features demo includes a network query; enable it with `--network-status`.

Share-in, secure storage with biometric authentication, and home-screen widgets
use the same Go contracts on Android and iOS. The app authors requests and
handles typed outcomes in `nativeui.Route.Services`. The platform adapters own
OS prompts, storage, intents, and extension processes. All three are opt-in.

| Feature | Go request | Android adapter | iOS adapter |
| --- | --- | --- | --- |
| In-app browser | `ServiceRequest.Browser` | Unavailable | SFSafariViewController (`browser/v1`) |
| Receive shares | `ServiceRequest.IncomingShare` | SEND intents and a private inbox | Share extension and App Group inbox |
| Secrets and authentication | `ServiceRequest.SecureStorage` | Keystore AES-GCM and BiometricPrompt | Keychain and LocalAuthentication |
| Home-screen summary | `ServiceRequest.Widget` | AppWidgetProvider and RemoteViews | WidgetKit extension and App Group snapshot |
| Foreground location | `ServiceRequest.Location` | LocationManager, coarse/fine runtime permission, bounded updates | CoreLocation, when-in-use authorization, bounded updates |
| Barcode scanner | `ServiceRequest.Scan` | Not implemented | One-shot AVFoundation sheet; see [scanner](scanner.md) |
| Network status | `ServiceRequest.Network` | One-shot ConnectivityManager snapshot (`network/v1`); see [network status](network-status.md) | Not implemented |
| Calendar events | `ServiceRequest.Calendar` | `edit`: system editor intent, save not confirmed | `status`/`add`/`read`: EventKit (`--calendar`); see [calendar events](calendar-events.md) |
| Remote push | `ServiceRequest.Push` | Firebase Cloud Messaging and a private inbox (`--push-config`, `--push-libraries`) | APNs registration and tap routing (`--push`); no inbox |
| Speech | `ServiceRequest.Speech` | TextToSpeech and one-utterance SpeechRecognizer (`text-to-speech/v1`, `speech-recognition/v1`); see [speech](android.md#speech) | Not implemented; the runtime refuses the request |

[Location](location.md) is an additional opt-in service on iOS and Android.
Build the demo with `--location --location-usage "Show nearby places in the foreground."`
to enable its Location section (`--location-usage` is required on iOS and
optional on Android). It reports permission, current position and up to five
samples over five seconds; it never requests background access.
The other three features remain shared across Android and iOS.

The browser is a separate, user-initiated service in `device/browser`. It accepts
only bounded absolute HTTP/HTTPS URLs without credentials. Reader mode and
Done/Close/Cancel labels are optional; dismissal reports completed, not page-load
success. Android and WebView do not fall back to opening an external browser.
Try **About → Open docs** in the native Starter example. See the
[UIKit service notes](../render/ios/docs/services.md#in-app-browser).

The same Go-only share-in, storage and widgets demo runs on both platforms:

```powershell
go run ./cmd/gofastr-native build --platform android-emu --renderer native --pkg ./examples/devicefeatures/featuresnative --receive-shares --secure-storage --biometric-usage 'Authenticate to use the demo secret.' --widgets --url-scheme gofastr-features --out dist/mobile/device-features
```

On a Mac, use `--platform ios-sim` and add
`--app-group group.dev.example.devicefeatures`. The CLI compiles and embeds the
share and WidgetKit executables separately under `PlugIns/*.appex`, with a shared
App Group entitlement. Neither extension links the Go runtime. The host's
Keychain group stays private to the host. App Group names must match the
registered group and provisioning profiles for a physical device. The current
CLI builds simulator apps with ad-hoc signing; physical signing is still a
separate release step.

The demo reviews or discards incoming shares. It does not import them into Notes.
For an app import, save using the stable incoming identifier for deduplication,
then acknowledge it in a separate service. A repeated read is intentionally safe.

The secure demo uses fixed test bytes and displays only the byte count.
Biometric authentication does not encrypt an existing database. Android
device-protected storage works from API 24; crypto-bound biometric operations
require API 28 or newer. No passcode fallback releases a biometric secret.

Widgets display only explicitly published public snapshots. Add the widget
through the system launcher/gallery after publishing. Updating the stored
snapshot requests a system reload; the completion does not prove that the OS
has displayed it. Clear the snapshot when signing out or removing its content.

Push uses one `device/push` contract on both platforms. `State.Provider` says
whether APNs or FCM issued the token. Android push needs a Firebase project and
libraries the owner downloads under Google's terms, and keeps received messages
in an inbox the demo's Push notifications screen reads and acknowledges. iOS
has no inbox and reports `next` and `acknowledge` unsupported. See
[push notifications](push.md).

See [share-in](share-in.md), [secure storage](secure-storage.md), and
[widgets](widgets.md) for bounds, platform behavior, and validation gates.

## Acceptance

`scripts/android_features_smoke.py` builds a small test sender and AppWidgetHost
and installs a debuggable copy of the demo on an explicitly selected emulator.
It never clears app data. Private-file inspection and deliberate corruption
apply only to the fixed demo secret in that test app. The delivered demo APK
does not enable debugging. The script checks encrypted storage, restart and
tamper handling, bounded incoming shares with real provider grants, explicit
acknowledgement, widget display, deep links, and clearing.

With a virtual fingerprint already enrolled as emulator ID 1, add
`--biometrics-only` to check crypto-bound reads and writes, cancellation, and
backgrounding. That mode does not change enrollment or lock settings. Physical
biometric hardware, enrollment invalidation, lockout, and iOS acceptance remain
separate gates.

Windows checks include the Go suite, vet, Android SDK compilation/resource
linking, actual Android 14 x86-64 behavior, and the existing WebView tests.
macOS tests compile and execute Foundation wire fixtures and compile/sign both
iOS extensions against the simulator SDK. These tests skip on Windows, so the
iOS implementation still requires a Mac build and runtime acceptance.

For location, `GOFASTR_IOS_UI=1 GOWORK=off go test -v -count=1 -timeout=20m ./internal/iosuitest`
builds the demo, boots a fresh simulator, grants location permission, sets a
simulated position and drives all four actions through the UI. iOS 26.5
simulator acceptance passed with visible current and tracking coordinates.
First-use prompts, denial UI and physical-device location checks are NOT RUN;
see [location acceptance limits](location.md#checks-and-acceptance). Android
location has SDK compilation checks only; emulator and device runs are NOT RUN.

## Contact picker

The iOS [contact picker](contacts-services.md) supports explicit single or multiple
selection with requested fields only, through `contacts/v1`. The native and
WebView hosts reuse the service runner. No Contacts authorization or
`NSContactsUsageDescription` is needed for the system picker. Android does
not advertise this capability. Native picker GUI acceptance: NOT RUN.
