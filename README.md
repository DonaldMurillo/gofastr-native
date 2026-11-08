# gofastr-native

Write an app in Go and run it natively: as a phone app on iOS and Android,
or as a terminal app on macOS, Linux and Windows. The project is
experimental and not ready for production use.

It has two parts, each its own Go module:

- **Phone apps** (`github.com/DonaldMurillo/gofastr-native`). Screens and
  actions are Go `nativeui` definitions. A bundled renderer draws them with
  UIKit, Android views or a WebView, so apps write no Swift, Java, HTML or
  CSS. Device services (files, photos, audio, notifications, secure storage,
  incoming shares, widgets) are typed Go requests. The `gofastr-native` CLI
  builds and runs the app with the platforms' command-line tools: no Xcode
  project, no Gradle. It can also run an existing GoFastr web app inside the
  phone app's process, served over an in-memory listener with no socket.
- **Terminal apps** (`github.com/DonaldMurillo/gofastr-native/tui`). A
  terminal UI library written from scratch on the standard library and
  `golang.org/x/sys`: raw mode and input decoding, a diffing cell renderer,
  Unicode widths, layout, a retained view tree, widgets and images. See
  [`tui/README.md`](tui/README.md).

The two parts don't share an API yet. Phone screens are `nativeui`
definitions, and terminal apps use `tui/view` and `tui/widget`. A `nativeui`
renderer built on `tui` would let one set of screens run in both places; it
has not been written.

## Start a phone app

```sh
go run ./cmd/gofastr-native doctor --platform android-emu
go run ./cmd/gofastr-native init --out ../hello
cd ../hello
go test ./...
go tool gofastr-native dev --platform android-emu --renderer native --pkg ./hello
```

`doctor` checks the toolchain without changing anything and prints the fix
for whatever is missing. `init` creates a module with Go screens, a private
item store, durable drafts, tests, and the CLI pinned as a `go tool`. It
never writes into an existing directory. Until gofastr-native publishes a
version, the new `go.mod` uses this checkout through a `replace`. `dev`
rebuilds, reinstalls and restarts the app after each change; it is not hot
reload. Use `--platform ios-sim` for the iOS simulator.

See [getting started](docs/getting-started.md) and
[running on a simulator, emulator or device](docs/running-on-devices.md).

## Try the terminal library

```sh
cd tui
go run ./cmd/tui-demo     # paint with the mouse; shows what the terminal supports
go run ./cmd/netmon       # the machine's network connections, refreshed live
```

## Examples

| Path | What it shows |
|---|---|
| [`examples/starter`](examples/starter) | What `init` generates, kept in the repository. |
| [`examples/notes`](examples/notes) | The main phone example: one Notes app with native and WebView renderers, files, photos, reminders, sharing, widgets, and terminal and web presentations of the same service. |
| [`examples/gallery`](examples/gallery) | Every screen contract `nativeui` supports, one at a time. |
| [`examples/devicefeatures`](examples/devicefeatures) | Incoming shares, secure storage and widgets through their opt-in APIs. |
| [`examples/chordharp`](examples/chordharp) | A chord harp with sound, as a terminal app and as an Android app that share one Go synthesizer. |

## Layout

| Path | What it is |
|---|---|
| `nativeui/` | Screens and actions defined in Go, routing, and the runtime that coordinates drafts and actions. |
| `behavior/` | Editor checkpoints and draft storage that do not depend on a renderer. |
| `device/` | Typed requests for device services: `audio`, `files`, `notifications`, `photos`, `securestore`, `sharein`, `sharing`, `widgets`. |
| `extension/` | The API for libraries that add a screen component or a device adapter, with the built-in `media` and `photos` extensions. |
| `render/ios/`, `render/android/` | The native renderers: UIKit (Swift) and Android (Java), compiled into the app without Xcode or Gradle. |
| `render/webview/` | The WebView renderer for the same definitions, with its component styles in `render/webview/mobileui`. |
| `mobilehost/` | Runs a GoFastr app inside the phone app's process, with the native bridge (`bridge.js`). |
| `cmd/gofastr-native/` | The CLI: `doctor`, `init`, `dev`, `build`, `run`, `inspect`, `extension`. |
| `tui/` | The terminal UI library, a separate module. |
| `internal/` | Private file helpers, the Chromium phone simulator used by tests (`browsersim`), and the integration tests that drive it (`simtest`). |
| `scripts/` | Android emulator smoke tests. |
| `docs/` | Guides, indexed in [`docs/README.md`](docs/README.md). Design notes and the project status are in [`docs/design/`](docs/design/). |

## The gofastr version

`go.mod` requires gofastr v0.96.0. The module uses gofastr's public API only:
Go's `internal/` rule goes by import path, so this repository sees exactly
what any other app sees. When it needs something the framework does not
expose, the fix goes into gofastr as a public seam, not a copy here.

| Seam | What it does here |
|---|---|
| `framework.App.Run(ctx, framework.ServicesHost())` | Runs the service-only lifecycle without a listener, HTTP server or HTTP client. |
| `framework.App.ServicesReady()` | Reports service readiness after all boot phases succeed. |
| `framework.App.Serve(net.Listener)` | Runs the WebView app's full lifecycle on the in-memory listener. |
| `window.__gofastr_transport` (core-ui runtime) | Every runtime request goes through it, so `bridge.js` can carry requests over the native message channel. |
| `framework/localapp` | Opens the data directory: SQLite DB, session secret, the installation's owner id. Shared with the desktop and service batteries. |
| `App.NoteAuthenticator` and the banner | The startup banner prints paths for a listener with no `host:port`, and the host quiets the "No auth is wired" warning. |

To work against a local gofastr checkout, copy `go.work.example` to `go.work`
and point it at the checkout. `go.work` is gitignored.

## Run the tests

Validation is local-only; this repository intentionally has no CI workflows.

```sh
go vet ./...
go test ./...
node --test render/webview/*.test.cjs
(cd tui && go vet ./... && go test ./...)
(cd examples/chordharp && go vet ./... && go test ./...)
```

`go test ./...` includes `internal/simtest`, which drives the WebView
renderer in headless Chromium as an iOS and an Android phone. Set
`SIM_SHOTS` to a directory to keep its screenshots:

```sh
SIM_SHOTS=/tmp/shots go test ./internal/simtest/ -run TestPhoneWebView -v
```

The generated Java is compiled against gobind's Java bindings and
signature-exact Android stubs when a working `javac` (JDK 17+) is on
`PATH`; the test skips otherwise.

## Status

The phone side has run on the iOS 26.5 simulator, on Android emulators and
in the Chromium phone simulator. It has not run on a physical iPhone or a
physical Android phone. The terminal library is tested against an
in-memory terminal emulator and real ptys on macOS; on Windows it builds
and passes vet but has not been run. What has been checked, what has not,
the known gaps and the next steps are in
[docs/design/status.md](docs/design/status.md).

The project was previously named `gofastr-mobile`; see
[the rename](docs/design/project-rename.md) for the old paths.
