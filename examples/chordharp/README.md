# Chord harp

A chord harp in two forms that share one engine: a terminal app built on
the `tui` module, and a phone app for Android and iOS built with the
native renderer. Both are laid out like the electronic autoharps of the early 1980s: a grid of 27
chords, a strumplate, and a rhythm box. `harp` holds the music and the
synthesizer.

## Terminal

```sh
cd examples/chordharp
GOWORK=off go run ./cmd/chordharp
GOWORK=off go run ./cmd/chordharp -silent   # no sound device
```

The top three letter rows hold major, minor and seventh chords, rooted from
E flat to B in fifths (`q` is E flat, `r` is C, `o` is B). Terminals send no
key releases, so a chord stays held until another is pressed, and Esc
releases it. A held-down key repeats, so a chord key only ever selects its
chord. It asks for the kitty keyboard protocol's event types, so in
kitty, Ghostty, WezTerm, foot and other terminals that have it, a held
strum key, `/`, Space, Enter or Esc acts once and every new tap counts.
Other terminals send repeats as plain presses. There Space, the one
toggle, ignores a second Space within 700 ms, a window that slides with
each repeat. Enter starts the rhythm, or restarts it from beat 1, and
Backspace stops it; neither toggles, so they act on every press. Clicking
the held chord's pad releases it, since a click does not repeat.
The number row, `1` to `=`, plucks the
strumplate's 12 zones, and dragging the mouse across the plate strums it.
A fast drag still plucks every zone it crossed. `/` sweeps the whole
plate.
The zones play the held chord's tones upward from G3, so the plate stays in
one register when the chord changes. The header keeps the audio state at
its right edge, cutting the text before it on a narrow screen. Space
starts the rhythm (rock, waltz,
slow rock, latin, foxtrot, swing), and with the rhythm running the bass and
chord accompaniment follow the held chord. The arrow keys, a click, a drag
or the wheel set volume, the chord, harp and rhythm levels, tempo and harp
sustain. A click on a control's name or number only selects it; the bar
sets the value, from 0 at its first cell to full at its last.

`?` opens help. On a short terminal the arrow and page keys scroll it,
and Esc, Enter or its Close button shut it. Ctrl+C quits from anywhere,
help included: the framework's `Modal` passes Ctrl+C and Ctrl+Z on to the
app.

The layout follows the terminal's height. From 24 rows each chord pad shows
its key under its name, and from 32 rows the pads grow taller. From 21 rows
the key goes before the name. From 17 rows section rules replace the
boxes. The strumplate keeps at least three rows in all of them. Below 60×17
the screen says how big it needs to be, and how to quit. Each strumplate
zone is labeled with its key and note, and state that colors show (the held
chord, the beat, the pattern) also has a mark or an attribute, for
`NO_COLOR` and 16-color terminals.

The synthesizer in `harp` is plain Go: additive voices for the pad and harp, and
synthesized drums. Plucks scheduled ahead, such as the sweep, start on an
exact sample. On macOS with cgo it plays through an AudioQueue with three
256-frame buffers, about 17 ms. On Windows it plays through WinMM's
`waveOut`, loaded from `winmm.dll` with `x/sys/windows`, so a
cross-compiled `GOOS=windows` build needs neither cgo nor anything
installed: four 768-frame buffers of 16-bit PCM, about 70 ms queued,
refilled as the driver signals each one done. Without cgo on macOS, and on
other systems, it pipes samples to the first of `pw-cat`, `pacat`, `aplay`
or `ffplay` on `PATH`, staying 30 ms ahead of the clock. The tests drive the engine and the screen
through a real tree. `CHORDHARP_AUDIO_TEST=1` also opens the sound device,
with nothing playing, and checks that it pulls samples at 44.1 kHz.

## Phone

The phone app is a native view, an Android `View` or a UIKit `UIView`,
that calls the same engine directly.

```sh
cd examples/chordharp
GOWORK=off go run github.com/DonaldMurillo/gofastr-native/cmd/gofastr-native run \
  --platform android-emu --renderer native --pkg ./chordharp \
  --name "Chord Harp" --id dev.gofastr.chordharp --device <avd>
GOWORK=off go run github.com/DonaldMurillo/gofastr-native/cmd/gofastr-native run \
  --platform ios-sim --renderer native --pkg ./chordharp \
  --name "Chord Harp" --id dev.gofastr.chordharp
```

Run it from this directory: the example is its own module, so it can
require the tui module (through `replace`) without the root module
depending on it. `adb install` the APK it builds to put it on an Android
phone.

### Playing on a phone

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
| `harp/` | The music and the synthesizer: chord spelling, the strumplate's notes, the accompaniment patterns, and an engine that renders mono samples. Both apps use it. |
| `cmd/chordharp/` | The terminal app and its sound output for macOS, Windows and other systems. |
| `chordharp/` | The gomobile bind package. `Start`, `Stop`, `Root`, `Render`, `Dispatch` and `Command` serve the one `nativeui` screen. The instrument view calls `Open`, `Fill`, `Hold`, `Release`, `Pluck`, `Toggle`, `NextPattern`, `NudgeTempo`, `SetLatch`, `Step` and `State`. |
| `harpview/` | The `dev.gofastr.chordharp/v1` rendering component. Go sends the grid labels and pattern names. `ChordHarpView.java` and `ChordHarp.swift` each draw the instrument, track each finger, and run the audio output. |
| `screens/` | The screen: a title and the component. |

Touches never wait on a screen render. The view calls the bound Go
functions on the UI thread, and audio is pulled from `Fill` on a thread
of its own. The chord, latch and rhythm live in Go, so a rebuilt view
comes back where the old one left off.

On Android a separate thread at urgent-audio priority
pulls 16-bit mono PCM from `Fill` into an `AudioTrack` at the device's
native rate. The track asks for low-latency mode on Android 8 and later.
Sound stops when the window is hidden or the host suspends the screen, and
starts again when the activity resumes or the window comes back. The
resume hook matters: a relaunch intent pauses and resumes the activity
without the window ever losing focus, and before the hook that left the
app silent while the screen still responded. If the audio track fails, the
thread logs it and opens a new one.

On iOS an audio queue plays the samples at the audio session's rate, from
three 256-frame buffers (about 16 ms at 48 kHz), as the terminal app does
on macOS. The queue calls `Fill` on its own thread rather than the
real-time I/O thread, where Go's allocation and locking could make the
output underrun, and the buffers queued ahead cover a short stall in Go.
The session uses the playback category, mixed with other apps' audio. The
queue follows output route changes by itself. Sound stops when the view
leaves the window or the app goes to the background, and a new queue
starts when the app is active again, after an interruption such as a call
ends, or when the media server resets. The view sizes itself to the
host's scroll view and turns that scroll view's scrolling and touch delay
off while it is on screen, so a strum is never taken for a scroll and
touches arrive without the 150 ms scroll-detection delay. VoiceOver
reaches Start/Stop and Latch as buttons, the rhythm and tempo as
adjustable values (swipe up or down), and the held chord as text. The
pads and strumplate are one element that allows direct interaction, so
the harp can be played with VoiceOver on.

`ChordHarpView.java` imports the bound package's Java class
(`chordharp.Chordharp`) and `ChordHarp.swift` imports its framework
(`Chordharp`), so the component only builds with this app's bind package.
The WebView adapter is a placeholder that says the instrument plays in
the native renderers. The manifest requires all three.

## Tests

```sh
cd examples/chordharp
GOWORK=off go test -race ./...
```

The terminal tests drive the engine and the screen through a real view
tree. The Go tests for the Android side cover the bind surface (silence
before `Open`, a pluck that sounds, range checks, tempo limits, pattern
wrapping, state kept across `Open`), the component's payload checks and
the screen.

The Java and Swift views have no automated tests. The Java view was
checked on an API 35 emulator
in portrait and landscape: latch, chord holding, strum glow, the rhythm and
its beat lights, and sound carrying on across a pause and resume. Emulator
audio is not a fair test of the sound itself: on a loaded Mac the
emulator's virtual sound card crackles and then fails with I/O errors while
the app's track keeps running. It has not been played on a physical phone,
where latency and multi-touch matter most.

The Swift view was checked on the iOS 26.5 simulator (iPhone 17 Pro) in
portrait: the instrument fills the screen under the title on every launch,
Latch holds and a second tap releases, a tap without Latch plays and lets
go, a strum lights the zones it crossed, Start lights the beats, a held +
repeats, the pattern arrows step through the rhythms, the VoiceOver
elements cover the controls they name, and activating the Latch element
turns Latch on. A temporary counter in the queue callback showed it
pulling 48,000 frames a second, stopping in the background and pulling
again within about 2 s of returning. Landscape, multi-touch, VoiceOver
itself and listening to the output were NOT RUN on iOS: the simulator
automation has no rotation or multi-finger drag, the elements were read
through the accessibility tree with VoiceOver off, and the checks were
made without listening. It has not run on a physical iPhone.

Windows has been compiled and vetted, not run. On Windows,
`CHORDHARP_AUDIO_TEST=1 go test ./cmd/chordharp` opens the WinMM output,
and `TestWaveStructLayout` checks the structs against the C layout.
