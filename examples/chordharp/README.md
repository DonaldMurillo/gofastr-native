# Chord harp for Android

The terminal chord harp (`tui/cmd/chordharp`) as an Android app. Both apps
play through the same Go synthesizer, `tui/cmd/chordharp/harp`. The Android
instrument is a native view that calls that engine directly.

```sh
cd examples/chordharp
GOWORK=off go run github.com/DonaldMurillo/gofastr-native/cmd/gofastr-native run \
  --platform android-emu --renderer native --pkg ./chordharp \
  --name "Chord Harp" --id dev.gofastr.chordharp --device <avd>
```

Run it from this directory: the example is its own module, so it can
require the tui module (through `replace`) without the root module
depending on it.

## Playing

- The grid has 27 chords: major, minor and seventh, rooted from E flat to B
  in fifths. In portrait the chord kinds are columns. In landscape they are
  rows.
- A chord sounds while a finger is on its pad. Sliding to another pad
  changes the chord, and when two fingers are down the later one wins.
  With Latch on, a tap holds the chord until its pad is tapped again.
- The strumplate on the right has 12 zones that play the held chord's
  tones up from G3, low at the bottom. A swipe plucks every zone it
  crosses, a few milliseconds apart, so a fast swipe still sounds like a
  strum. Each zone shows its note and lights when plucked.
- The bar starts and stops the rhythm (rock, waltz, slow rock, latin,
  foxtrot, swing) and sets the tempo from 50 to 200 bpm. Holding − or +
  repeats. The dots light on each beat. With the rhythm running, the bass
  and chord accompaniment follow the held chord.

## How it is built

| Path | What it is |
|---|---|
| `chordharp/` | The gomobile bind package. `Start`, `Stop`, `Root`, `Render`, `Dispatch` and `Command` serve the one `nativeui` screen. The instrument view calls `Open`, `Fill`, `Hold`, `Release`, `Pluck`, `Toggle`, `NextPattern`, `NudgeTempo`, `SetLatch`, `Step` and `State`. |
| `harpview/` | The `dev.gofastr.chordharp/v1` rendering component. Go sends the grid labels and pattern names. `ChordHarpView.java` draws the instrument on a canvas, tracks each pointer, and runs the audio thread. |
| `screens/` | The screen: a title and the component. |

Touches never wait on a screen render. The view calls the bound Go
functions on the UI thread, and a separate thread at urgent-audio priority
pulls 16-bit mono PCM from `Fill` into an `AudioTrack` at the device's
native rate. The track asks for low-latency mode on Android 8 and later.
Sound stops when the window is hidden or the host suspends the screen, and
starts again when the activity resumes or the window comes back. The
resume hook matters: a relaunch intent pauses and resumes the activity
without the window ever losing focus, and before the hook that left the
app silent while the screen still responded. If the audio track fails, the
thread logs it and opens a new one. The chord, latch and rhythm
live in Go, so a rebuilt view comes back where the old one left off.

`ChordHarpView.java` imports the bound package's Java class
(`chordharp.Chordharp`), so the component only builds with this app's bind
package. The iOS and WebView adapters are placeholders that say the
instrument is Android-only. The manifest requires all three.

## Tests

```sh
cd examples/chordharp
GOWORK=off go test -race ./...
```

The Go tests cover the bind surface (silence before `Open`, a pluck that
sounds, range checks, tempo limits, pattern wrapping, state kept across
`Open`), the component's payload checks and the screen. The Java view has
no automated tests. It was checked on an API 35 emulator in portrait and
landscape: latch, chord holding, strum glow, the rhythm and its beat
lights, with AudioFlinger showing the app's track active and no
underruns. It has not been played on a physical phone, where latency and
multi-touch matter most.
