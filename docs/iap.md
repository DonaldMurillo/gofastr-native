# iOS in-app purchases and subscriptions

`device/iap` provides typed StoreKit 2 services for the native UIKit renderer.
Check the `iap/v1` capability (`nativeui.CapabilityIAP`) before showing purchase
controls. The adapter requires iOS 15 or later; builds from the CLI target iOS 16
or later. Android and WebView report `unsupported` and never start a purchase.

Return a `nativeui.Result{Service: &nativeui.ServiceRequest{ID: "store", IAP: &operation}}`
from an explicit action and register `"store"` in the route's `Services` map.
The continuation receives a `nativeui.ServiceResult`; on `EffectCompleted`,
read `result.IAP`. No IAP payload is returned on cancellation, denial, failure,
or unsupported platforms.

| Operation | Arguments | Completed payload |
| --- | --- | --- |
| `iap.Products` | `ProductIDs` (1–100 unique App Store IDs) | `Products` with localized names, descriptions and display prices; unknown product IDs can be absent |
| `iap.Purchase` | `ProductID` | `PurchaseState` is `iap.Purchased` with a verified `Transaction`, or `iap.Pending` without one (for example, Ask to Buy) |
| `iap.Entitlements` | None | `Transactions` from StoreKit current entitlements; excludes revoked, upgraded and consumable transactions and requires a matching verified subscribed or grace-period status for auto-renewable subscriptions |
| `iap.Updates` | None | `Transactions` combining verified unfinished transactions and up to 100 recent verified StoreKit updates observed in this process; excludes matching subscription transactions with unknown renewal state |
| `iap.Finish` | `TransactionID` (decimal string) | Empty result; finishes a matching verified unfinished transaction, or does nothing if none remains. Fails if StoreKit still lists the transaction as unfinished after finishing it |
| `iap.SubscriptionStatus` | `ProductID` of an auto-renewable subscription | `Subscriptions` for the product's group, including renewal state, auto-renewal flag and verified transaction; the transaction may identify another tier |
| `iap.Sync` | None | Empty result; asks the App Store to sync purchases, after which the app should re-read entitlements |

Show StoreKit's `DisplayPrice` rather than formatting or hardcoding prices.
Initiate purchases and restore only from user actions; restore can prompt for
App Store authentication. Never grant access for a pending purchase. Refresh
entitlements at launch, on return to the app and after purchase or restore;
consult expiration and renewal state as appropriate for the product.

The native host listens to `Transaction.updates` at startup. `Updates` is a
non-destructive snapshot, not a stream to Go. Poll when you need to reconcile
access or delivery; observations can repeat. The recent cache is not persistent
or a record of every App Store event. Unfinished transactions survive relaunch.
Treat updates as reconciliation signals, not proof of access. An unknown
subscription status is not an entitlement, including while Ask to Buy awaits
approval in StoreKit testing.
Deduplicate fulfillment by transaction ID, account for revocations/upgrades,
and call `Finish` only **after** durably delivering the purchase. StoreKit
verifies results on device; the API does not send signed receipts or replace
app-specific server-side validation and App Store Server Notifications.

## Demo and simulator acceptance

The Go demo exercises the same service path as a UIKit app. Build it and run
the UI suite on a dedicated booted simulator. The test runner installs the
app and loads the local StoreKit configuration through `SKTestSession`. The
tests reset local transactions and must not run in parallel.

```sh
export GOWORK=off
out="$PWD/dist/storefront"
device="<booted simulator UDID>"
go run ./cmd/gofastr-native build --platform ios-sim --renderer native \
  --pkg ./examples/storefront/storefrontnative --id dev.gofastr.storefront \
  --name "StoreKit Demo" --out "$out"
bash examples/storefront/uitests/run.sh "$device" "$out/Storefrontnative.app" "$out/ui-tests"
```

A simulator `.app` zip can be extracted and installed with `simctl install`
instead of rebuilding. A standalone launch does not activate the local store:
run the suite above to exercise local transactions without App Store Connect.

With Xcode 26.6 on an iOS 18.5 simulator, the three UI tests passed:
purchase, finish and restore; finishing two purchases one at a time, with
Updates empty afterward and again after a relaunch; and an approved Ask to Buy
subscription. On an iOS 26.5 simulator with the same Xcode, catalog lookup
returned no products. An earlier run on iOS 27 that mixed an Xcode 26.6 app
build with the Xcode 27 StoreKit runner reported Finish as done while both
transactions stayed unfinished after relaunch; Finish now fails in that case.
Physical devices, Sandbox and real billing: **NOT RUN**.

Some iOS simulator runtimes reject StoreKit test configuration from the
command line (`SKInternalErrorDomain Code=3`, Apple Feedback FB22237318).
If catalog lookup returns no products and the test log shows this setup error,
use a runtime that supports CLI StoreKit testing rather than signing in to
the App Store.

Before distributing to devices, the app owner must enable In-App Purchase for
the App ID, configure matching products and subscription groups in App Store
Connect, complete the applicable agreements, tax and banking setup, and approve
signing, provisioning and TestFlight/App Store submission. Local simulator
tests do not verify Sandbox, TestFlight, real billing or hardware behavior.
