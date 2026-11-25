// Package ar describes local augmented reality availability and plane placement.
package ar

import (
	"errors"
	"math"
)

type Operation string

const (
	Check Operation = "check"
	Place Operation = "place"
)

type Availability string

const (
	Ready         Availability = "ready"
	InstallNeeded Availability = "install-needed"
	UpdateNeeded  Availability = "update-needed"
	Unsupported   Availability = "unsupported"
	Unknown       Availability = "unknown"
)

type Request struct {
	Operation Operation `json:"operation"`
}

type Result struct {
	Availability Availability `json:"availability,omitempty"`
	// Position is the anchor translation in meters (x, y, z). It is relative
	// to the just-ended session, not shareable or persistent world coordinates.
	Position []float64 `json:"position,omitempty"`
}

func (r Request) Validate() error {
	if r.Operation != Check && r.Operation != Place {
		return errors.New("ar: unknown operation")
	}
	return nil
}

func (r Request) Accept(result Result) error {
	switch r.Operation {
	case Check:
		if len(result.Position) == 0 {
			switch result.Availability {
			case Ready, InstallNeeded, UpdateNeeded, Unsupported, Unknown:
				return nil
			}
		}
	case Place:
		if result.Availability == "" && len(result.Position) == 3 {
			for _, coordinate := range result.Position {
				if math.IsNaN(coordinate) || math.IsInf(coordinate, 0) {
					return errors.New("ar: anchor position must be finite")
				}
			}
			return nil
		}
	}
	return errors.New("ar: result does not match the operation")
}
