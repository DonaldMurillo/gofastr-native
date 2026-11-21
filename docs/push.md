# Push notifications

Remote push uses one Go contract, `device/push`, on both platforms: APNs on
iOS and Firebase Cloud Messaging on Android. Both are opt-in. Local reminders
(`ServiceRequest.Notification`) are separate and need neither.

This page covers Android. iOS builds use `--push`; see
[iOS services](../render/ios/docs/services.md#push-notifications-ios) for
entitlements, the app-delegate callbacks and simulator checks.

## Licensing

The Firebase and Google Play services libraries are under the
[Android Software Development Kit License](https://developer.android.com/studio/terms)
and Firebase/Google APIs terms, not an open-source license. This repository
does not commit, embed or redistribute them. The app owner downloads a pinned
set once and accepts those terms:

```sh
go run ./cmd/gofastr-native push-libraries --out "$HOME/.cache/gofastr-fcm" --accept-licenses
```

The command fetches `com.google.firebase:firebase-messaging:26.0.2` and its 73
runtime dependencies from Google's Maven repository and Google's Maven Central
mirror, and checks every file against a SHA-256 pinned in
`cmd/gofastr-native/push_libraries.go`. The AndroidX, Kotlin and okio libraries
in the set are Apache-2.0. An APK built with push contains these libraries;
distributing it is subject to their terms.

## Build

Register the Android application id in the Firebase console and download its
`google-services.json`. The file names the project; it is not a server
credential, but keep it out of public repositories.

```sh
go run ./cmd/gofastr-native build --platform android-emu --renderer native \
  --pkg ./examples/devicefeatures/featuresnative \
  --push-config /path/to/google-services.json \
  --push-libraries "$HOME/.cache/gofastr-fcm" --out dist/mobile/device-features
```

There is no Gradle step. The build verifies the hashes, unpacks the AARs,
merges their manifest permissions and components (including Firebase's
component registrars), turns the matching `google-services.json` client into
the string resources the Firebase Gradle plugin would generate, compiles the
library resources, generates their R classes, and links all classes into the
APK, splitting dex files when needed. Without both flags the APK has no
Firebase code and `push/v1` reports unavailable. The flags are rejected for
iOS builds, which use `--push` instead.

## Go contract

`ServiceRequest.Push` carries a `push.Request`. A completed result carries a
`push.State` in `ServiceResult.Push`: `Permission`, `Registered`, an optional
`Token`, and `Provider`, which names the service that issued the token
(`apns` or `fcm`). Go rejects a token without a provider, an APNs token that
is not lowercase hex, and an APNs `Environment` on an FCM state.

| Operation | Android (FCM) | iOS (APNs) |
| --- | --- | --- |
| `register` | Asks for notification permission on Android 13+, then returns the FCM token. | Asks for permission, then returns the APNs token. |
| `status` | The last token Android issued, when registered, and the notification permission. | Permission, OS registration, and the token observed in this process. |
| `unregister` | Deletes the FCM token. | Stops remote registration. |
| `next` | `State.Message`, the oldest unacknowledged message, or none. | Unsupported. |
| `acknowledge` | Removes the message with that inbox identifier and its notification. | Unsupported. |

Android reports permission as `authorized` or `denied` from whether the app
may post notifications; it has no `notDetermined` state before the Android 13
prompt. Tokens rotate. Register when the app starts and send the server any
token that changed. Go validates every token and message before a handler
sees it.

## Messages

Send data messages with `title`, `body` and an optional `gofastr.route` key,
the same route key APNs payloads use on iOS, plus up to 32 other string keys
within the 4 KiB FCM payload. The app stores each message
in a private inbox of the newest 32 and shows a notification on the
`gofastr.push` channel when it has text and notifications are allowed. Tapping
it opens `gofastr.route` through the same local-path guard as deep links. A repeated
`next` returns the same message until it is acknowledged, so an app can save it
under its stable identifier first.

Notification messages behave differently when the app is in the background:
Android's FCM client shows them itself and opening one starts the app without
storing it. The launcher activity is exported, so its extras cannot prove that
FCM sent them and are not read into the inbox. Use data messages when every
message must reach the inbox.

## Validation

Go tests cover the request, state, token and message bounds for both
providers, manifest merging, `google-services.json` parsing and multi-dex
packaging.

On November 20, 2026 the device-features demo was built with push for x86-64
and installed on an Android 14 Google APIs emulator with a placeholder Firebase
configuration. FirebaseApp initialized. A data message injected into the FCM
receiver with `adb root` was stored, shown as a notification, opened the
`/features/push` route when tapped, was returned by `next`, and was removed by
`acknowledge`. Registration failed, as expected without a real Firebase project
and with the emulator's out-of-date Google Play services. That run predates the
shared iOS contract: it used the earlier `route` data key and result shape.
The emulator check has not been repeated with `gofastr.route` and
`push.State`: NOT RUN.

NOT RUN: token issue and delivery through Firebase's servers, background and
killed-app delivery, token rotation, and any physical device.
