// Package push describes remote push notifications delivered through the
// platform push service (Firebase Cloud Messaging on Android).
//
// Register returns the device token the application's own server uses to
// address this installation. Tokens rotate: register again when the app
// starts and send the server any token that changed. Received messages stay
// in a private inbox until the application acknowledges them, like the
// incoming-share inbox, so an uncertain save can be repeated without loss.
package push

import (
	"errors"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr-native/internal/uitext"
)

const (
	Register    = "register"
	Unregister  = "unregister"
	Status      = "status"
	Next        = "next"
	Acknowledge = "acknowledge"

	// MaxDataBytes matches the FCM payload ceiling.
	MaxDataBytes = 4096
	MaxDataKeys  = 32
	MaxInbox     = 32
	MaxToken     = 4096
)

// Request is one explicit push operation. Only acknowledge names a message.
type Request struct {
	Operation  string `json:"operation"`
	Identifier string `json:"identifier,omitempty"`
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

// Result carries the token for register and status, and the oldest
// unacknowledged message for next. Both are empty when absent.
type Result struct {
	Token   string   `json:"token,omitempty"`
	Message *Message `json:"message,omitempty"`
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

// Accept checks a completed host result against the request that asked for it.
func (request Request) Accept(result Result) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if result.Token != "" && (request.Operation != Register && request.Operation != Status || !uitext.Token(result.Token, MaxToken)) {
		return errors.New("push: unexpected or invalid token")
	}
	if request.Operation == Register && result.Token == "" {
		return errors.New("push: registration returned no token")
	}
	if result.Message != nil {
		if request.Operation != Next {
			return errors.New("push: only next returns a message")
		}
		return result.Message.Validate()
	}
	return nil
}
