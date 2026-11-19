package capture

import (
	"math"
	"time"
)

// Clock values are nanoseconds on Android's elapsedRealtimeNanos clock, the
// clock camera frames, sensor events and location fixes share. Info.WallTime
// converts them.

// Location is one position fix.
type Location struct {
	Clock int64 `json:"clock"`
	// Time is the provider's UTC fix time.
	Time      time.Time `json:"time"`
	Provider  string    `json:"provider"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	// Accuracy is the horizontal 68% radius in metres.
	Accuracy float64 `json:"accuracy"`
	// Altitude is metres above the WGS84 ellipsoid.
	Altitude         *float64 `json:"altitude,omitempty"`
	VerticalAccuracy *float64 `json:"verticalAccuracy,omitempty"`
	// Speed is metres per second over ground.
	Speed         *float64 `json:"speed,omitempty"`
	SpeedAccuracy *float64 `json:"speedAccuracy,omitempty"`
	// Bearing is the direction of travel in degrees from true north. It is
	// not where the camera points; see Orientation.
	Bearing         *float64 `json:"bearing,omitempty"`
	BearingAccuracy *float64 `json:"bearingAccuracy,omitempty"`
}

// Orientation is where the camera's optical axis points, from the fused
// rotation-vector sensor (gyroscope, accelerometer and magnetometer).
type Orientation struct {
	Clock int64 `json:"clock"`
	// Azimuth is degrees clockwise from magnetic north, 0 to 360.
	Azimuth float64 `json:"azimuth"`
	// Heading is degrees clockwise from true north. It needs a location fix
	// for the magnetic declination and is nil until one arrives.
	Heading *float64 `json:"heading,omitempty"`
	// Pitch is degrees above the horizon, -90 (down) to 90 (up).
	Pitch float64 `json:"pitch"`
	// Roll is the device's rotation around the optical axis in degrees: 0
	// upright portrait, 90 when the device's right edge points up.
	Roll float64 `json:"roll"`
	// Accuracy is the sensor's heading accuracy estimate in degrees, when
	// the device reports one.
	Accuracy *float64 `json:"accuracy,omitempty"`
	// Quaternion is the device-to-world rotation (x, y, z, w) in an
	// east-north-up frame, for consumers that need the full attitude.
	Quaternion [4]float64 `json:"quaternion"`
}

// Rotation is raw gyroscope angular velocity in radians per second around
// the device's x (right), y (up) and z (out of the screen) axes.
type Rotation struct {
	Clock int64   `json:"clock"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Z     float64 `json:"z"`
}

// Acceleration is linear acceleration with gravity removed, in metres per
// second squared along the device's x, y and z axes.
type Acceleration struct {
	Clock int64   `json:"clock"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Z     float64 `json:"z"`
}

// standardGravity is one g in m/s².
const standardGravity = 9.80665

// G is the acceleration's magnitude in g.
func (a Acceleration) G() float64 { return math.Sqrt(a.X*a.X+a.Y*a.Y+a.Z*a.Z) / standardGravity }

// Telemetry carries exactly one sample.
type Telemetry struct {
	Location     *Location     `json:"location,omitempty"`
	Orientation  *Orientation  `json:"orientation,omitempty"`
	Rotation     *Rotation     `json:"rotation,omitempty"`
	Acceleration *Acceleration `json:"acceleration,omitempty"`
}

// Track identifies an encoded stream.
type Track int

const (
	Video Track = 0
	Audio Track = 1
)

// Media is one encoded access unit.
type Media struct {
	Track Track
	// Codec is "h264" (Annex B, parameter sets before every keyframe) or
	// "opus".
	Codec string
	// Clock is the presentation time on the device clock.
	Clock int64
	Key   bool
	Data  []byte
}
