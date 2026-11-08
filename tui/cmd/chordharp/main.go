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
	"time"

	"github.com/DonaldMurillo/gofastr-native/tui/cmd/chordharp/harp"

	"github.com/DonaldMurillo/gofastr-native/tui/app"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
)

func main() {
	quiet := flag.Bool("silent", false, "run without sound")
	flag.Parse()

	eng := harp.NewEngine(sampleRate)
	var out output = silent{reason: "off (-silent)"}
	if !*quiet {
		o, err := openAudio(eng)
		if err != nil {
			fmt.Fprintln(os.Stderr, "chordharp: no sound:", err)
			o = silent{reason: "none"}
		}
		out = o
	}
	defer out.Close()

	m := newModel(eng, out.Name(), time.Now)
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
