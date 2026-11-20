# Push notifications (Android FCM)

Remote push uses Firebase Cloud Messaging on Android. It is opt-in and needs a
Firebase project. Local reminders (`ServiceRequest.Notification`) are separate
and need neither.

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
Firebase code and `push/v1` reports unavailable. iOS push (APNs) is not
implemented; the flags are rejected for iOS builds.

## Go contract

`ServiceRequest.Push` carries a `push.Request`:

| Operation | Result |
| --- | --- |
| `register` | `ServiceResult.Push.Token`, the FCM token for your server. Asks for notification permission on Android 13+. |
| `status` | The last token Android issued, when registered, and the notification permission. |
| `unregister` | Deletes the FCM token. |
| `next` | `ServiceResult.Push.Message`, the oldest unacknowledged message, or none. |
| `acknowledge` | Removes the message with that inbox identifier and its notification. |

Tokens rotate. Register when the app starts and send the server any token that
changed. Go validates every token and message before a handler sees it.

## Messages

Send data messages with `title`, `body` and an optional `route` key, plus up to
32 other string keys within the 4 KiB FCM payload. The app stores each message
in a private inbox of the newest 32 and shows a notification on the
`gofastr.push` channel when it has text and notifications are allowed. Tapping
it opens `route` through the same local-path guard as deep links. A repeated
`next` returns the same message until it is acknowledged, so an app can save it
under its stable identifier first.

Notification messages behave differently when the app is in the background:
Android's FCM client shows them itself, and their data reaches the inbox only
when the user opens that notification. Use data messages when every message
must reach the inbox.

## Validation

Go tests cover the request, token and message bounds, manifest merging,
`google-services.json` parsing and multi-dex packaging.

On November 20, 2026 the device-features demo was built with push for x86-64
and installed on an Android 14 Google APIs emulator with a placeholder Firebase
configuration. FirebaseApp initialized. A data message injected into the FCM
receiver with `adb root` was stored, shown as a notification, opened the
`/features/push` route when tapped, was returned by `next`, and was removed by
`acknowledge`. Registration failed, as expected without a real Firebase project
and with the emulator's out-of-date Google Play services.

NOT RUN: token issue and delivery through Firebase's servers, background and
killed-app delivery, token rotation, and any physical device.
