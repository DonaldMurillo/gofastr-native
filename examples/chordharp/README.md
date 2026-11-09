# Chord harp

A chord harp in two forms that share one engine: a terminal app built on
the `tui` module, and an Android app built with the native renderer. Both
are laid out like the electronic autoharps of the early 1980s: a grid of 27
chords, a strumplate, and a rhythm box. `harp` holds the music and the
synthesizer.

## Terminal

```sh
cd examples/chordharp
GOWORK=off go run ./cmd/chordharp
GOWORK=off go run ./cmd/chordharp -silent   # no sound device
GOWORK=off go run ./cmd/chordharp -takes ~/Music   # where Ctrl+E saves takes
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
The zones play the held chord's tones upward from G4 in the default sound
(G3 in Classic), so the plate stays in one register when the chord changes. The header keeps the audio state at
its right edge, cutting the text before it on a narrow screen. Space
starts the rhythm (rock, waltz,
slow rock, latin, foxtrot, swing), and with the rhythm running the bass and
chord accompaniment follow the held chord. The arrow keys, a click, a drag
or the wheel set volume, the chord, harp and rhythm levels, tempo and harp
sustain. A click on a control's name or number only selects it; the bar
sets the value, from 0 at its first cell to full at its last.

### Sound and takes

Tab switches the panel beside the rhythm between Mix, Sound and Take, and
Shift+Tab goes back. Every page is as tall as Mix, so the rest of the
screen stays put.

Sound has three rows. Preset picks Classic, Bright or High. Octave moves
the whole instrument from -1 to +2 octaves; the bass follows only from 0
to +1. Tone sets how much upper harmonic content the voices have, 0 to
100%. Changing Octave or Tone by hand makes the preset Custom. Bright
(+1 octave, 60% tone) is the default. Classic is the sound before presets
existed, and a test holds it to a fingerprint of that sound. The sound is
saved to `chordharp/settings.json` in the user config folder
(`os.UserConfigDir`) and read at the next start.

Ctrl+R records a take, and Ctrl+R again stops it. A take stops itself at
five minutes. It holds what was played: chords, plucks, and the rhythm's
start, stop, pattern and tempo, with the rhythm's place in the bar when
recording began. It does not hold the mix or the sound. Ctrl+P plays the
take back with the current mix and sound, so one take can be heard in
each preset. Playing a chord, strumming, or a rhythm key stops playback
and puts back the chord and rhythm you had. Mix and sound changes do not
stop it. While it plays, the header shows `▶ take 0:05 / 0:45` and the
chords, plate and rhythm show the take. While recording, the header shows
`● REC 0:12`.

Ctrl+E saves the take as `chordharp-YYYYMMDD-HHMMSS.mid` and `.wav` in the
`-takes` folder, the current one by default. It renders the take again,
offline, through a fresh engine at 44.1 kHz. The WAV is 16-bit mono with up
to three seconds of tail. The MIDI file is type 1 at 480 ticks per quarter
note, following the take's tempo changes, with a track each for the harp
(General MIDI orchestral harp), the chord pad (warm pad), the bass
(acoustic bass), the chord accompaniment (electric piano) and the drums on
channel 10. The save runs off the UI loop, and the Take page says where it
went or why it failed.

With `-silent`, or when no sound device opens, a clock still runs the
engine in real time, so the rhythm, recording and playback all move.

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

## Android

The Android app is a native view that calls the same engine directly.

```sh
cd examples/chordharp
GOWORK=off go run github.com/DonaldMurillo/gofastr-native/cmd/gofastr-native run \
  --platform android-emu --renderer native --pkg ./chordharp \
  --name "Chord Harp" --id dev.gofastr.chordharp --device <avd>
```

Run it from this directory: the example is its own module, so it can
require the tui module (through `replace`) without the root module
depending on it. `adb install` the APK it builds to put it on a phone.

### Playing on Android

- The grid has 27 chords: major, minor and seventh, rooted from E flat to B
  in fifths. In portrait the chord kinds are columns. In landscape they are
  rows.
- A chord sounds while a finger is on its pad. Sliding to another pad
  changes the chord, and when two fingers are down the later one wins.
  With Latch on, a tap holds the chord until its pad is tapped again.
- The strumplate on the right has 12 zones that play the held chord's
  tones up from G4 in the default sound, low at the bottom. A swipe plucks every zone it
  crosses, a few milliseconds apart, so a fast swipe still sounds like a
  strum. Each zone shows its note and lights when plucked.
- The bar starts and stops the rhythm (rock, waltz, slow rock, latin,
  foxtrot, swing) and sets the tempo from 50 to 200 bpm. Holding − or +
  repeats. The dots light on each beat. With the rhythm running, the bass
  and chord accompaniment follow the held chord.
- Rec records a take and shows its time; tap it again to stop. Play plays
  the take back and lights the plate where it strums. Touching a chord,
  the plate or the rhythm stops playback, as in the terminal. Leaving the
  app ends a recording and keeps the take, until the app's process ends.
- Share renders the take on a worker thread into the app's private
  transfer folder and opens the share sheet with the `.mid` and `.wav`.
- Sound opens a panel over the chord grid with the presets and steppers
  for octave and tone. The strumplate stays live beside it, so a change can
  be heard as it is made. The sound is kept in the app's data folder.

## How it is built

| Path | What it is |
|---|---|
| `harp/` | The music and the synthesizer: chord spelling, the sound presets and the notes they give the plate, pad and bass, the accompaniment patterns, an engine that renders mono samples, takes and their playback, and MIDI and WAV export. Both apps use it. |
| `cmd/chordharp/` | The terminal app and its sound output for macOS, Windows and other systems. |
| `chordharp/` | The gomobile bind package. `Start`, `Stop`, `Root`, `Render`, `Dispatch` and `Command` serve the one `nativeui` screen. The instrument view calls `Open`, `Fill`, `Hold`, `Release`, `Pluck`, `Toggle`, `NextPattern`, `NudgeTempo`, `SetLatch`, `SetPreset`, `NudgeOctave`, `NudgeTone`, `Record`, `PlayTake`, `Export`, `Step` and `State`. `Start` reads the saved sound from its data folder. |
| `harpview/` | The `dev.gofastr.chordharp/v1` rendering component. Go sends the grid labels, pattern names and preset names. `ChordHarpView.java` draws the instrument on a canvas, tracks each pointer, and runs the audio thread. |
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

The terminal tests drive the engine and the screen through a real view
tree. The Go tests for the Android side cover the bind surface (silence
before `Open`, a pluck that sounds, range checks, tempo limits, pattern
wrapping, state kept across `Open`), the component's payload checks and
the screen.

The Java view has no automated tests. It was checked on an API 35 emulator
in portrait and landscape: latch, chord holding, strum glow, the rhythm and
its beat lights, and sound carrying on across a pause and resume. Emulator
audio is not a fair test of the sound itself: on a loaded Mac the
emulator's virtual sound card crackles and then fails with I/O errors while
the app's track keeps running. It has not been played on a physical phone,
where latency and multi-touch matter most.

The take row, the Sound panel and Share were checked on an API 34 Pixel 5
emulator in portrait, from an APK built with
`gofastr-native build --platform android-emu --renderer native`:
Rec counts up in red, Stop keeps the take, Play shows the take's chord
and then puts the held chord back when it ends, the presets and steppers
change the plate's notes and show Custom, the sound survives a relaunch,
and Share opens the share sheet with both files. NOT RUN: landscape and
tablet widths for the new row, opening the shared files on another app
(the APK is not marked debuggable, so `run-as` cannot read its files), TalkBack, and listening
to the presets on a phone.

Windows has been compiled and vetted, not run. On Windows,
`CHORDHARP_AUDIO_TEST=1 go test ./cmd/chordharp` opens the WinMM output,
and `TestWaveStructLayout` checks the structs against the C layout.
