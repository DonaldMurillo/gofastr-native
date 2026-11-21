# Background refresh

`ServiceRequest.Refresh` lets Go code schedule periodic work that the OS runs
while the app is in the background or not running. The OS decides when, and
whether, each run happens. The interval is the earliest time before the next
run, and is not a timer. One Go API covers both platforms:

- iOS uses `BGTaskScheduler` with one `BGAppRefreshTask`.
- Android uses `JobScheduler` with one persisted periodic job. No WorkManager,
  AndroidX or Gradle dependency is added.

## Go contract

| Operation | Request | Completed result |
| --- | --- | --- |
| `refresh.Schedule` | `IntervalMinutes` from 15 to 10080 (one week) | `Refresh.Scheduled == true` |
| `refresh.Cancel` | no interval | `Refresh.Scheduled == false` |
| `refresh.Status` | no interval | whether the OS holds a pending request |

```go
"schedule": func(context.Context, nativeui.Request) (nativeui.Result, error) {
	return nativeui.Result{Service: &nativeui.ServiceRequest{ID: "device",
		Refresh: &refresh.Request{Operation: refresh.Schedule, IntervalMinutes: 60}}}, nil
},
```

Other outcomes:

- `denied` with `Permission` `denied` or `restricted`. On iOS, Background App
  Refresh is off for the app in Settings, or restricted on the device; this
  applies to every operation. On Android 9 and later, a schedule reports
  `restricted` when the user set the app's battery usage to Restricted. Cancel
  and status still work there. Low Power Mode, Doze and battery saver also delay
  runs but do not appear here.
- `unsupported`: the app was built without `--background-refresh`, or
  `BGTaskScheduler` is unavailable. The iOS simulator never runs app refresh
  tasks.
- `failed` on Android when JobScheduler rejects the job, or when another
  component already holds the job ID the adapter uses.

The work itself runs through two functions the bound `--pkg` package exports.
`refresh.Runner` implements them:

```go
var background = refresh.NewRunner(func(ctx context.Context) error {
	return syncChanges(ctx) // stop when ctx ends
})

func BackgroundRefresh(timeoutMillis int64) error { return background.Run(timeoutMillis) }
func CancelBackgroundRefresh()                    { background.Cancel() }
```

A run gets at most 25 seconds on both platforms. Only one run happens at a time.
`Cancel` ends the context when the OS ends the task early, and the task then
reports failure. The run is not a screen: it cannot show UI or return a service
result. Persist what it changes, and let the next rendered screen read it.
Write the work so a repeated run is harmless. Go cannot stop a goroutine that
ignores its context, so work that does keeps running until the OS kills the
process.

## Build

```sh
go run ./cmd/gofastr-native build --platform ios-sim --renderer native --pkg ./examples/devicefeatures/featuresnative --background-refresh --out dist/mobile/device-features
go run ./cmd/gofastr-native build --platform android-emu --renderer native --pkg ./examples/devicefeatures/featuresnative --background-refresh --out dist/mobile/device-features-android
```

The flag requires the native renderer. A bound package that lacks either
function fails the Swift or Java compile.

On iOS the flag adds three Info.plist entries: `UIBackgroundModes` with `fetch`,
`BGTaskSchedulerPermittedIdentifiers` with `<bundle id>.refresh`, and
`GofastrBackgroundRefresh` naming that identifier. The generated app delegate
registers the task before launch finishes. A run waits for the Go runtime's
`Start` to return, then calls `BackgroundRefresh` off the main thread. Every run
submits the next request first, using the last scheduled interval, so a
schedule lasts until `Cancel`. The Background Modes capability needs no extra
entitlement or App ID setting.

On Android the flag declares an unexported `BackgroundService` guarded by
`android.permission.BIND_JOB_SERVICE`, and generates that class in the app
package. It extends the renderer's `BackgroundJobs` JobService and calls the
bound package's `BackgroundRefresh` and `CancelBackgroundRefresh`. The job is
persisted, so the native renderer's existing `RECEIVE_BOOT_COMPLETED`
permission keeps it across reboots. Android repeats a periodic job on its own,
so a schedule lasts until `Cancel`, and scheduling again replaces the interval.
The activity and the job share a process-wide lease on the Go runtime: the
first one calls `Start`, the last one calls `Stop`, so a job can run in a
process that has no activity. A failed or stopped run waits for the next
period; it is not retried early.

## Acceptance limits

- The iOS simulator accepts the configuration but does not run app refresh
  tasks; scheduling there reports `unsupported`.
- On an iOS device, iOS learns from usage and may wait hours or skip runs. To
  force a run, attach the debugger to the device app, pause it, and run
  `e -l objc -- (void)[[BGTaskScheduler sharedScheduler] _simulateLaunchForTaskWithIdentifier:@"<bundle id>.refresh"]`,
  then resume it.
- A force-quit iOS app does not get refresh runs until it is opened again.
  Force-stopping an Android app cancels its job; schedule it again after the
  next launch.
- On Android, force a run with `adb shell cmd jobscheduler run -f <application id> 1734767206`
  (the adapter's fixed job ID, `0x67667266`). `adb shell dumpsys jobscheduler`
  shows the pending job. A forced run skips the scheduler's own timing, so it
  says nothing about when Android would have run the job.
- The demo's `Background refresh` screen shows when the last run happened.
