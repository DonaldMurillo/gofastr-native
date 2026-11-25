// Package arkit provides a compiled iOS ARKit world-tracking rendering component.
package arkit

import (
	"embed"
	"encoding/json"
	"errors"
	"maps"
	"math"
	"unicode"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr-native/behavior"
	"github.com/DonaldMurillo/gofastr-native/extension"
)

// Name is the compiled component capability.
const Name = "gofastr.arkit/v1"

// MaxObjects bounds the virtual objects in one scene.
const MaxObjects = 64

// Plane is a surface alignment ARKit detects.
type Plane string

// Detectable surface alignments.
const (
	Horizontal Plane = "horizontal"
	Vertical   Plane = "vertical"
)

// Shape is a built-in SceneKit primitive.
type Shape string

// Built-in object shapes.
const (
	Box      Shape = "box"
	Sphere   Shape = "sphere"
	Cylinder Shape = "cylinder"
	Cone     Shape = "cone"
	Pyramid  Shape = "pyramid"
)

// Vector is a position in meters in the session's world space: the origin is
// where the device was when tracking started, +Y is up and -Z is forward.
type Vector struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// Object is a keyed virtual object. Without Position it waits for the person
// to tap a detected surface, which places it there.
type Object struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Shape       Shape   `json:"shape"`
	Size        float64 `json:"size"`
	Color       string  `json:"color"`
	Position    *Vector `json:"position,omitempty"`
	Destination string  `json:"destination,omitempty"`
}

// Content is an immutable AR scene snapshot.
type Content struct {
	Planes          []Plane  `json:"planes"`
	ShowsPlanes     bool     `json:"showsPlanes"`
	Coaching        bool     `json:"coaching"`
	LightEstimation bool     `json:"lightEstimation"`
	Objects         []Object `json:"objects"`
}

//go:embed gofastr-native.extension.json GofastrAR.swift
var assets embed.FS

// Definition returns the compiled iOS adapter and authoritative Go validation.
func Definition() extension.Definition {
	data, _ := assets.ReadFile(extension.ManifestName)
	manifest, _ := extension.ParseManifest(data)
	return extension.Definition{Manifest: manifest, Assets: assets, ValidateComponent: validate}
}

// Block validates and freezes content and its navigation event allowlist.
func Block(id string, content Content) (extension.Component, error) {
	data, err := json.Marshal(content)
	if err != nil {
		return extension.Component{}, err
	}
	component := extension.Component{ID: id, Name: Name, Payload: string(data), Destinations: destinations(content)}
	if err := validate(component); err != nil {
		return extension.Component{}, err
	}
	return component, nil
}

func finite(value, limit float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && math.Abs(value) <= limit
}

func title(value string) bool {
	if value == "" || !utf8.ValidString(value) || len(value) > 256 {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func color(value string) bool {
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	for _, r := range value[1:] {
		if !('0' <= r && r <= '9' || 'a' <= r && r <= 'f' || 'A' <= r && r <= 'F') {
			return false
		}
	}
	return true
}

func validate(component extension.Component) error {
	if component.Name != Name || !behavior.SafeToken(component.ID) {
		return errors.New("arkit: invalid identity")
	}
	var content Content
	if err := extension.DecodeComponent(component.Payload, &content); err != nil {
		return err
	}
	planes := map[Plane]bool{}
	for _, plane := range content.Planes {
		if plane != Horizontal && plane != Vertical || planes[plane] {
			return errors.New("arkit: planes must be distinct horizontal or vertical")
		}
		planes[plane] = true
	}
	if content.ShowsPlanes && len(planes) == 0 {
		return errors.New("arkit: showing planes requires plane detection")
	}
	if len(content.Objects) > MaxObjects {
		return errors.New("arkit: at most 64 objects")
	}
	seen := map[string]bool{}
	for _, o := range content.Objects {
		if !behavior.SafeToken(o.ID) || len(o.ID) > 64 || seen[o.ID] || !title(o.Title) || !color(o.Color) || len(o.Destination) > 4096 {
			return errors.New("arkit: invalid object")
		}
		switch o.Shape {
		case Box, Sphere, Cylinder, Cone, Pyramid:
		default:
			return errors.New("arkit: unknown object shape")
		}
		if !finite(o.Size, 2) || o.Size < 0.01 {
			return errors.New("arkit: object size must be 0.01 to 2 meters")
		}
		if p := o.Position; p != nil && (!finite(p.X, 50) || !finite(p.Y, 50) || !finite(p.Z, 50)) {
			return errors.New("arkit: object position must be within 50 meters of the origin")
		}
		if o.Position == nil && len(planes) == 0 {
			return errors.New("arkit: tap-to-place objects require plane detection")
		}
		seen[o.ID] = true
	}
	if !maps.Equal(component.Destinations, destinations(content)) {
		return errors.New("arkit: navigation events do not match the content")
	}
	return nil
}

func destinations(content Content) map[string]string {
	result := map[string]string{}
	for _, o := range content.Objects {
		if o.Destination != "" {
			result["object."+o.ID] = o.Destination
		}
	}
	return result
}
