# iOS maps

`extension/maps` is an iOS-only compiled MapKit component, capability
`gofastr.maps/v1`. Register `maps.Definition()` with `nativeui.NewWithExtensions`.
Other renderers do not advertise it and refuse map screens with an unsupported
capability error. No WebView or Android adapter is supplied.

```go
block, err := maps.Block("places", maps.Content{
    Region: &maps.Region{Latitude: 37.78, Longitude: -122.42,
        LatitudeDelta: 0.08, LongitudeDelta: 0.08},
    Pins: []maps.Pin{{ID: "hall", Title: "City Hall", Subtitle: "San Francisco",
        Latitude: 37.7793, Longitude: -122.4193, Destination: "/places/hall"}},
    ShowsUserLocation: true,
})
```

Put the block in `Screen.Components` and handle `err`. A region or at least one
pin is required. Coordinates must be finite, latitude within [-90,90], longitude
within [-180,180]. Region deltas are (0,180] and (0,360]. There may be at most
200 pins, each with a unique `behavior.SafeToken` ID of at most 64 bytes, so the
prefixed event key fits the generic destination-key limit. Titles and subtitles are
plain UTF-8 without controls, at most 256 and 1024 bytes respectively. Destinations
require a nonempty title so MapKit can show their callout, and are ordinary
registered local routes, validated by the screen runtime.

## Wire and lifecycle

The component payload is a JSON string containing `region` (optional), `pins`,
and `showsUserLocation`. Region uses `latitude`, `longitude`, `latitudeDelta`,
`longitudeDelta`; pins use `id`, `title`, `subtitle`, `latitude`, `longitude`,
and optional `destination`. Only the component's exact `pin.<id>` destination
allowlist authorizes navigation; Swift never routes using payload destinations.
Select a pin and activate its detail-disclosure callout to navigate.

The 360-point map is retained on updates. Annotations reconcile by ID; callbacks
rebind to the fresh emitter and revoke the old generation. Disposal removes
annotations and view delegates and disables user location. A process-lifetime
authorization owner retains the permission request across view disposal when
the system alert suspends the renderer. Re-created maps read the current grant
and resume showing user location. Accessibility identifiers
are `native.map` and `native.map.pin.<id>`; pin labels use their titles.

## Location permission

Build with `--location --location-usage "Show your position on the map."` to add
`NSLocationWhenInUseUsageDescription`. Location is opt-in; both flags are required
and supported only with the native iOS renderer. Omitting both disables this
permission. The map requests when-in-use authorization on main only while
attached to a window, only if `ShowsUserLocation` is true and nonempty purpose
text exists. Without permission or purpose text, pins and the map still render.
No background location is requested; location is not sent to Go.

## Verification

Go tests cover bounds, empty and malformed data, event allowlists, 200 pins,
iOS-only manifests and unsupported renderer negotiation. Renderer tests execute
Foundation payload decoding and compile/link the maps-enabled optimized UIKit
source set for the iOS simulator. See `examples/gallery/gallerynative` for a
Go-only map and detail routes.

Simulator XCUITest acceptance passed on iPhone 17 with iOS 27.0: four tests,
zero failures, covering all three labelled pins, City Hall callout navigation,
and the system user-location annotation after a simulated location grant.
The fourth test resets location authorization, accepts the system when-in-use
alert and verifies that the re-created map shows the user-location annotation.
The captured map showed three pins and the simulated user-position indicator.
This does not establish physical-device location or device permission behavior.

NOT RUN: physical-device location and permission UX, VoiceOver, and
hardware performance. Simulator compilation or launch is not evidence of these.

### Simulator UI acceptance

The UI-test-only project in `extension/maps/uitest` uses XCUITest against the
separately installed CLI-built gallery, not an application target or signing
team. Boot an iPhone simulator and run from the repository root:

```sh
GOFASTR_IOS_UITEST=1 \
GOFASTR_IOS_DEVICE="<booted-simulator-udid>" \
GOFASTR_IOS_UITEST_OUT="$HOME/maps-simulator/acceptance" \
GOWORK=off go test ./extension/maps -run TestGalleryMapUI -v -count=1 -timeout=20m
```

The output directory must be outside the repository and must not already contain
`Maps.xcresult`. The opt-in wrapper builds the gallery with `--location --location-usage`,
installs it, grants location with `simctl privacy`, and sets a simulated position
before launching. It runs `xcodebuild test` with the shared `MapsUITests` scheme,
serially on the specified simulator. Normal Go test runs skip UI acceptance.
The four tests check pin identifiers and labels, callout navigation to a detail
screen, a visible user-location annotation, and first-time authorization across
renderer suspension. The result bundle retains a map
screenshot attachment; the wrapper also writes `map-screen.png` on success.

To rerun only XCUITest against an already built and installed gallery:

```sh
xcodebuild test -project extension/maps/uitest/MapsUITests.xcodeproj \
  -scheme MapsUITests -destination "platform=iOS Simulator,id=<booted-simulator-udid>" \
  -parallel-testing-enabled NO -derivedDataPath "$HOME/maps-simulator/DerivedData" \
  -resultBundlePath "$HOME/maps-simulator/Maps.xcresult" CODE_SIGNING_ALLOWED=NO
```
