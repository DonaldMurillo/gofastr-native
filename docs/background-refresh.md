# Background refresh

`ServiceRequest.Refresh` lets Go code schedule periodic work that iOS runs while
the app is in the background or not running. The OS decides when, and whether,
each run happens. The interval is the earliest time before the next run, and is
not a timer. The iOS adapter uses `BGTaskScheduler` with one
`BGAppRefreshTask`. There is no Android adapter yet, so the CLI rejects
`--background-refresh` for Android builds.

## Go contract

| Operation | Request | Completed result |
| --- | --- | --- |
| `refresh.Schedule` | `IntervalMinutes` from 15 to 10080 (one week) | `Refresh.Scheduled == true` |
| `refresh.Cancel` | no interval | `Refresh.Scheduled == false` |
| `refresh.Status` | no interval | whether iOS holds a pending request |

```go
"schedule": func(context.Context, nativeui.Request) (nativeui.Result, error) {
	return nativeui.Result{Service: &nativeui.ServiceRequest{ID: "device",
		Refresh: &refresh.Request{Operation: refresh.Schedule, IntervalMinutes: 60}}}, nil
},
```

Other outcomes:

- `denied` with `Permission` `denied` or `restricted`: Background App Refresh is
  off for the app in Settings, or restricted on the device. Low Power Mode also
  pauses refresh but does not appear here.
- `unsupported`: the app was built without `--background-refresh`, or
  `BGTaskScheduler` is unavailable. The simulator never runs app refresh tasks.

The work itself runs through two functions the bound `--pkg` package exports.
`refresh.Runner` implements them:

```go
var background = refresh.NewRunner(func(ctx context.Context) error {
	return syncChanges(ctx) // stop when ctx ends
})

func BackgroundRefresh(timeoutMillis int64) error { return background.Run(timeoutMillis) }
func CancelBackgroundRefresh()                    { background.Cancel() }
```

A run gets at most 25 seconds. Only one run happens at a time. `Cancel` ends the
context when iOS expires the task, and the task then reports failure. The run is
not a screen: it cannot show UI or return a service result. Persist what it
changes, and let the next rendered screen read it.

## Build

```sh
go run ./cmd/gofastr-native build --platform ios-sim --renderer native --pkg ./examples/devicefeatures/featuresnative --background-refresh --out dist/mobile/device-features
```

The flag adds three Info.plist entries: `UIBackgroundModes` with `fetch`,
`BGTaskSchedulerPermittedIdentifiers` with `<bundle id>.refresh`, and
`GofastrBackgroundRefresh` naming that identifier. The generated app delegate
registers the task before launch finishes. A run waits for the Go runtime's
`Start` to return, then calls `BackgroundRefresh` off the main thread. Every run
submits the next request first, using the last scheduled interval, so a
schedule lasts until `Cancel`. The Background Modes capability needs no extra
entitlement or App ID setting.

## Acceptance limits

- The simulator accepts the configuration but does not run app refresh tasks;
  scheduling there reports `unsupported`.
- On a device, iOS learns from usage and may wait hours or skip runs. To force a
  run, attach the debugger to the device app, pause it, and run
  `e -l objc -- (void)[[BGTaskScheduler sharedScheduler] _simulateLaunchForTaskWithIdentifier:@"<bundle id>.refresh"]`,
  then resume it.
- A force-quit app does not get refresh runs until it is opened again.
- The demo's `Background refresh` screen shows when the last run happened.
