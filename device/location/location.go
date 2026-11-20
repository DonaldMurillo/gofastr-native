// Package location defines explicitly requested foreground location reads.
package location

import (
	"errors"
	"math"
	"time"
)

type Operation string

const (
	Current Operation = "current"
	Status  Operation = "status"
)

type Accuracy string

const (
	Precise     Accuracy = "precise"
	Approximate Accuracy = "approximate"
)

type Request struct {
	Operation      Operation `json:"operation"`
	Accuracy       Accuracy  `json:"accuracy,omitempty"`
	MaxAgeSeconds  int       `json:"maxAgeSeconds,omitempty"`
	TimeoutSeconds int       `json:"timeoutSeconds,omitempty"`
}

type Fix struct {
	Latitude       float64   `json:"latitude"`
	Longitude      float64   `json:"longitude"`
	AccuracyMeters float64   `json:"accuracyMeters"`
	Altitude       *float64  `json:"altitude,omitempty"`
	Speed          *float64  `json:"speed,omitempty"`
	Bearing        *float64  `json:"bearing,omitempty"`
	Time           time.Time `json:"time"`
}

type Result struct {
	Fix             *Fix   `json:"fix,omitempty"`
	Permission      string `json:"permission"`
	ServicesEnabled bool   `json:"servicesEnabled"`
}

func (request Request) Validate() error {
	if request.Operation == Status {
		if request.Accuracy != "" || request.MaxAgeSeconds != 0 || request.TimeoutSeconds != 0 {
			return errors.New("location: status has no arguments")
		}
		return nil
	}
	if request.Operation != Current || (request.Accuracy != Precise && request.Accuracy != Approximate) || request.MaxAgeSeconds < 0 || request.MaxAgeSeconds > 600 || request.TimeoutSeconds < 0 || request.TimeoutSeconds > 60 {
		return errors.New("location: invalid current request")
	}
	return nil
}

func (request Request) Accept(result Result) error {
	if err := request.Validate(); err != nil {
		return err
	}
	switch result.Permission {
	case "precise", "approximate", "denied", "undetermined":
	default:
		return errors.New("location: invalid permission")
	}
	if request.Operation == Status {
		if result.Fix != nil {
			return errors.New("location: status cannot return a fix")
		}
		return nil
	}
	if result.Fix == nil || !result.ServicesEnabled || (result.Permission != "precise" && result.Permission != "approximate") {
		return errors.New("location: current requires a permitted fix with services enabled")
	}
	f := result.Fix
	finite := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
	if !finite(f.Latitude) || f.Latitude < -90 || f.Latitude > 90 || !finite(f.Longitude) || f.Longitude < -180 || f.Longitude > 180 || !finite(f.AccuracyMeters) || f.AccuracyMeters < 0 || f.Time.IsZero() {
		return errors.New("location: invalid fix")
	}
	for _, v := range []*float64{f.Altitude, f.Speed, f.Bearing} {
		if v != nil && !finite(*v) {
			return errors.New("location: nonfinite fix detail")
		}
	}
	if f.Speed != nil && *f.Speed < 0 || f.Bearing != nil && (*f.Bearing < 0 || *f.Bearing >= 360) {
		return errors.New("location: invalid speed or bearing")
	}
	return nil
}
