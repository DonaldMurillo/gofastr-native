// Package contacts describes explicit, bounded contact picker selections.
package contacts

import (
	"errors"

	"github.com/DonaldMurillo/gofastr-native/internal/uitext"
)

type Field string

const (
	Phone        Field = "phone"
	Email        Field = "email"
	Postal       Field = "postal"
	Organization Field = "organization"
	MaxContacts        = 50
	MaxValues          = 20
	MaxText            = 512
	MaxPostal          = 2048
)

type Request struct {
	Multiple bool    `json:"multiple"`
	Fields   []Field `json:"fields,omitempty"`
}
type Labeled struct {
	Label string `json:"label"`
	Value string `json:"value"`
}
type Contact struct {
	ID           string    `json:"id"`
	GivenName    string    `json:"givenName"`
	FamilyName   string    `json:"familyName"`
	Organization string    `json:"organization,omitempty"`
	Phones       []Labeled `json:"phones,omitempty"`
	Emails       []Labeled `json:"emails,omitempty"`
	Postal       []string  `json:"postal,omitempty"`
}
type Selection struct {
	Contacts []Contact `json:"contacts"`
}

func (r Request) Validate() error {
	seen := map[Field]bool{}
	for _, f := range r.Fields {
		if seen[f] {
			return errors.New("contacts: duplicate field")
		}
		switch f {
		case Phone, Email, Postal, Organization:
		default:
			return errors.New("contacts: unknown field")
		}
		seen[f] = true
	}
	return nil
}

func (r Request) Accept(s *Selection) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if s == nil || len(s.Contacts) == 0 || len(s.Contacts) > MaxContacts || !r.Multiple && len(s.Contacts) != 1 {
		return errors.New("contacts: invalid selection count")
	}
	fields := map[Field]bool{}
	for _, f := range r.Fields {
		fields[f] = true
	}
	for _, c := range s.Contacts {
		if c.ID == "" || !uitext.Plain(c.ID, MaxText) || !uitext.Plain(c.GivenName, MaxText) || !uitext.Plain(c.FamilyName, MaxText) || !uitext.Plain(c.Organization, MaxText) {
			return errors.New("contacts: invalid contact text")
		}
		if !fields[Organization] && c.Organization != "" || !fields[Phone] && len(c.Phones) > 0 || !fields[Email] && len(c.Emails) > 0 || !fields[Postal] && len(c.Postal) > 0 {
			return errors.New("contacts: unrequested field")
		}
		for _, list := range [][]Labeled{c.Phones, c.Emails} {
			if len(list) > MaxValues {
				return errors.New("contacts: too many values")
			}
			for _, v := range list {
				if !uitext.Plain(v.Label, MaxText) || !uitext.Plain(v.Value, MaxText) {
					return errors.New("contacts: invalid labeled value")
				}
			}
		}
		if len(c.Postal) > MaxValues {
			return errors.New("contacts: too many addresses")
		}
		for _, v := range c.Postal {
			if !uitext.Multiline(v, MaxPostal) {
				return errors.New("contacts: invalid address")
			}
		}
	}
	return nil
}
