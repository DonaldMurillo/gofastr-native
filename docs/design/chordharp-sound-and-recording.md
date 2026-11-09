# Chord harp: sound settings and recording

Status: built. The example's README lists what was checked and what was
not.

Players said the chord harp sounds dark and low. Today the chord pad sits
between F#2 and F3, the strumplate starts at G3, and the harp's upper
harmonics fade within a quarter second. This plan adds a brighter sound as
the default, settings to change it, and a way to record a session, play it
back, and export it as MIDI and WAV.

It covers the terminal app and the Android app. The iOS and WebView
adapters stay placeholders. Both features live in the shared `harp`
package, so an iOS or WebView instrument would get them by calling the
same engine.

## Sound

`harp.Sound` has two settings:

| Setting | Range | What it changes |
|---|---|---|
| Octave | -1 to +2 | Shifts the pad, the strumplate and the chord accompaniment by whole octaves. The bass moves with it, but only between 0 and +1, so it stays a bass. |
| Tone | 0 to 100% | How much upper harmonic content the pad, plucks and accompaniment have, and how long a pluck stays bright. |

Presets:

| Preset | Octave | Tone | Notes |
|---|---|---|---|
| Classic | 0 | 30% | Today's sound, sample for sample. |
| Bright | +1 | 60% | The new default. |
| High | +2 | 50% | Plate from G5. |

Changing Octave or Tone by hand shows the preset as Custom.
`testdata/classic.golden` holds the RMS of every 10 ms of a scripted
scenario rendered before this change, and a test checks that Classic still
matches it. The synthesizer drops any harmonic above 45% of the sample
rate, so the higher presets do not alias. At Classic's register no
harmonic gets that high, so the golden test still holds.

The setting is saved: in the user config directory
(`chordharp/settings.json` under `os.UserConfigDir`) for the terminal, and
in the app's data directory on Android, which `Start` already receives.

## Recording

A take is what was played, not how it was mixed. The engine records every
input that reaches it, stamped with its sample clock:

- chord held, changed or released
- a strumplate pluck, at the sample it sounds
- the rhythm started, stopped or restarted, its pattern and its tempo

A take starts with a snapshot of the chord, the rhythm and the rhythm's
position in the bar, so a take begun mid-bar plays back in time. Recording
stops itself at five minutes. A five-minute WAV is about 26 MB, inside the
32 MB that Android's private transfer storage allows.

Playback feeds the take's events back into the engine at their sample
times. It uses the current mix, sustain and sound, so one take can be
heard with each preset. Playing a chord, strumming or touching the rhythm
stops playback and returns the instrument to the state it had before;
changing the mix or the sound does not. While a take plays, both apps draw
the take's chord, rhythm and plucks from the engine instead of from their
own state.

Export renders the take offline through a fresh engine at 44.1 kHz:

- **WAV**: 16-bit mono PCM, with up to three seconds of tail so the last
  notes ring out.
- **MIDI**: a type 1 Standard MIDI File, 480 ticks per quarter note,
  following the take's tempo changes. One track each for the harp
  (General MIDI orchestral harp), the chord pad (warm pad), the bass
  (acoustic bass), the chord accompaniment (electric piano) and the drums
  on channel 10 (kick, snare, closed hat, ride, side stick).

The offline pass makes WAV and MIDI agree with each other. They can differ
slightly from the live take, because the drums' noise starts from a fresh
seed.

## Terminal

- Tab switches the mix panel between three pages: Mix (today's six
  controls), Sound (preset, octave, tone) and Take (record, play, save,
  and the take's length and last saved path). The arrow keys and the
  mouse work on each page as they do on Mix today, so the layout and the
  17-row minimum do not change.
- Ctrl+R starts and stops recording, Ctrl+P plays and stops the take, and
  Ctrl+E saves it. Every other letter is a chord key.
- The header shows `● REC 0:12` while recording and `▶ take 0:05 / 0:45`
  while playing.
- `-takes dir` sets where saved takes go (default: the current
  directory), as `chordharp-YYYYMMDD-HHMMSS.mid` and `.wav`.
- `-silent` keeps a clock running, so recording and playback still work
  with no sound device.

## Android

- The bar gets a row with Rec, Play, Share and Sound buttons and the
  take's time: three rows on a phone, two on a screen wide enough for the
  transport to fit on one.
- Sound opens a panel over the chord grid with the presets and the octave
  and tone steppers. The strumplate stays live beside it, so a change can
  be heard while it is made.
- Share renders the take on a worker thread into private transfer files
  and opens the share sheet with the `.mid` and `.wav`.
- Audio pausing (the window hidden, the activity paused) stops recording
  and keeps the take.

## Tests

- `harp`: Classic matches the golden fingerprint. Octave shifts every
  part by 12 semitones. Higher Tone raises the high-frequency energy of a
  pluck. No harmonic above the limit is rendered. A recorded session plays
  back the same events at the same samples. A take started mid-bar keeps
  its beat. Input stops playback and restores the live state. The MIDI
  file parses, with the expected tracks, tempo and note counts. The WAV
  header matches its data.
- Terminal: Tab cycles the pages, the Sound page changes the engine,
  Ctrl+R/Ctrl+P/Ctrl+E record, play and save into a temporary directory,
  and the header shows the transport.
- Android bind package: presets, octave and tone limits, settings saved
  to and read from the data directory, record and play, export to given
  paths.
- The Java view has no automated tests. It is compiled with the Android
  build, and the README records the emulator check and what it left out.
