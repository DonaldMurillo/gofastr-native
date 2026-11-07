# Mobile application lifecycle: M3

## Gate A: UI-independent startup

Native Notes composes the same entity, owner-scoped repository, service and
installation data as both web presentations, without mounting a `UIHost` or
importing `notesweb`, `mobileui`, or `render/webview` into the native binding.
Gate A covers composition without a web presentation. Gate B below covers the
listener-free service lifecycle provided by gofastr v0.96.0.

- `examples/notes/notesapp/application.Open(dataDir)` constructs the shared
  `Application`: `App`, `Data`, and `Service`. It does not choose a presentation,
  install identity middleware, initialize plugins, migrate, or start the app.
  The absolute directory is opened once through `localapp.Open`, which creates
  private first-launch files and reuses the persisted identity and session
  secret. `Data.User` is the identity for authorized native service contexts.
- The application registers `Data.Close` as its first stop hook. Later hooks
  and battery cleanup run while the database is still open. A caller abandoning
  construction calls `App.Shutdown`; after host startup, use `Host.Stop`.
  `WithDB` alone does not transfer database ownership to GoFastr.
- `mobilehost.New(app, mobilehost.Config{Data: shared.Data, ServiceOnly: true})`
  explicitly omits the WebView script route, script injection, and `UIHost`
  requirement. It still installs the local identity middleware, respecting a
  foreign authenticator, and the owner extractor used by direct service calls.
  It runs `App.Run(context.Background(), framework.ServicesHost())` without
  constructing a listener, HTTP server, HTTP client or cookie jar.
- `mobilehost.New` defaults are unchanged: without `ServiceOnly`, a mounted
  `uihost.UIHost` is required at startup and the bridge is installed normally.
  `notesapp.Build`, `BuildWithPlatform`, `BuildScreensWithPlatform`, and
  `notesapp.Service` retain their existing signatures and presentations.
- Native `Start` rejects a foreign owner extractor before opening installation
  data. It publishes command state only after startup readiness. Native
  `Render`, `Dispatch`, `List`, `Get`, and `Save` call Go screens or the shared
  service directly, never HTTP endpoints. The service adapter retains the same
  owner scopes, validation, hooks, not-found behavior and persisted data as HTTP.

### One lifecycle owner

`Host.Start` delegates to public `App.Run` with `ServicesHost` for service-only
apps and `App.Serve` on a memory listener for WebView apps. The v0.96.0 pin
performs migration/entity seeding, app seed hooks,
plugin/battery initialization, a second migration pass for schema registered
during initialization, battery/application start hooks, readiness and handler
composition. `App.Shutdown` owns cancellation, request/stream drain, reverse
battery stop and stop hooks. Mobile code does not reproduce those phases.

`InitPlugins` alone is not a replacement: it initializes registrations but does
not provide migration/seeding, start hooks, readiness, or complete
startup-failure handling. Nor does `RunWithSignals` initialize an application;
it waits for signals and invokes shutdown. Mobile disables process signal
handling in WebView mode because the OS host owns its lifecycle. `App.Run`
installs no signal handler, so service-only mode does not change that setting.

Service-only Start waits on `App.ServicesReady()`, not `OnReady`. `App.Run`
refuses `OnReady` hooks because they need an HTTP listener. It also refuses
interactive HTTP setup; headless setup remains available. Framework HTTP routes,
`WrapServer`, TLS and server timeouts stay configured but unused.

Run receives `context.Background()`, not the bridge context or the startup
timer's context. Stop calls `App.Shutdown` with the caller's context; Shutdown
cancels the framework lifecycle context passed to boot hooks and workers. This
avoids a competing Run-context shutdown with a different deadline.

Host completion is a closed, repeatably observable channel, not a consumed
error receive. Run rolls back admitted startup failures itself. When Run or
Serve returns on its own, the completion path claims shutdown only if Stop has
not claimed it. Its idempotent `App.Shutdown` also closes a composition refused
before framework boot begins. Startup timeout requests Stop with a five-second
context. `Stop` closes WebView transport when present, joins Run or Serve within
the supplied context and reports deadline errors. Repeat/concurrent stops share
their result. A Host/App is one-shot; restart constructs a fresh pair over the
same data directory.

Host shutdown has one owner: either the first `Stop`, or the completion path
if the run ends on its own. Run may return nil while an existing Shutdown is
still tearing down. Stop waits for its own Shutdown call, not just the run's
completion channel, so stop-hook failures stay with that owner. A stop during
boot cancels the current phase and waits for teardown without publishing
readiness. Listener closure during a normal WebView Stop does
not start a second, shorter shutdown budget. Long requests such as live watches
drain against the Stop caller's context rather than producing an unrelated
five-second follower timeout. The HTTP-boundary regression is
`TestStopDrainsLongRequestWithOneShutdownOwner` in `mobilehost/lifecycle_test.go`.

The WebView Host also owns the bridge-request context. Stop cancels outstanding
native-channel requests before HTTP drain, so abandoned live polls cannot
consume the entire shutdown deadline. Cancellation does not imply that an
already-started write failed; no action is replayed. Direct `Do`/`Open` callers
keep their own contexts and graceful-drain behavior. Storage closes only after
request handlers drain; `TestStopCancelsOutstandingBridgeRequestsBeforeClosingStorage`
exercises that boundary.

### Native command admission and drain

Native entry points acquire a service/screen/identity snapshot and increment
an active-operation counter under the lifecycle mutex. They release that mutex
before running Go application code or waiting for a capability callback, and
decrement the counter on every return. Each operation has a 30-second context.

`Stop` closes admission first, rejects new commands and overlapping Start, and
waits for all admitted operations without holding the business/lifecycle lock.
Only then does it invoke Host.Stop with a 10-second shutdown budget and discard
state. Concurrent Stop calls join the same result. An already-admitted callback
can finish; a callback attempting a new command after stop admission closes
receives an error instead of deadlocking behind a held lifecycle lock.

This is not an unconditional hard wall-clock bound: Go commands and platform
capabilities must honor their contexts and return. A command ignoring its
context can delay the admission drain; the 10-second host shutdown budget starts
after that drain. Code running inside an admitted operation must not synchronously
call `Stop`, because Stop necessarily waits for that operation itself.

## Gate B: listener-free startup

Gate B is complete for `Config.ServiceOnly` using the public v0.96.0 lifecycle.
No GoFastr core change or manual boot pipeline is needed. This does not remove
HTTP dependencies or route registration from the application. It leaves those
routes unused and rejects `Host.Do` and `Host.Open` with the exported
`mobilehost.ErrServiceOnly`, including before startup and after shutdown.

`mobilehost/lifecycle_test.go` exercises these boundaries:

- `TestServiceOnlyHasNoTransport` checks construction and startup without a
  listener, client or cookie jar, unused `WrapServer`, readiness, and transport
  errors before Start, while running and after Stop.
- `TestServiceOnlyStopDuringBoot` cancels seed and start hooks, joins the Start
  caller and run goroutine, closes storage once, and never publishes readiness.
- `TestServiceOnlyConcurrentStartStop` rejects a second Start while booting and
  joins concurrent Stops with the same teardown error and one stop-hook call.
- `TestServiceOnlyRejectsReadyHooksDuringBoot` exercises startup refusal and one
  rollback when a start hook registers an HTTP-dependent `OnReady` hook.
- `TestStartupFailureDrainsAndStopIsRepeatable` covers pre-boot refusal,
  migration, seed, init and start failures, plus WebView HTTP preflight failure.
- `TestServiceOnlyStopWaitsForTeardown` proves that Run completion does not
  replace waiting for Shutdown or consume its teardown error.
- `TestStartupTimeoutCancelsAndJoinsWorkers` checks timeout cancellation,
  run completion and database cleanup.

WebView request drain and bridge cancellation tests still run on a mounted
`uihost.UIHost` with the in-memory transport. Notes service/HTTP interoperability
and forged-owner assertions use that WebView mode rather than a service-only
host. Service-only identity and unused-route checks call the configured handler
directly without constructing a host transport. Notes keeps its missing-route
assertions in `TestServiceOnlyLeavesWebRoutesUnused`; its native bootstrap test
checks that HTTP access is unavailable.

## Regression checks and implementation notes

The acceptance boundaries are real SQLite installation files, native exported
commands/screens, the service's scoped CRUD adapter and requests through the
in-memory HTTP server. Coverage includes fresh nested directories and file
modes, migration of an existing Notes table, identity/secret persistence,
schema declared during battery initialization, seed/start order, migration,
seed, initialization and start failures, timeout cancellation, database cleanup,
repeat shutdown/restart, foreign/anonymous authorization, forged HTTP ownership,
service/HTTP interoperability, legacy web builders and native operation drain
with a reentrant callback. Dependency checks prevent web adapters returning to
the native bootstrap.

Next time: inspect the pinned module's `framework/host.go` with `GOWORK=off`;
the sibling workspace checkout may expose a different contract. Keep application
construction separate from lifecycle execution.
Use the admission helper for new native commands so asynchronous waits cannot
reintroduce a lifecycle lock held across business code.

```sh
gofmt -l $(git ls-files '*.go' ':!:tui/**')
GOWORK=off go vet ./...
GOWORK=off go test -race ./mobilehost/... ./examples/notes/...
GOWORK=off go test ./...
```

These checks do not establish renderer pixel quality, capability negotiation,
Android native support. Those have separate gates.
