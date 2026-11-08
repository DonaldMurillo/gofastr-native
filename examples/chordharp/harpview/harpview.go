// Package harpview is the chord harp's instrument component: the chord
// grid, the strumplate and the rhythm bar, drawn by a compiled Android view.
// The view plays sound by calling the bound chordharp package directly, so
// touches reach the synthesizer without a screen render in between.
package harpview

import (
	"embed"
	"encoding/json"
	"errors"

	"github.com/DonaldMurillo/gofastr-native/behavior"
	"github.com/DonaldMurillo/gofastr-native/examples/chordharp/harp"
	"github.com/DonaldMurillo/gofastr-native/extension"
	"github.com/DonaldMurillo/gofastr-native/extension/webassets"
)

// Name is the component's rendering capability.
const Name = "dev.gofastr.chordharp/v1"

// Content is the instrument's static layout: the grid's labels and the
// rhythm names. Live state (the held chord, tempo) stays in Go and the
// view reads it through the bound package.
type Content struct {
	Kinds    []string   `json:"kinds"`    // row labels, major first
	Chords   [][]string `json:"chords"`   // chord names by kind, then column
	Patterns []string   `json:"patterns"` // rhythm names in menu order
}

//go:embed gofastr-native.extension.json *.java ChordHarp.swift harp.js
var assets embed.FS

func init() { webassets.Register(Definition()) }

// Layout returns the grid and rhythm labels from the shared music code.
func Layout() Content {
	var c Content
	for kind := harp.Major; kind < harp.Kinds; kind++ {
		c.Kinds = append(c.Kinds, kind.String())
		row := make([]string, 0, len(harp.Roots))
		for _, root := range harp.Roots {
			row = append(row, harp.Chord{Root: root, Kind: kind}.String())
		}
		c.Chords = append(c.Chords, row)
	}
	for _, p := range harp.Patterns {
		c.Patterns = append(c.Patterns, p.Name)
	}
	return c
}

// Block returns the instrument as a keyed component.
func Block(id string) (extension.Component, error) {
	payload, err := json.Marshal(Layout())
	if err != nil {
		return extension.Component{}, err
	}
	component := extension.Component{ID: id, Name: Name, Payload: string(payload)}
	if err := validate(component); err != nil {
		return extension.Component{}, err
	}
	return component, nil
}

// Definition registers the component's sources and payload check.
func Definition() extension.Definition {
	content, _ := assets.ReadFile(extension.ManifestName)
	manifest, _ := extension.ParseManifest(content)
	return extension.Definition{Manifest: manifest, Assets: assets, ValidateComponent: validate}
}

func validate(component extension.Component) error {
	if component.Name != Name || !behavior.SafeToken(component.ID) {
		return errors.New("harpview: invalid component identity")
	}
	if len(component.Destinations) != 0 {
		return errors.New("harpview: the instrument does not navigate")
	}
	var c Content
	if err := extension.DecodeComponent(component.Payload, &c); err != nil {
		return err
	}
	if len(c.Kinds) != int(harp.Kinds) || len(c.Chords) != int(harp.Kinds) || len(c.Patterns) == 0 || len(c.Patterns) > 16 {
		return errors.New("harpview: expected three chord rows and 1 to 16 patterns")
	}
	for _, row := range c.Chords {
		if len(row) != len(harp.Roots) {
			return errors.New("harpview: every chord row needs one chord per root")
		}
		for _, name := range row {
			if name == "" || len(name) > 8 {
				return errors.New("harpview: invalid chord name")
			}
		}
	}
	for _, name := range append(append([]string(nil), c.Kinds...), c.Patterns...) {
		if name == "" || len(name) > 24 {
			return errors.New("harpview: invalid label")
		}
	}
	return nil
}
