# Changelog

This repository holds two Go modules, tagged separately:

- `github.com/DonaldMurillo/gofastr-native`, tagged `vX.Y.Z`
- `github.com/DonaldMurillo/gofastr-native/tui`, tagged `tui/vX.Y.Z`

Both are experimental. Until 1.0, any minor or patch release can change the API.

## v0.0.1 (2026-11-08)

The first tagged release of both modules.

### Phone apps (`gofastr-native`)

- `nativeui`: screens, routes and actions defined in Go, with a runtime that
  owns drafts, saves, presentations and paging. Apps write no Swift, Java,
  HTML or CSS.
- Renderers for UIKit (`render/ios`), Android views (`render/android`) and a
  WebView (`render/webview`), all drawing the same definitions.
- Typed device services: audio, files, notifications, photos, secure storage,
  incoming shares, sharing and widgets.
- `extension`: an API for libraries that add a screen component or device
  adapter, with the built-in `media` and `photos` extensions.
- `mobilehost`: runs an existing GoFastr web app inside the phone app over an
  in-memory listener, or as a service-only host with no transport.
- The `gofastr-native` CLI: `doctor`, `init`, `dev`, `build`, `run`,
  `inspect` and `extension`. It builds with the platforms' command-line tools
  (gomobile, aapt2, apksigner, swiftc, codesign), without an Xcode project or
  Gradle. Release builds sign APKs, AABs and IPAs with keys you supply and
  write `release-report.json`.
- Examples: `starter`, `notes`, `gallery`, `devicefeatures` and `chordharp`.
- Requires gofastr v0.96.0.

Tested on the iOS 26.5 simulator, Android emulators and the Chromium phone
simulator. Not yet run on a physical iPhone or Android phone. See
[docs/design/status.md](docs/design/status.md) for what is and is not
verified.

### Terminal apps (`tui`)

- Raw mode and input decoding for Unix and Windows terminals, including
  bracketed paste, mouse, key repeat and a startup capability probe.
- A diffing cell renderer with an image layer (kitty, iTerm2 and Sixel).
- Unicode 17.0.0 grapheme and width tables (`tui/uni`).
- Layout, a retained view tree, focus handling and an app event loop.
- Widgets: text, inputs and text areas, buttons, lists, tables, tabs, scroll
  and split views, dialogs, notifications and images.
- An in-memory VT emulator for tests (`tui/vt`).
- Demos: `tui-demo` and `netmon`.

Tested against the VT emulator and real ptys on macOS. On Windows it builds
and passes vet but has not been run.

### Repository

- `internal/repocheck` runs with `go test ./...` and fails on local paths,
  tool names, credentials, stale release claims, uncredited third-party
  notices and module layout mistakes.
- `go run ./internal/releasecheck vX.Y.Z` checks a release from a fresh clone,
  including dependency licenses. See [RELEASING.md](RELEASING.md).
- `scripts/hooks/pre-commit` runs the repocheck test before each commit.
