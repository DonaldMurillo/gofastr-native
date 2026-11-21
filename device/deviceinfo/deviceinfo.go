// Package deviceinfo describes snapshot-only device and battery reads.
package deviceinfo

import (
	"errors"
	"regexp"

	"github.com/DonaldMurillo/gofastr-native/internal/uitext"
)

type Operation string

const (
	ReadInfo    Operation = "info"
	ReadBattery Operation = "battery"
)

type Request struct {
	Operation Operation `json:"operation"`
}

type Info struct {
	Platform     string `json:"platform"`
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	OSVersion    string `json:"osVersion"`
	APILevel     int    `json:"apiLevel"`
	Locale       string `json:"locale"`
	AppVersion   string `json:"appVersion"`
}

type State string

const (
	Charging     State = "charging"
	Discharging  State = "discharging"
	Full         State = "full"
	NotCharging  State = "not_charging"
	UnknownState State = "unknown"
)

type PowerSource string

const (
	AC            PowerSource = "ac"
	USB           PowerSource = "usb"
	Wireless      PowerSource = "wireless"
	None          PowerSource = "none"
	UnknownSource PowerSource = "unknown"
)

type Battery struct {
	LevelKnown  bool        `json:"levelKnown"`
	Level       int         `json:"level"`
	State       State       `json:"state"`
	PowerSource PowerSource `json:"powerSource"`
	PowerSave   bool        `json:"powerSave"`
}

type Result struct {
	Info    *Info    `json:"info,omitempty"`
	Battery *Battery `json:"battery,omitempty"`
}

var localePattern = regexp.MustCompile(`^[A-Za-z]{2,8}(-[A-Za-z0-9]{1,8})*$`)

func (request Request) Validate() error {
	if request.Operation != ReadInfo && request.Operation != ReadBattery {
		return errors.New("deviceinfo: unknown operation")
	}
	return nil
}

func (request Request) Accept(result Result) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if (request.Operation == ReadInfo) != (result.Info != nil) || (request.Operation == ReadBattery) != (result.Battery != nil) {
		return errors.New("deviceinfo: result must match the requested snapshot")
	}
	if info := result.Info; info != nil {
		for _, value := range []string{info.Platform, info.Manufacturer, info.Model, info.OSVersion, info.Locale, info.AppVersion} {
			if !uitext.Plain(value, 128) {
				return errors.New("deviceinfo: invalid info text")
			}
		}
		if (info.Platform != "android" && info.Platform != "ios") || info.APILevel < 0 || (info.Platform == "ios" && info.APILevel != 0) || !localePattern.MatchString(info.Locale) {
			return errors.New("deviceinfo: invalid platform, API level or locale")
		}
	}
	if battery := result.Battery; battery != nil {
		if battery.Level < 0 || battery.Level > 100 || (!battery.LevelKnown && battery.Level != 0) {
			return errors.New("deviceinfo: invalid battery level")
		}
		switch battery.State {
		case Charging, Discharging, Full, NotCharging, UnknownState:
		default:
			return errors.New("deviceinfo: invalid battery state")
		}
		switch battery.PowerSource {
		case AC, USB, Wireless, None, UnknownSource:
		default:
			return errors.New("deviceinfo: invalid power source")
		}
	}
	return nil
}
