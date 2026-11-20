// Package contacts validates a single user-selected contact detail.
package contacts

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr-native/device/files"
)

type Kind string

const (
	Phone    Kind = "phone"
	Email    Kind = "email"
	MaxBytes      = 4096
)

type Request struct {
	Title string `json:"title"`
	Kind  Kind   `json:"kind"`
}

type Contact struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
	Email string `json:"email"`
}

func (request Request) Validate() error {
	if len(request.Title) < 1 || len(request.Title) > 200 || (request.Kind != Phone && request.Kind != Email) {
		return errors.New("contacts: invalid title or kind")
	}
	return nil
}

func (request Request) Accept(document files.Document) error {
	_, err := request.Contact(document)
	return err
}

func (request Request) Contact(document files.Document) (Contact, error) {
	if err := request.Validate(); err != nil {
		return Contact{}, err
	}
	contact, err := Parse(document)
	if err != nil {
		return Contact{}, err
	}
	if (request.Kind == Phone && (strings.TrimSpace(contact.Phone) == "" || contact.Email != "")) ||
		(request.Kind == Email && (strings.TrimSpace(contact.Email) == "" || contact.Phone != "")) {
		return Contact{}, errors.New("contacts: selected detail does not match the request")
	}
	return contact, nil
}

// Parse reads a bounded contact document without granting access to other rows.
func Parse(document files.Document) (Contact, error) {
	var contact Contact
	if err := document.Validate(); err != nil {
		return contact, err
	}
	if document.Name != "Contact.json" || document.MediaType != "application/json" || len(document.Data) > MaxBytes || !utf8.Valid(document.Data) {
		return contact, errors.New("contacts: a bounded Contact.json document is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(document.Data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&contact); err != nil {
		return Contact{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Contact{}, errors.New("contacts: trailing JSON")
	}
	for _, field := range []struct {
		value   string
		maximum int
	}{{contact.Name, 512}, {contact.Phone, 256}, {contact.Email, 320}} {
		if len(field.value) > field.maximum || strings.ContainsFunc(field.value, unicode.IsControl) {
			return Contact{}, errors.New("contacts: invalid contact text")
		}
	}
	if (strings.TrimSpace(contact.Phone) == "") == (strings.TrimSpace(contact.Email) == "") {
		return Contact{}, errors.New("contacts: exactly one contact detail is required")
	}
	return contact, nil
}
