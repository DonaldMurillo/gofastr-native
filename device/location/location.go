// Package location defines foreground-only, bounded location operations.
package location

import (
	"errors"
	"math"

	"github.com/DonaldMurillo/gofastr-native/internal/uitext"
)

type Operation string

const (
	Status    Operation = "status"
	Authorize Operation = "authorize"
	Current   Operation = "current"
	Track     Operation = "track"
)

type Accuracy string

const (
	Best     Accuracy = "best"
	Balanced Accuracy = "balanced"
	Coarse   Accuracy = "coarse"
)

type Permission string

const (
	NotDetermined Permission = "not_determined"
	Denied        Permission = "denied"
	Restricted    Permission = "restricted"
	WhenInUse     Permission = "when_in_use"
	Always        Permission = "always"
)

const DefaultTimeoutMillis = 15000

type Request struct {
	Operation            Operation `json:"operation"`
	Accuracy             Accuracy  `json:"accuracy,omitempty"`
	TimeoutMillis        int       `json:"timeoutMillis,omitempty"`
	MaxSamples           int       `json:"maxSamples,omitempty"`
	DurationMillis       int       `json:"durationMillis,omitempty"`
	DistanceFilterMeters float64   `json:"distanceFilterMeters,omitempty"`
	Reason               string    `json:"reason,omitempty"`
}

type Result struct {
	Permission      Permission `json:"permission"`
	ServicesEnabled bool       `json:"servicesEnabled"`
	Precise         bool       `json:"precise"`
	Fixes           []Fix      `json:"fixes,omitempty"`
}

// Fix uses -1 for unknown vertical accuracy, speed, or course. Altitude is
// meaningful only when vertical accuracy is nonnegative. Timestamps are Unix milliseconds.
type Fix struct {
	Latitude                 float64 `json:"latitude"`
	Longitude                float64 `json:"longitude"`
	HorizontalAccuracyMeters float64 `json:"horizontalAccuracyMeters"`
	AltitudeMeters           float64 `json:"altitudeMeters"`
	VerticalAccuracyMeters   float64 `json:"verticalAccuracyMeters"`
	SpeedMetersPerSecond     float64 `json:"speedMetersPerSecond"`
	CourseDegrees            float64 `json:"courseDegrees"`
	TimestampMillis          int64   `json:"timestampMillis"`
}

func (request Request) Validate() error {
	if !uitext.Plain(request.Reason, 200) || !finite(request.DistanceFilterMeters) || request.DistanceFilterMeters < 0 {
		return errors.New("location: invalid reason or distance filter")
	}
	switch request.Operation {
	case Status, Authorize:
		if request.Accuracy != "" || request.TimeoutMillis != 0 || request.MaxSamples != 0 || request.DurationMillis != 0 || request.DistanceFilterMeters != 0 || request.Operation == Status && request.Reason != "" {
			return errors.New("location: unexpected permission operation arguments")
		}
		return nil
	case Current, Track:
		if request.Accuracy != "" && request.Accuracy != Best && request.Accuracy != Balanced && request.Accuracy != Coarse {
			return errors.New("location: invalid accuracy")
		}
	default:
		return errors.New("location: unknown operation")
	}
	if request.Operation == Current {
		if request.TimeoutMillis < 0 || request.TimeoutMillis > 60000 || request.MaxSamples != 0 || request.DurationMillis != 0 || request.DistanceFilterMeters != 0 {
			return errors.New("location: invalid current position bounds")
		}
	} else if request.TimeoutMillis != 0 || request.MaxSamples < 1 || request.MaxSamples > 100 || request.DurationMillis < 1000 || request.DurationMillis > 120000 {
		return errors.New("location: invalid tracking bounds")
	}
	return nil
}

func ValidPermission(permission Permission) bool {
	switch permission {
	case NotDetermined, Denied, Restricted, WhenInUse, Always:
		return true
	}
	return false
}

func (request Request) Accept(result Result) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if !ValidPermission(result.Permission) {
		return errors.New("location: invalid permission")
	}
	switch request.Operation {
	case Status, Authorize:
		if len(result.Fixes) != 0 {
			return errors.New("location: permission result contains fixes")
		}
	case Current, Track:
		limit := request.MaxSamples
		if request.Operation == Current {
			limit = 1
		}
		if !result.ServicesEnabled || result.Permission != WhenInUse && result.Permission != Always || len(result.Fixes) < 1 || len(result.Fixes) > limit {
			return errors.New("location: invalid position result")
		}
	}
	for _, fix := range result.Fixes {
		for _, value := range []float64{fix.Latitude, fix.Longitude, fix.HorizontalAccuracyMeters, fix.AltitudeMeters, fix.VerticalAccuracyMeters, fix.SpeedMetersPerSecond, fix.CourseDegrees} {
			if !finite(value) {
				return errors.New("location: nonfinite fix")
			}
		}
		if fix.Latitude < -90 || fix.Latitude > 90 || fix.Longitude < -180 || fix.Longitude > 180 || fix.HorizontalAccuracyMeters < 0 || fix.VerticalAccuracyMeters < 0 && fix.VerticalAccuracyMeters != -1 || fix.SpeedMetersPerSecond < 0 && fix.SpeedMetersPerSecond != -1 || (fix.CourseDegrees < 0 && fix.CourseDegrees != -1) || fix.CourseDegrees >= 360 || fix.TimestampMillis < 0 {
			return errors.New("location: invalid fix")
		}
	}
	return nil
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
