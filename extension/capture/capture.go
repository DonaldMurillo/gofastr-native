// Package capture records and streams camera video with GPS position and
// device orientation. With the native renderer, Android runs the camera,
// encoders and sensors in a foreground service. With the WebView renderer,
// the page runs them through getUserMedia and WebCodecs and reports to
// WebViewHandler, and capture stops with the page. Go owns sessions, the
// telemetry sidecar, recordings and network sinks such as WHIP.
//
// An app starts a session from an action, shows Block on a screen and reads
// status through Watch:
//
//	session, err := capture.Start(capture.Config{Record: true, Stream: true, Location: true, Motion: true})
//	block, err := capture.Block("camera", capture.View{Session: session, Preview: true})
//
// The device session starts when a screen first shows a Block carrying it.
// On Android's native renderer it keeps running in the background until Stop,
// the notification's Stop action, or an error ends it.
package capture

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/DonaldMurillo/gofastr-native/behavior"
	"github.com/DonaldMurillo/gofastr-native/extension"
	"github.com/DonaldMurillo/gofastr-native/extension/webassets"
)

// Name is the component and capability name.
const Name = "gofastr.capture/v1"

//go:embed gofastr-native.extension.json *.java capture.js
var assets embed.FS

func init() { webassets.Register(Definition()) }

// Definition registers the Android and WebView adapters. iOS reports the
// capability as missing.
func Definition() extension.Definition {
	content, _ := assets.ReadFile(extension.ManifestName)
	manifest, _ := extension.ParseManifest(content)
	return extension.Definition{Manifest: manifest, Assets: assets, ValidateComponent: validate}
}

// Facing selects a camera.
type Facing string

const (
	Back  Facing = "back"
	Front Facing = "front"
)

// Config is what the native session captures. Zero values take defaults:
// back camera, 1280x720, 30 fps, 4 Mbit/s, motion at 30 Hz, one segment.
type Config struct {
	Camera    Facing `json:"camera"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	FrameRate int    `json:"frameRate"`
	Bitrate   int    `json:"bitrate"`
	// Audio records the microphone. The app must be built with
	// --microphone-usage; otherwise the session runs without audio.
	Audio bool `json:"audio,omitempty"`
	// Location reports GPS and network fixes about once a second.
	Location bool `json:"location,omitempty"`
	// Motion reports camera azimuth, pitch and roll from the fused
	// rotation-vector sensor at MotionRate.
	Motion bool `json:"motion,omitempty"`
	// Gyroscope also reports raw angular velocity at MotionRate.
	Gyroscope bool `json:"gyroscope,omitempty"`
	// Acceleration reports linear acceleration, gravity removed, at
	// MotionRate.
	Acceleration bool `json:"acceleration,omitempty"`
	MotionRate   int  `json:"motionRate,omitempty"`
	// Impact locks the recording around any acceleration of at least this
	// many g (9.80665 m/s²), as Session.Lock does. Zero turns it off; any
	// other value turns on Acceleration.
	Impact float64 `json:"impact,omitempty"`
	// Record writes MP4 segments and telemetry sidecars under the capture
	// root.
	Record bool `json:"record,omitempty"`
	// Segment starts a new MP4 file this often, at the next keyframe. A
	// process that dies loses only the segment it was writing. Zero records
	// the whole session as one segment.
	Segment time.Duration `json:"segment,omitempty"`
	// Budget turns on loop recording. Once the unlocked segments of every
	// recording under the capture root pass this many bytes, the oldest are
	// deleted. Locked segments are kept and do not count. Zero keeps
	// everything.
	Budget int64 `json:"budget,omitempty"`
	// Stream sends encoded H.264 (and Opus audio on Android 10+) to the
	// session's sinks, such as a WHIP publisher.
	Stream bool `json:"stream,omitempty"`
}

func (config Config) withDefaults() Config {
	if config.Camera == "" {
		config.Camera = Back
	}
	if config.Width == 0 && config.Height == 0 {
		config.Width, config.Height = 1280, 720
	}
	if config.FrameRate == 0 {
		config.FrameRate = 30
	}
	if config.Bitrate == 0 {
		config.Bitrate = 4_000_000
	}
	if config.Impact > 0 {
		config.Acceleration = true
	}
	if config.sensing() && config.MotionRate == 0 {
		config.MotionRate = 30
	}
	return config
}

// Validate reports whether Start would accept config. Zero values count as
// their defaults.
func (config Config) Validate() error {
	config = config.withDefaults()
	switch {
	case config.Camera != Back && config.Camera != Front:
		return errors.New("capture: camera must be back or front")
	case config.Width < 160 || config.Width > 3840 || config.Height < 120 || config.Height > 2160:
		return errors.New("capture: size must be between 160x120 and 3840x2160")
	case config.FrameRate < 1 || config.FrameRate > 60:
		return errors.New("capture: frame rate must be 1 to 60")
	case config.Bitrate < 100_000 || config.Bitrate > 50_000_000:
		return errors.New("capture: bitrate must be 100 kbit/s to 50 Mbit/s")
	case config.MotionRate < 0 || config.MotionRate > 200 || config.sensing() && config.MotionRate == 0:
		return errors.New("capture: motion rate must be 1 to 200 Hz")
	case config.Impact < 0 || config.Impact > 0 && (config.Impact < 0.5 || config.Impact > 20):
		return errors.New("capture: impact must be 0.5 to 20 g")
	case config.Segment != 0 && (config.Segment < 5*time.Second || config.Segment > time.Hour):
		return errors.New("capture: segments must be 5 seconds to an hour long")
	case config.Budget < 0:
		return errors.New("capture: budget must not be negative")
	case !config.Record && !config.Stream:
		return errors.New("capture: enable Record, Stream or both")
	}
	return nil
}

// sensing reports whether a motion sensor runs at MotionRate.
func (config Config) sensing() bool { return config.Motion || config.Gyroscope || config.Acceleration }

// View is what one screen shows. Preview alone opens the camera without
// recording; Session attaches the preview to a session and starts it.
type View struct {
	Session *Session
	Preview bool
	// Camera picks the preview camera when no session is attached.
	Camera Facing
}

type wireView struct {
	Preview bool         `json:"preview,omitempty"`
	Camera  Facing       `json:"camera,omitempty"`
	Session *wireSession `json:"session,omitempty"`
}

type wireSession struct {
	ID     string `json:"id"`
	Config Config `json:"config"`
}

// Block freezes a view into a keyed rendering component.
func Block(id string, view View) (extension.Component, error) {
	wire := wireView{Preview: view.Preview, Camera: view.Camera}
	if view.Session != nil {
		if view.Session.State().Active() {
			wire.Session = &wireSession{ID: view.Session.ID(), Config: view.Session.Config()}
		}
		wire.Camera = view.Session.Config().Camera
	}
	if wire.Camera == "" {
		wire.Camera = Back
	}
	payload, err := json.Marshal(wire)
	if err != nil {
		return extension.Component{}, err
	}
	component := extension.Component{ID: id, Name: Name, Payload: string(payload)}
	if err := validate(component); err != nil {
		return extension.Component{}, err
	}
	return component, nil
}

func validate(component extension.Component) error {
	if component.Name != Name || !behavior.SafeToken(component.ID) || len(component.Destinations) != 0 {
		return errors.New("capture: invalid component identity")
	}
	var view wireView
	if err := extension.DecodeComponent(component.Payload, &view); err != nil {
		return err
	}
	if view.Camera != Back && view.Camera != Front {
		return errors.New("capture: camera must be back or front")
	}
	if view.Session != nil {
		if !validID(view.Session.ID) {
			return errors.New("capture: invalid session id")
		}
		if err := view.Session.Config.Validate(); err != nil {
			return fmt.Errorf("capture: %w", err)
		}
	}
	return nil
}
