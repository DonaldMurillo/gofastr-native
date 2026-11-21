// Package push describes explicit APNs registration and observed device state.
package push

import (
	"errors"
	"strings"
)

const (
	Register       = "register"
	Unregister     = "unregister"
	Status         = "status"
	MaxTokenLength = 512
)

type Request struct {
	Operation string `json:"operation"`
}

// State contains an ephemeral APNs token, not a stable device identifier.
type State struct {
	Permission  string `json:"permission"`
	Registered  bool   `json:"registered"`
	Token       string `json:"token,omitempty"`
	Environment string `json:"environment,omitempty"`
}

func (request Request) Validate() error {
	switch request.Operation {
	case Register, Unregister, Status:
		return nil
	default:
		return errors.New("push: unsupported operation")
	}
}

func (request Request) Accept(state State) error {
	if err := request.Validate(); err != nil {
		return err
	}
	switch state.Permission {
	case "notDetermined", "denied", "authorized", "provisional", "ephemeral":
	default:
		return errors.New("push: invalid permission")
	}
	if state.Environment != "" && state.Environment != "development" && state.Environment != "production" {
		return errors.New("push: invalid environment")
	}
	if request.Operation == Unregister && state.Registered || !state.Registered && state.Token != "" {
		return errors.New("push: unexpected registration or token")
	}
	if state.Token != "" && (len(state.Token) < 2 || len(state.Token) > MaxTokenLength || len(state.Token)%2 != 0 || strings.Trim(state.Token, "0123456789abcdef") != "") {
		return errors.New("push: invalid token")
	}
	return nil
}
