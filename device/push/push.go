// Package push describes explicit remote-push registration and observed
// device state. iOS registers with APNs and Android with Firebase Cloud
// Messaging; State.Provider says which service issued the token.
//
// Tokens are ephemeral, not stable device identifiers, and they rotate:
// register when the app starts and send the application's own server any
// token that changed. On Android received messages also stay in a private
// inbox until the application acknowledges them, like the incoming-share
// inbox, so an uncertain save can be repeated without loss. iOS has no inbox;
// it opens a tapped notification's route directly.
package push

import (
	"errors"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr-native/internal/uitext"
)

const (
	Register   = "register"
	Unregister = "unregister"
	Status     = "status"
	// Next and Acknowledge read and settle the Android inbox. iOS reports
	// them unsupported.
	Next        = "next"
	Acknowledge = "acknowledge"

	// ProviderAPNs and ProviderFCM name the service that issued a token.
	ProviderAPNs = "apns"
	ProviderFCM  = "fcm"

	// MaxTokenLength bounds a hex APNs token.
	MaxTokenLength = 512
	// MaxFCMTokenLength bounds an FCM registration token.
	MaxFCMTokenLength = 4096

	// MaxDataBytes matches the FCM payload ceiling.
	MaxDataBytes = 4096
	MaxDataKeys  = 32
	MaxInbox     = 32
)

// Request is one explicit push operation. Only acknowledge names a message.
type Request struct {
	Operation  string `json:"operation"`
	Identifier string `json:"identifier,omitempty"`
}

// State is the observed registration. Token is present only while
// registered and names its Provider. Environment is the APNs environment
// from the iOS provisioning profile. Message is the oldest unacknowledged
// Android inbox entry, returned only by next.
type State struct {
	Permission  string   `json:"permission"`
	Registered  bool     `json:"registered"`
	Token       string   `json:"token,omitempty"`
	Provider    string   `json:"provider,omitempty"`
	Environment string   `json:"environment,omitempty"`
	Message     *Message `json:"message,omitempty"`
}

// Message is a copied push payload. Route, when present, is the local route a
// notification tap opens through the navigation guard.
type Message struct {
	Identifier string            `json:"identifier"`
	Title      string            `json:"title,omitempty"`
	Body       string            `json:"body,omitempty"`
	Route      string            `json:"route,omitempty"`
	Data       map[string]string `json:"data,omitempty"`
}

func validIdentifier(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// ValidRoute accepts an application-local path such as /notes/1.
func ValidRoute(route string) bool {
	parsed, err := url.ParseRequestURI(route)
	return err == nil && !parsed.IsAbs() && parsed.Host == "" && strings.HasPrefix(route, "/") && !strings.HasPrefix(route, "//") && !strings.ContainsAny(route, "#\\") && len(route) <= 4096
}

func (request Request) Validate() error {
	switch request.Operation {
	case Register, Unregister, Status, Next:
		if request.Identifier != "" {
			return errors.New("push: only acknowledge names a message")
		}
		return nil
	case Acknowledge:
		if !validIdentifier(request.Identifier) {
			return errors.New("push: acknowledge requires an inbox identifier")
		}
		return nil
	}
	return errors.New("push: unsupported operation")
}

func (message Message) Validate() error {
	if !validIdentifier(message.Identifier) {
		return errors.New("push: invalid inbox identifier")
	}
	if !uitext.Plain(message.Title, 200) || !uitext.NoNUL(message.Body, MaxDataBytes) {
		return errors.New("push: invalid message text")
	}
	if message.Route != "" && !ValidRoute(message.Route) {
		return errors.New("push: a message route must be a local path")
	}
	if len(message.Data) > MaxDataKeys {
		return errors.New("push: too many data keys")
	}
	total := 0
	for key, value := range message.Data {
		total += len(key) + len(value)
		if key == "" || !uitext.Plain(key, 128) || !uitext.NoNUL(value, MaxDataBytes) {
			return errors.New("push: invalid message data")
		}
	}
	if total > MaxDataBytes {
		return errors.New("push: message data exceeds the payload limit")
	}
	return nil
}

func validToken(provider, token string) bool {
	switch provider {
	case ProviderAPNs:
		return len(token) >= 2 && len(token) <= MaxTokenLength && len(token)%2 == 0 && strings.Trim(token, "0123456789abcdef") == ""
	case ProviderFCM:
		return uitext.Token(token, MaxFCMTokenLength)
	}
	return false
}

// Accept checks a completed host result against the request that asked for it.
func (request Request) Accept(state State) error {
	if err := request.Validate(); err != nil {
		return err
	}
	switch state.Permission {
	case "notDetermined", "denied", "authorized", "provisional", "ephemeral":
	default:
		return errors.New("push: invalid permission")
	}
	if state.Provider != "" && state.Provider != ProviderAPNs && state.Provider != ProviderFCM {
		return errors.New("push: invalid provider")
	}
	if state.Environment != "" && (state.Provider == ProviderFCM || state.Environment != "development" && state.Environment != "production") {
		return errors.New("push: invalid environment")
	}
	if request.Operation == Unregister && state.Registered || !state.Registered && state.Token != "" {
		return errors.New("push: unexpected registration or token")
	}
	if state.Token != "" && (request.Operation != Register && request.Operation != Status || !validToken(state.Provider, state.Token)) {
		return errors.New("push: unexpected or invalid token")
	}
	if state.Message != nil {
		if request.Operation != Next {
			return errors.New("push: only next returns a message")
		}
		return state.Message.Validate()
	}
	return nil
}
