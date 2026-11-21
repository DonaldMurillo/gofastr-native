# Device and battery snapshots

`device/deviceinfo` defines bounded, snapshot-only reads. Native Android API 24+
implements `deviceinfo/v1` without permissions or build flags. iOS and WebView
validate requests and return `unsupported` if dispatched; neither advertises
the capability. There are no subscriptions or background monitors.

Return a service from an admitted Go action and register its continuation:

```go
return nativeui.Result{Service: &nativeui.ServiceRequest{
    ID: "device",
    DeviceInfo: &deviceinfo.Request{Operation: deviceinfo.ReadInfo},
}}, nil
```

`Route.Services["device"]` receives a `nativeui.ServiceResult`. Only completed
outcomes carry `DeviceInfo`. Check the status before reading the snapshot.
Use `deviceinfo.ReadBattery` for the battery operation. Both requests contain
only an operation; they never trigger a prompt.

## Results

- `Result.Info`: platform (`android` or `ios`), manufacturer, model, OS version,
  API level (zero where not applicable), BCP-47 locale, and app version.
  Android reads Build metadata, the default locale and PackageManager's
  versionName; app version can be empty. Text is plain and at most 128 bytes.
- `Result.Battery`: `levelKnown`, integer `level` from 0 to 100, `state`,
  `powerSource`, and `powerSave`. An unknown level is zero with
  `levelKnown=false`; zero and false are always present in JSON.
- States: `charging`, `discharging`, `full`, `not_charging`, `unknown`.
  Sources: `ac`, `usb`, `wireless`, `none`, `unknown`. An absent sticky
  battery intent produces unknown level/state/source. Power-save mode comes
  from PowerManager independently of the battery intent.

`Request.Accept` rejects missing, mixed or mismatched snapshots, invalid text,
platforms, locale shape, API levels, enum values and battery levels. Battery
state and power-save mode are independent observations, not inferred from the
percentage. A snapshot may become stale immediately.

The service exposes no serial number, installation ID, advertising ID, screen
metrics or permission state. Applications decide whether to display or retain
these observations; no telemetry is sent by the service.

## Demo and checks

`examples/devicefeatures/featuresnative` has a Device and battery screen with
Device info and Battery actions. The latter displays, for example,
`Battery: 42% charging (ac), power save off.`

```sh
GOWORK=off go test ./device/deviceinfo ./nativeui ./examples/devicefeatures/featuresnative
GOWORK=off go test ./cmd/gofastr-native -run TestNativeAndroidCompilesAgainstSDKAndGeneratedBindings
python scripts/android_smoke.py --serial emulator-5580 --package dev.gofastr.devicefeatures --deviceinfo
```

Run the last command against an installed devicefeatures APK on an isolated
emulator. It compares model/API with getprop, injects a 42% AC-charging battery
state, checks the rendered result, and resets battery simulation in a finally
block. Screenshots, XML and results go to the selected `--out` directory.
Android 14 x86-64 emulator acceptance matched model/API to system properties and
displayed the injected 42% AC-charging result. Physical-device battery behavior
and iOS runtime acceptance are NOT RUN.
