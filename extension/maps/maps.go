// Package maps provides a compiled iOS MapKit rendering component.
package maps

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

const Name = "gofastr.maps/v1"

type Region struct {
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	LatitudeDelta  float64 `json:"latitudeDelta"`
	LongitudeDelta float64 `json:"longitudeDelta"`
}

type Pin struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Subtitle    string  `json:"subtitle"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Destination string  `json:"destination,omitempty"`
}

type Content struct {
	Region            *Region `json:"region,omitempty"`
	Pins              []Pin   `json:"pins"`
	ShowsUserLocation bool    `json:"showsUserLocation"`
}

//go:embed gofastr-native.extension.json GofastrMaps.swift
var assets embed.FS

func Definition() extension.Definition {
	data, _ := assets.ReadFile(extension.ManifestName)
	manifest, _ := extension.ParseManifest(data)
	return extension.Definition{Manifest: manifest, Assets: assets, ValidateComponent: validate}
}

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

func bounded(value, min, max float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= min && value <= max
}
func text(value string, limit int) bool {
	if !utf8.ValidString(value) || len(value) > limit {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func validate(component extension.Component) error {
	if component.Name != Name || !behavior.SafeToken(component.ID) {
		return errors.New("maps: invalid identity")
	}
	var content Content
	if err := extension.DecodeComponent(component.Payload, &content); err != nil {
		return err
	}
	if len(content.Pins) > 200 || content.Region == nil && len(content.Pins) == 0 {
		return errors.New("maps: expected a region or 1 to 200 pins")
	}
	if r := content.Region; r != nil {
		if !bounded(r.Latitude, -90, 90) || !bounded(r.Longitude, -180, 180) || !bounded(r.LatitudeDelta, 0, 180) || r.LatitudeDelta == 0 || !bounded(r.LongitudeDelta, 0, 360) || r.LongitudeDelta == 0 {
			return errors.New("maps: invalid region")
		}
	}
	seen := map[string]bool{}
	for _, p := range content.Pins {
		if !behavior.SafeToken(p.ID) || seen[p.ID] || !bounded(p.Latitude, -90, 90) || !bounded(p.Longitude, -180, 180) || !text(p.Title, 256) || !text(p.Subtitle, 1024) || len(p.Destination) > 4096 {
			return errors.New("maps: invalid pin")
		}
		seen[p.ID] = true
	}
	if !maps.Equal(component.Destinations, destinations(content)) {
		return errors.New("maps: navigation events do not match the content")
	}
	return nil
}
func destinations(content Content) map[string]string {
	result := map[string]string{}
	for _, p := range content.Pins {
		if p.Destination != "" {
			result["pin."+p.ID] = p.Destination
		}
	}
	return result
}
