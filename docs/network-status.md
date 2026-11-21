# Network status

The Android native renderer supports one-shot observations of the active network.
Enable the service with `--network-status --renderer native`. This opt-in adds
`android.permission.ACCESS_NETWORK_STATE` to the generated manifest; it does not
request a runtime permission. Without the flag the capability is not advertised.
iOS and WebView do not implement or advertise `network/v1`.

Return a `nativeui.ServiceRequest` with a `Network` request from an admitted
action and register a `nativeui.ServiceRespond` under the same service ID:

```go
nativeui.ServiceRequest{ID: "device", Network: &network.Request{Operation: network.Status}}
```

The completed result's `Network` contains `Connected`, `Transport`, `Metered`,
and `Validated`. Transport is `wifi`, `cellular`, `ethernet`, `vpn`, `other`, or
`none`; VPN takes precedence over its underlying transport. Disconnected results
use `none` and false flags. Metered reflects the OS network capability, not a
data-usage measurement. Validated means Android validated internet reachability;
connected alone does not promise internet access or a successful app request.

There is no subscription or automatic resume query. Apps must explicitly query
again when needed, including after resuming or changing connectivity. Results
are snapshots and can become stale immediately.

The device-features demo includes a Network status screen:

```sh
GOWORK=off go run ./cmd/gofastr-native run --platform android-emu --renderer native --pkg ./examples/devicefeatures/featuresnative --network-status --device Pixel_5_API_34 --out dist/mobile/device-features
```
