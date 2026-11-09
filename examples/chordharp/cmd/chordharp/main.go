// Command chordharp is a chord harp for the terminal: hold a chord from a
// grid of 27, strum it on a 12-note strumplate with the mouse or the
// number row, and play over a drum machine with bass and chord
// accompaniment. The synthesizer is written in Go; on macOS it plays
// through Core Audio, elsewhere through pw-cat, pacat, aplay or ffplay.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/DonaldMurillo/gofastr-native/examples/chordharp/harp"

	"github.com/DonaldMurillo/gofastr-native/tui/app"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
)

func main() {
	quiet := flag.Bool("silent", false, "run without sound")
	takes := flag.String("takes", ".", "the folder Ctrl+E saves takes in")
	flag.Parse()

	eng := harp.NewEngine(sampleRate)
	var out output
	if *quiet {
		out = openSilent(eng, "off (-silent)")
	} else {
		o, err := openAudio(eng)
		if err != nil {
			fmt.Fprintln(os.Stderr, "chordharp: no sound:", err)
			o = openSilent(eng, "none")
		}
		out = o
	}
	defer out.Close()

	m := newModel(eng, out.Name(), time.Now)
	m.takesDir = *takes
	// The sound is kept in the user's config folder, when there is one.
	if dir, err := os.UserConfigDir(); err == nil {
		m.settings = filepath.Join(dir, "chordharp", "settings.json")
		sound, err := harp.LoadSound(m.settings)
		if err != nil {
			m.soundNote = "Settings unreadable, using " + sound.Name()
		}
		m.sound = sound
		m.apply()
	}
	err := app.Run(func(*app.App) view.Element { return m.view() }, app.Options{
		// Terminals with the kitty keyboard protocol mark a held key's
		// repeats, so quick taps of Space are never mistaken for one.
		KeyEvents: true,
		Start: func(a *app.App) {
			// Redraw while zones fade and the beat moves. A frame with
			// nothing new writes nothing to the terminal.
			a.Every(33*time.Millisecond, func() {})
		},
	})
	if err != nil {
		out.Close()
		fmt.Fprintln(os.Stderr, "chordharp:", err)
		os.Exit(1)
	}
}
