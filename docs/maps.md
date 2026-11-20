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
200 pins, each with a unique `behavior.SafeToken` ID. Titles and subtitles are
plain UTF-8 without controls, at most 256 and 1024 bytes respectively. Destinations
are ordinary registered local routes, validated by the screen runtime.

## Wire and lifecycle

The component payload is a JSON string containing `region` (optional), `pins`,
and `showsUserLocation`. Region uses `latitude`, `longitude`, `latitudeDelta`,
`longitudeDelta`; pins use `id`, `title`, `subtitle`, `latitude`, `longitude`,
and optional `destination`. Only the component's exact `pin.<id>` destination
allowlist authorizes navigation; Swift never routes using payload destinations.
Select a pin and activate its detail-disclosure callout to navigate.

The 360-point map is retained on updates. Annotations reconcile by ID; callbacks
rebind to the fresh emitter and revoke the old generation. Disposal removes
annotations and delegates and disables user location. Accessibility identifiers
are `native.map` and `native.map.pin.<id>`; pin labels use their titles.

## Location permission

Build with `--location-usage "Show your position on the map."` to add
`NSLocationWhenInUseUsageDescription`. The default empty flag disables this
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

NOT RUN: physical-device location, permission UX acceptance, VoiceOver, and
hardware performance. Simulator compilation or launch is not evidence of these.
