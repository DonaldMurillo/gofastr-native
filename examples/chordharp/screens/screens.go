// Package screens defines the chord harp's one screen: the instrument.
package screens

import (
	"context"

	"github.com/DonaldMurillo/gofastr-native/extension"
	"github.com/DonaldMurillo/gofastr-native/nativeui"

	"github.com/DonaldMurillo/gofastr-native/examples/chordharp/harpview"
)

// Root is the instrument's route.
const Root = "/harp"

// New returns the app.
func New() (*nativeui.App, error) {
	return nativeui.NewWithExtensions(Root, []extension.Definition{harpview.Definition()},
		nativeui.Route{Path: Root, Screen: instrument})
}

func instrument(context.Context, nativeui.Request) (nativeui.Screen, error) {
	block, err := harpview.Block("instrument")
	if err != nil {
		return nativeui.Screen{}, err
	}
	return nativeui.Screen{Title: "Chord Harp", Components: []extension.Component{block}}, nil
}
