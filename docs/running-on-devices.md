# Running on a simulator, emulator or device

`gofastr-native` does for phones what `gofastr desktop build` does for the
Mac. It generates a small native shell, compiles it with the platform's
command-line tools, assembles the `.app` or `.apk` itself, then installs and
launches it. Xcode and the Android SDK must be installed; neither is opened.

Check the toolchain first. It is read-only and prints the fix for anything
missing:

```bash
go run ./cmd/gofastr-native doctor
```

iOS simulator (a Mac with Xcode):

```bash
go run ./cmd/gofastr-native run --platform ios-sim --pkg ./examples/notes/notesmobile --screenshot dist/ios.png
```

Real UIKit Notes (separate app and data, no WebView screens):

```bash
go run ./cmd/gofastr-native run --platform ios-sim \
  --pkg ./examples/notes/notesnative \
  --renderer native \
  --id dev.gofastr.notesnative --name 'Native Notes' --out dist/mobile/ios-native
```

Android emulator (an Android SDK with build-tools, a platform, the NDK, the
emulator and one AVD, plus a JDK 17+):

Native Notes on Windows, from PowerShell:

```powershell
go run ./cmd/gofastr-native doctor --platform android-emu
go run ./cmd/gofastr-native run --platform android-emu --renderer native --pkg ./examples/notes/notesnative --device Pixel_5_API_34 --microphone-usage 'Record private voice notes.' --url-scheme notes-demo --out dist/mobile/android-native
```

Use an AVD name listed by `doctor`, or an attached device's adb serial. Camera
capture is enabled by default; recording requires the explicit microphone flag.
Build-only uses `build` in place of `run`. The APK contains ARM64 and x86-64
libraries and uses a debug signing key. See [Android implementation and tests](android.md).

The WebView host remains available:

```bash
go run ./cmd/gofastr-native run --platform android-emu --pkg ./examples/notes/notesmobile --screenshot dist/android.png
```

`build` instead of `run` stops after the `.app` or `.apk`. `--dry-run`
prints every step without running any. `--device` picks a simulator (name or
UDID), an adb serial or an AVD. `--id` and `--name` set the bundle id and the
display name. Output lands in `dist/mobile/<platform>/`.
`--renderer native` selects the bundled Go-definition-driven UIKit or Android
renderer. Without it the WebView hosts remain the default. `--host-template` remains an experimental custom-host escape hatch,
not the application screen-authoring model; it cannot be combined with native
renderer selection. See [the contract and limits](native-presentation.md).
`--microphone-usage 'Record a voice note to attach privately.'` explicitly adds
the iOS microphone purpose string; recording is unavailable when it is omitted
or empty. The default iOS WebView host also bundles the reusable native-service
adapter; custom host templates must opt into that library integration themselves.

| iOS simulator | Android emulator |
|---|---|
| build the pinned `gomobile` + `gobind` (go.mod `tool` directive) | same |
| `gomobile bind -target=iossimulator` -> `Notesmobile.xcframework` | `gomobile bind -target=android/arm64,android/amd64` -> `notesmobile.aar` |
| render `App.swift`, `Info.plist` | render `MainActivity.java`, `AndroidManifest.xml` |
| `xcrun swiftc` links the shell to the framework | `javac` -> `d8` -> `aapt2 link`, then dex + `libgojni.so` added in Go |
| `codesign -s -` (ad hoc; enough for the simulator) | `zipalign`, `apksigner` (debug key) |
| `simctl boot/install/launch`, `simctl io screenshot` | `emulator -avd`, `adb install/am start`, `screencap` |

Safari > Develop > Simulator attaches the Web Inspector to the iOS WebView;
`chrome://inspect` attaches DevTools to the Android one.

Signed release builds, Android App Bundles, and `--platform ios-device` are
described in [release builds](release.md). That page lists which release
gates still need hardware or a store account and have not been run.

Run the [component gallery](component-gallery.md) to explore supported
controls. Use [inspection and tracing](debugging.md) to diagnose screen
definitions and runtime commands without logging app content.

A simulator build runs one JavaScript check on request: the body of an async
function in `GOFASTR_MOBILE_EVAL` runs once in the first page, and its result
is logged. `simctl launch` passes it through as
`SIMCTL_CHILD_GOFASTR_MOBILE_EVAL`; read the log with `--console-pty`. Device
builds compile the hook out.

