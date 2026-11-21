package nativeui

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/DonaldMurillo/gofastr-native/behavior"
	"github.com/DonaldMurillo/gofastr-native/device/audio"
	"github.com/DonaldMurillo/gofastr-native/device/authentication"
	"github.com/DonaldMurillo/gofastr-native/device/browser"
	"github.com/DonaldMurillo/gofastr-native/device/calendar"
	"github.com/DonaldMurillo/gofastr-native/device/contacts"
	"github.com/DonaldMurillo/gofastr-native/device/deviceinfo"
	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/device/iap"
	"github.com/DonaldMurillo/gofastr-native/device/location"
	"github.com/DonaldMurillo/gofastr-native/device/notifications"
	"github.com/DonaldMurillo/gofastr-native/device/push"
	"github.com/DonaldMurillo/gofastr-native/device/refresh"
	"github.com/DonaldMurillo/gofastr-native/device/scanner"
	"github.com/DonaldMurillo/gofastr-native/device/securestore"
	"github.com/DonaldMurillo/gofastr-native/device/sharein"
	"github.com/DonaldMurillo/gofastr-native/device/sharing"
	"github.com/DonaldMurillo/gofastr-native/device/widgets"
	"github.com/DonaldMurillo/gofastr-native/internal/uitext"
)

// ServiceRequest is one immutable, explicitly initiated device operation.
type ServiceRequest struct {
	DeviceInfo     *deviceinfo.Request     `json:"deviceInfo,omitempty"`
	Push           *push.Request           `json:"push,omitempty"`
	Browser        *browser.Request        `json:"browser,omitempty"`
	Contacts       *contacts.Request       `json:"contacts,omitempty"`
	ID             string                  `json:"id"`
	Notification   *notifications.Request  `json:"notification,omitempty"`
	Share          *sharing.Request        `json:"share,omitempty"`
	Clipboard      *sharing.Clipboard      `json:"clipboard,omitempty"`
	Audio          *audio.Request          `json:"audio,omitempty"`
	IncomingShare  *sharein.Request        `json:"incomingShare,omitempty"`
	Calendar       *calendar.Request       `json:"calendar,omitempty"`
	SecureStorage  *securestore.Request    `json:"secureStorage,omitempty"`
	Location       *location.Request       `json:"location,omitempty"`
	Widget         *widgets.Request        `json:"widget,omitempty"`
	Scan           *scanner.Request        `json:"scan,omitempty"`
	Authentication *authentication.Request `json:"authentication,omitempty"`
	Refresh        *refresh.Request        `json:"refresh,omitempty"`
	IAP            *iap.Request            `json:"iap,omitempty"`
	Commit         bool                    `json:"commit,omitempty"`
}

// ServiceResult is an observed OS outcome, not permission to repeat an operation.
type ServiceResult struct {
	DeviceInfo     *deviceinfo.Result     `json:"deviceInfo,omitempty"`
	Push           *push.State            `json:"push,omitempty"`
	Contacts       *contacts.Selection    `json:"contacts,omitempty"`
	Status         EffectStatus           `json:"status"`
	Message        string                 `json:"message,omitempty"`
	Permission     string                 `json:"permission,omitempty"`
	Identifier     string                 `json:"identifier,omitempty"`
	File           *files.Document        `json:"file,omitempty"`
	DurationMillis int                    `json:"durationMillis,omitempty"`
	IncomingShare  *sharein.Item          `json:"incomingShare,omitempty"`
	Calendar       *calendar.Result       `json:"calendar,omitempty"`
	SecureStorage  *securestore.Result    `json:"secureStorage,omitempty"`
	Location       *location.Result       `json:"location,omitempty"`
	Widget         *widgets.State         `json:"widget,omitempty"`
	Scan           *scanner.Result        `json:"scan,omitempty"`
	Authentication *authentication.Result `json:"authentication,omitempty"`
	Refresh        *refresh.State         `json:"refresh,omitempty"`
	IAP            *iap.Result            `json:"iap,omitempty"`
}

// ServiceRespond receives every validated outcome, including cancellation.
// Only completed results may carry device content. Incoming shares remain in
// their inbox until a separate explicit acknowledgement.
type ServiceRespond func(context.Context, Request, ServiceResult) (Result, error)

// Validate checks the operation without invoking application or platform code.
func (request ServiceRequest) Validate() error {
	operations := 0
	for _, present := range []bool{request.DeviceInfo != nil, request.Push != nil, request.Browser != nil, request.Contacts != nil, request.Notification != nil, request.Share != nil, request.Clipboard != nil, request.Audio != nil, request.IncomingShare != nil, request.SecureStorage != nil, request.Widget != nil, request.Location != nil, request.Scan != nil, request.Authentication != nil, request.Calendar != nil, request.Refresh != nil, request.IAP != nil} {
		if present {
			operations++
		}
	}
	if !behavior.SafeToken(request.ID) || operations != 1 {
		return errors.New("nativeui: exactly one named device service is required")
	}
	switch {
	case request.DeviceInfo != nil:
		return request.DeviceInfo.Validate()
	case request.Browser != nil:
		return request.Browser.Validate()
	case request.Contacts != nil:
		return request.Contacts.Validate()
	case request.Scan != nil:
		return request.Scan.Validate()
	case request.Push != nil:
		return request.Push.Validate()
	case request.Notification != nil:
		return request.Notification.Validate()
	case request.Share != nil:
		return request.Share.Validate()
	case request.Clipboard != nil:
		return request.Clipboard.Validate()
	case request.IncomingShare != nil:
		return request.IncomingShare.Validate()
	case request.SecureStorage != nil:
		return request.SecureStorage.Validate()
	case request.Calendar != nil:
		return request.Calendar.Validate()
	case request.Widget != nil:
		return request.Widget.Validate()
	case request.Location != nil:
		return request.Location.Validate()
	case request.Authentication != nil:
		return request.Authentication.Validate()
	case request.Refresh != nil:
		return request.Refresh.Validate()
	case request.IAP != nil:
		return request.IAP.Validate()
	default:
		return request.Audio.Validate()
	}
}

func (request ServiceRequest) capability() Capability {
	switch {
	case request.DeviceInfo != nil:
		return CapabilityDeviceInfo
	case request.Browser != nil:
		return CapabilityBrowser
	case request.Contacts != nil:
		return CapabilityContacts
	case request.Scan != nil:
		return CapabilityScanner
	case request.Push != nil:
		return CapabilityPush
	case request.Notification != nil:
		return CapabilityNotifications
	case request.Share != nil:
		return CapabilitySharing
	case request.Clipboard != nil:
		return CapabilityClipboard
	case request.IncomingShare != nil:
		return CapabilityIncomingShares
	case request.SecureStorage != nil:
		return CapabilitySecureStorage
	case request.Calendar != nil:
		return CapabilityCalendar
	case request.Widget != nil:
		return CapabilityWidgets
	case request.Location != nil:
		return CapabilityLocation
	case request.Authentication != nil:
		if request.Authentication.Operation == authentication.Apple {
			return CapabilitySignInWithApple
		}
		return CapabilityOAuthWebLogin
	case request.Refresh != nil:
		return CapabilityBackgroundRefresh
	case request.IAP != nil:
		return CapabilityIAP
	default:
		return CapabilityAudio
	}
}

func (request ServiceRequest) accept(outcome ServiceResult) error {
	if outcome.DeviceInfo != nil && request.DeviceInfo == nil {
		return errors.New("nativeui: service result contains unrelated device content")
	}
	if outcome.DeviceInfo != nil && outcome.Status != EffectCompleted {
		return errors.New("nativeui: unsuccessful service cannot return device content")
	}
	if outcome.Location != nil && (request.Location == nil || outcome.Status != EffectCompleted) {
		return errors.New("nativeui: unexpected location content")
	}
	if request.Location != nil && outcome.Permission != "" && !location.ValidPermission(location.Permission(outcome.Permission)) {
		return errors.New("nativeui: invalid location permission")
	}
	if outcome.Scan != nil && (request.Scan == nil || outcome.Status != EffectCompleted) {
		return errors.New("nativeui: unexpected scanner content")
	}
	if outcome.Push != nil && request.Push == nil {
		return errors.New("nativeui: service result contains unrelated device content")
	}
	if outcome.Push != nil && outcome.Status != EffectCompleted {
		return errors.New("nativeui: unsuccessful service cannot return device content")
	}
	if (outcome.Calendar != nil && request.Calendar == nil) || (outcome.Contacts != nil && request.Contacts == nil) || (outcome.IncomingShare != nil && request.IncomingShare == nil) || (outcome.SecureStorage != nil && request.SecureStorage == nil) || (outcome.Widget != nil && request.Widget == nil) || (outcome.Authentication != nil && request.Authentication == nil) || (outcome.Refresh != nil && request.Refresh == nil) || (outcome.IAP != nil && request.IAP == nil) {
		return errors.New("nativeui: service result contains unrelated device content")
	}
	if outcome.Status != EffectCompleted && (outcome.Contacts != nil || outcome.IncomingShare != nil || outcome.SecureStorage != nil || outcome.Widget != nil || outcome.Authentication != nil || outcome.Calendar != nil || outcome.Refresh != nil || outcome.IAP != nil) {
		return errors.New("nativeui: unsuccessful service cannot return device content")
	}
	for _, text := range []string{outcome.Message, outcome.Permission, outcome.Identifier} {
		if !uitext.Plain(text, 512) {
			return errors.New("nativeui: invalid service outcome text")
		}
	}
	if outcome.Identifier != "" && (request.Notification == nil || outcome.Identifier != request.Notification.Identifier) {
		return errors.New("nativeui: service result has a different reminder identifier")
	}
	switch outcome.Status {
	case EffectCompleted:
		if request.DeviceInfo != nil {
			if outcome.DeviceInfo == nil {
				return errors.New("nativeui: device info result is required")
			}
			if err := request.DeviceInfo.Accept(*outcome.DeviceInfo); err != nil {
				return err
			}
		}
		if request.Contacts != nil {
			if err := request.Contacts.Accept(outcome.Contacts); err != nil {
				return err
			}
		}
		if request.Location != nil {
			if outcome.Location == nil {
				return errors.New("nativeui: location result is required")
			}
			if err := request.Location.Accept(*outcome.Location); err != nil {
				return err
			}
		}
		if request.Scan != nil {
			if outcome.Scan == nil {
				return errors.New("nativeui: scanner result is required")
			}
			if err := request.Scan.Accept(*outcome.Scan); err != nil {
				return err
			}
		}
		if request.Calendar != nil {
			if outcome.Calendar == nil {
				return errors.New("nativeui: calendar result required")
			}
			if err := request.Calendar.Accept(*outcome.Calendar); err != nil {
				return err
			}
		}
		if request.Push != nil {
			if outcome.Push == nil {
				return errors.New("nativeui: push result is required")
			}
			if err := request.Push.Accept(*outcome.Push); err != nil {
				return err
			}
		}
		if request.IncomingShare != nil {
			if err := request.IncomingShare.Accept(outcome.IncomingShare); err != nil {
				return err
			}
		}
		if request.SecureStorage != nil {
			if outcome.SecureStorage == nil {
				return errors.New("nativeui: secure storage result is required")
			}
			if err := request.SecureStorage.Accept(*outcome.SecureStorage); err != nil {
				return err
			}
		}
		if request.Widget != nil {
			if outcome.Widget == nil {
				return errors.New("nativeui: widget result is required")
			}
			if err := request.Widget.Accept(*outcome.Widget); err != nil {
				return err
			}
		}
		if request.Authentication != nil {
			if err := request.Authentication.Accept(outcome.Authentication); err != nil {
				return err
			}
		}
		if request.Refresh != nil {
			if outcome.Refresh == nil {
				return errors.New("nativeui: background refresh result is required")
			}
			if err := request.Refresh.Accept(*outcome.Refresh); err != nil {
				return err
			}
		}
		if request.IAP != nil {
			if outcome.IAP == nil {
				return errors.New("nativeui: in-app purchase result is required")
			}
			if err := request.IAP.Accept(*outcome.IAP); err != nil {
				return err
			}
		}
		if request.Audio != nil {
			if outcome.File == nil {
				return errors.New("nativeui: successful recording requires audio content")
			}
			return request.Audio.Accept(*outcome.File, outcome.DurationMillis)
		}
	case EffectCancelled, EffectDenied, EffectUnsupported, EffectFailed:
	default:
		return errors.New("nativeui: unknown service outcome")
	}
	if outcome.File != nil || outcome.DurationMillis != 0 {
		return errors.New("nativeui: only successful audio may return bytes or duration")
	}
	return nil
}

func cloneService(request *ServiceRequest) *ServiceRequest {
	if request == nil {
		return nil
	}
	encoded, _ := json.Marshal(request)
	var clone ServiceRequest
	_ = json.Unmarshal(encoded, &clone)
	return &clone
}
