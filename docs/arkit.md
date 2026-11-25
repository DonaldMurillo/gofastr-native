# iOS AR scenes

`extension/arkit` is an iOS-only compiled ARKit component, capability
`gofastr.arkit/v1`. Register `arkit.Definition()` with `nativeui.NewWithExtensions`.
Other renderers do not advertise it and refuse AR screens with an unsupported
capability error. No WebView or Android adapter is supplied.

```go
block, err := arkit.Block("scene", arkit.Content{
    Planes:      []arkit.Plane{arkit.Horizontal},
    ShowsPlanes: true,
    Coaching:    true,
    Objects: []arkit.Object{
        {ID: "cube", Title: "Blue cube", Shape: arkit.Box, Size: 0.25, Color: "#2F6FDE",
            Position: &arkit.Vector{Y: -0.45, Z: -1.2}, Destination: "/objects/cube"},
        {ID: "ball", Title: "Orange ball", Shape: arkit.Sphere, Size: 0.15, Color: "#F28C28"},
    },
})
```

Put the block in `Screen.Components` and handle `err`.

## Content

- `Planes` turns on ARKit plane detection: `Horizontal`, `Vertical` or both,
  each at most once. `ShowsPlanes` draws detected planes as translucent
  overlays and requires at least one plane kind.
- `Coaching` adds the system coaching overlay, whose goal follows `Planes`.
  `LightEstimation` turns on ARKit light estimation.
- `Objects` holds at most 64 built-in primitives: `Box`, `Sphere`,
  `Cylinder`, `Cone` or `Pyramid`. Each has a unique `behavior.SafeToken` ID
  of at most 64 bytes, a plain UTF-8 title without controls of 1 to 256 bytes
  (its accessibility label), a size from 0.01 to 2 meters and a `#RRGGBB`
  color.
- An object with `Position` is fixed in world space, in meters relative to
  where the session started (negative Z is in front). Each coordinate is
  finite and within 50 meters.
- An object without `Position` is placed by the person: objects are offered in
  order, and each tap on a detected surface places the next one on it. These
  objects require plane detection. `Reset` under the scene restarts tracking
  and clears placements.
- `Destination` makes an object open a registered local route when tapped.
  Only the component's exact `object.<id>` destination allowlist authorizes
  navigation; Swift never routes using payload destinations.

The payload is a JSON string with `planes`, `showsPlanes`, `coaching`,
`lightEstimation` and `objects` (`id`, `title`, `shape`, `size`, `color`,
optional `position` with `x`, `y`, `z`, optional `destination`). Swift
re-validates every bound before building the scene.

## Camera and lifecycle

ARKit needs the camera. The native iOS build already writes
`NSCameraUsageDescription` from `--camera-usage`; give it text that also covers
AR, for example `--camera-usage "Use the camera for photos and AR scenes."`.
Building with an empty `--camera-usage` turns AR off.

The 360-point scene is retained on updates; objects reconcile by ID and
callbacks rebind to the fresh emitter. The session runs only while the view is
in a window and pauses when it leaves. The component shows a status line under
the scene instead of the scene when:

- the device does not support ARKit world tracking (every simulator, older
  devices): "AR needs an iPhone or iPad that supports ARKit world tracking."
- the app has no camera purpose text;
- camera access is denied or restricted, or the session fails.

Tracking states (initializing, excessive motion, too few features,
relocalizing, interruption) are shown in the same line. Nothing about the
camera feed, anchors or placements is sent to Go; only `object.<id>` taps are.
Disposal pauses the session and removes delegates, nodes and gestures.

Accessibility identifiers are `native.ar`, `native.ar.status` (or
`native.ar.unsupported`), `native.ar.reset` and `native.ar.object.<id>`. Each
visible object is an accessibility element framed over its on-screen
projection, labelled with its title; navigable objects are buttons.

## Simulator fixture

The simulator has no world tracking, so a real AR scene only runs on a device.
For UI automation, launching a simulator build with the environment variable
`GOFASTR_AR_FIXTURE` set replaces the session with a fixed camera over one
synthetic horizontal surface. Fixed objects, tap-to-place, reset, navigation
and accessibility then run through the same code paths. The variable is
ignored on devices.

## Verification

Go tests cover bounds, NaN and infinite values, colors, duplicates, 64
objects, the destination allowlist, iOS-only manifests and unsupported
renderer negotiation. Renderer tests execute Foundation payload decoding and
status messages, and compile/link the ARKit-enabled optimized UIKit source set
for the iOS simulator. See `examples/gallery/gallerynative` (Controls, AR scene)
for a Go-only scene and detail route.

NOT RUN: physical-device world tracking, plane detection, raycast placement,
coaching overlay, camera permission UX, VoiceOver, and hardware performance.
Simulator fixture runs are not evidence of these.

### Simulator UI acceptance

The UI-test-only project in `extension/arkit/uitest` uses XCUITest against the
separately installed CLI-built gallery. Boot an iPhone simulator and run from
the repository root:

```sh
GOFASTR_IOS_UITEST=1 \
GOFASTR_IOS_DEVICE="<booted-simulator-udid>" \
GOFASTR_IOS_UITEST_OUT="$HOME/arkit-simulator/acceptance" \
GOWORK=off go test ./extension/arkit -run TestGalleryARUI -v -count=1 -timeout=20m
```

The output directory must be outside the repository and must not already
contain `ARKit.xcresult`. The wrapper builds and installs the gallery, then runs
the shared `ARKitUITests` scheme serially. The three tests check the
unsupported status without the fixture, fixed-object navigation, and
tap-to-place, navigation and reset with the fixture. They passed on iPhone
17 with iOS 26.5: three tests, zero failures. The captured fixture scene showed
the blue cube and the orange ball placed on the synthetic surface.
