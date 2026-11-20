# Android background tasks

Background tasks are deferrable work, including work while the activity is closed.
The native Android host uses the platform **JobScheduler**, not WorkManager.
WorkManager requires AndroidX, which the plain javac/d8/aapt2 build does not use.
The implementation is compatible with API 24 and needs no Gradle project.

Enable packaging with `--background-tasks --renderer native` on Android.
Disabled builds and WebView hosts do not advertise `background/v1`. iOS has no
adapter and reports unsupported through the normal service capability contract.
Passing the Android-only build flag for iOS is an error.

Register compiled handlers before creating the runtime:

```go
app, err = app.WithBackground(map[string]nativeui.BackgroundHandler{
    "refresh": func(ctx context.Context) error {
        return refreshCache(ctx)
    },
})
```

From an admitted action, return a typed service with a registered continuation:

```go
nativeui.ServiceRequest{
    ID: "schedule-refresh",
    Background: &background.Request{
        Operation: background.Schedule,
        Identifier: "cache-refresh",
        Task: "refresh",
        Network: "any",
        DelayMinutes: 1,
    },
}
```

`device/background.Request.Validate` checks shape only. Scheduling an
unregistered task is rejected by Go before a host operation is admitted.
Task names and app-scoped identifiers are 1–128 ASCII token characters
(`A-Z`, `a-z`, digits, `.`, `_`, `:`, `-`). Scheduling the same identifier replaces
its pending job. A stable positive integer hash selects the Android job ID;
a collision with another identifier or service fails rather than replacing it.
The scheduler persists the identifier and task in its job extras across reboot.
The manifest already includes `RECEIVE_BOOT_COMPLETED` for the native host.

One-off delay is 0–525,600 minutes. Periodic work sets `Periodic: true` and
`IntervalMinutes` to 15–525,600; it cannot also specify a delay. One-off work
cannot specify an interval. `Network` is empty, `any`, or `unmetered`;
`RequiresCharging` adds a charging constraint. These are earliest opportunities,
not exact times. Doze, quotas, battery restrictions, and unmet constraints can
delay execution. Force-stop prevents delivery until the app is opened again.

`background.Cancel` and `background.Status` accept only the identifier.
Completed results contain `Background: &background.State{Scheduled: ...}`.
Status reports whether JobScheduler currently has the job, not last-run success,
timing, or guaranteed future execution. An immediate one-off can finish before
a later status read. Schedule reports successful scheduler admission, and cancel
reports removal of the pending job. Cancel requests cancellation of running work too; it does
not roll back writes already performed.

## Execution and lifecycle

The generated app-package `BackgroundService` supplies the Go binding to the
ordinary Java `BackgroundJobs` JobService. Activity and job startup share
process-local reference-counted runtime leases: the first consumer starts Go,
and the last consumer stops it. Start/Stop are serialized even when a job starts
without an activity or an activity closes during a job. Applications must make
their own Start/Stop safe for concurrent Command calls, as the demo does.

The local `background/run` runtime command takes `backgroundTask` and a unique
`backgroundId`, without requiring a renderer connection. It returns a typed
`background.status`: `completed`, `retry`, or `failed`. Nil handler errors
complete; `nativeui.ErrBackgroundRetry` and cancellation request rescheduling;
other errors, panics, and unknown names fail without rescheduling the one-off.
A failed periodic invocation does not cancel the periodic schedule.

Handlers receive a nine-minute context deadline, below JobScheduler's usual
ten-minute execution ceiling. `onStopJob` sends `background/stop` to cancel that
context and suppresses late completion. Handlers **must honor context
cancellation**, including in network and database calls. Go cannot forcibly
terminate a goroutine; uncooperative work can remain until Android kills the
process. Persist useful progress promptly. Jobs may run more than once: handlers
must be idempotent, authorize their own data access, and never depend on a screen,
activity, an interactive authentication prompt, or in-memory UI state.

The device-features demo schedules one-off or periodic timestamp work, displays
the last timestamp from private storage, and offers status and cancellation.
Refresh the timestamp or reopen its screen after execution.

## Validation

Go tests cover request validation, service outcomes, registered handler dispatch,
deadlines, cancellation, panics, and unknown tasks. CLI tests cover opt-in flags,
manifest wiring, and SDK compilation of the generated JobService binding.
Emulator execution, closed-activity delivery, reboot persistence, constraints,
retry timing, and physical-device acceptance: **NOT RUN**.
