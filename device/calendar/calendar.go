// Package calendar defines bounded calendar event operations.
package calendar

import (
	"errors"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr-native/internal/uitext"
)

type Operation string

const (
	Status       Operation = "status"
	Add          Operation = "add"
	Read         Operation = "read"
	DefaultLimit           = 20
	MaxLimit               = 100
)

type Event struct {
	Identifier string    `json:"identifier,omitempty"`
	Title      string    `json:"title"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	AllDay     bool      `json:"allDay"`
	Location   string    `json:"location,omitempty"`
	Notes      string    `json:"notes,omitempty"`
}

type Request struct {
	Operation Operation `json:"operation"`
	Event     *Event    `json:"event,omitempty"`
	From      time.Time `json:"from,omitzero"`
	To        time.Time `json:"to,omitzero"`
	Limit     int       `json:"limit,omitempty"`
}

type Result struct {
	Authorization string  `json:"authorization,omitempty"`
	Identifier    string  `json:"identifier,omitempty"`
	Events        []Event `json:"events,omitempty"`
}

func validID(id string) bool { return strings.TrimSpace(id) != "" && uitext.Plain(id, 512) }

func (event Event) validate(result bool) error {
	if !uitext.Plain(event.Title, 200) || strings.TrimSpace(event.Title) == "" || !uitext.Plain(event.Location, 200) || !uitext.Plain(event.Notes, 2000) || event.Start.IsZero() || event.End.IsZero() || event.End.Before(event.Start) || event.Start.Year() < 1 || event.End.Year() > 9999 || (result && !validID(event.Identifier)) || (!result && event.Identifier != "") {
		return errors.New("calendar: invalid event")
	}
	return nil
}

func (request Request) EffectiveLimit() int {
	if request.Limit == 0 {
		return DefaultLimit
	}
	return request.Limit
}

func (request Request) Validate() error {
	switch request.Operation {
	case Status, Add:
		if !request.From.IsZero() || !request.To.IsZero() || request.Limit != 0 {
			return errors.New("calendar: unexpected range")
		}
		if request.Operation == Status {
			if request.Event != nil {
				return errors.New("calendar: unexpected event")
			}
			return nil
		}
		if request.Event == nil {
			return errors.New("calendar: event required")
		}
		return request.Event.validate(false)
	case Read:
		if request.Event != nil || request.From.IsZero() || request.To.IsZero() || !request.To.After(request.From) || request.To.Sub(request.From) > 366*24*time.Hour || request.From.Year() < 1 || request.To.Year() > 9999 || request.Limit < 0 || request.Limit > MaxLimit {
			return errors.New("calendar: invalid range or limit")
		}
		return nil
	default:
		return errors.New("calendar: unknown operation")
	}
}

func (request Request) Accept(result Result) error {
	if err := request.Validate(); err != nil {
		return err
	}
	switch request.Operation {
	case Status:
		if result.Identifier != "" || result.Events != nil {
			break
		}
		switch result.Authorization {
		case "full", "writeOnly", "denied", "restricted", "notDetermined":
			return nil
		}
	case Add:
		if result.Authorization == "" && result.Events == nil && validID(result.Identifier) {
			return nil
		}
	case Read:
		if result.Authorization != "" || result.Identifier != "" || len(result.Events) > request.EffectiveLimit() {
			break
		}
		for _, event := range result.Events {
			if err := event.validate(true); err != nil {
				return err
			}
			if !event.Start.Before(request.To) || event.End.Before(request.From) {
				return errors.New("calendar: unrelated event")
			}
		}
		return nil
	}
	return errors.New("calendar: unexpected result fields")
}
