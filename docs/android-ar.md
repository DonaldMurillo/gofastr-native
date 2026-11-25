# Local augmented reality on Android

`ar/v1` is an opt-in native Android service. It is not available in the
iOS or WebView renderers. The default Android build does not include the
ARCore SDK, its resources or native libraries, or the AR camera permission.

## Enable it

Review the [Google APIs Terms of Service](https://developers.google.com/terms),
the [ARCore Additional Terms of Service](https://developers.google.com/ar/develop/terms),
and the [user privacy requirements](https://developers.google.com/ar/develop/privacy-requirements)
before choosing to use the SDK. This repository does not vendor the SDK or
Google Play Services for AR. After accepting the applicable terms, download
the [pinned ARCore 1.57.0 AAR](https://dl.google.com/dl/android/maven2/com/google/ar/core/1.57.0/core-1.57.0.aar)
from Google Maven and pass its location explicitly:

```sh
GOWORK=off go run ./cmd/gofastr-native build --platform android-emu \
  --renderer native --pkg ./examples/ar/arnative \
  --arcore-aar /path/to/core-1.57.0.aar --out dist/android-ar
```

The build verifies the AAR SHA-256 and packages its classes, resources and
native libraries for the selected ABIs. The APK declares AR Optional, so
non-AR devices can still use the rest of the app. On a supported device
Google Play Services for AR must be installed or updated separately; the
placement action invokes Google's install flow when needed, then requests
camera permission. Test on an ARCore-compatible Android device or follow
the [ARCore emulator guide](https://developers.google.com/ar/develop/java/emulator).
For Pixel 5 API 34, select VirtualScene as the back camera and install the
x86 emulator APK for Google Play Services for AR from
[Google's ARCore SDK releases](https://github.com/google-ar/arcore-android-sdk/releases).

Plane tracking and anchor placement on the emulator: **NOT RUN**. Availability
returned `ready` on the Pixel 5 API 34 Google APIs image. On both the Google
APIs image and a cold-booted Google Play image with VirtualScene and Google's
ARCore 1.57.0 emulator APK, ARCore could not start a camera session: it tried
camera ID `0`, but the emulator exposed the back camera as ID `10`. Use an
ARCore-compatible physical Android device to verify plane tracking and placement.

## Use it from Go

Check the negotiated renderer contract for `nativeui.CapabilityAR`. It says
the app has the service adapter, not that the device can run a session.
Expose a fallback or a screen without an AR requirement if the capability
is absent or unavailable. Return a request from an explicit action:

```go
return nativeui.Result{Service: &nativeui.ServiceRequest{
    ID: "ar-check",
    AR: &ar.Request{Operation: ar.Check},
}}, nil
```

Use `github.com/DonaldMurillo/gofastr-native/device/ar` and register
`ar-check` in the route's `Services` map. A completed check returns
`result.AR.Availability`: ready, install-needed, update-needed, unsupported,
or unknown. Checking does not request camera permission or install anything.

A separate explicit `ar.Place` request opens a native camera screen. Move
the camera until a surface is detected, tap a point on it (or use **Place
at center**), then choose **Done**. A completed result returns the anchor's
`result.AR.Position` as x, y, z in meters relative to that session only.
These coordinates are not durable or shareable. Cancel and Back return
no position; denial, unsupported hardware and session errors are also
reported without a position. The session releases the camera when paused
and closes when the screen ends. No camera frames are sent to Go or saved
by the app. Cloud anchors, geospatial anchors and recording are not enabled.

The example opens directly to the availability and placement actions.
To compile the adapter against the installed Android SDK and generated Go
bindings:

```sh
GOWORK=off GOFASTR_ARCORE_AAR=/path/to/core-1.57.0.aar \
  go test ./cmd/gofastr-native -run '^TestARCore'
```

Before distribution, the app publisher must approve the SDK and Google Play
Services dependency, include the required end-user disclosures in the app's
terms of service (including links to the current Google Terms of Service and
Privacy Policy), and review camera/ARCore data disclosures and store listings.
