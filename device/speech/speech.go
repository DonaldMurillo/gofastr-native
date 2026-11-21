// Package speech describes foreground text-to-speech and single-utterance
// speech recognition using the platform's installed providers.
package speech

import (
	"errors"
	"strings"

	"github.com/DonaldMurillo/gofastr-native/internal/uitext"
	"golang.org/x/text/language"
)

type Operation string

const (
	Speak        Operation = "speak"
	Recognize    Operation = "recognize"
	MaxTextBytes           = 4000
)

// Request speaks Text or recognizes one utterance. Language is an optional
// BCP 47 language tag; an empty tag uses the platform provider's default.
type Request struct {
	Operation Operation `json:"operation"`
	Text      string    `json:"text,omitempty"`
	Language  string    `json:"language,omitempty"`
}

func (request Request) Validate() error {
	if request.Language != "" {
		if len(request.Language) > 64 || strings.ContainsAny(request.Language, "_ ") {
			return errors.New("speech: invalid language tag")
		}
		if _, err := language.Parse(request.Language); err != nil {
			return errors.New("speech: invalid language tag")
		}
	}
	switch request.Operation {
	case Speak:
		if !validText(request.Text) {
			return errors.New("speech: spoken text must be nonblank and at most 4000 bytes")
		}
	case Recognize:
		if request.Text != "" {
			return errors.New("speech: recognition does not accept input text")
		}
	default:
		return errors.New("speech: unknown operation")
	}
	return nil
}

// Accept checks the transcript on a completed outcome. Synthesis never returns text.
func (request Request) Accept(transcript string) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if request.Operation == Recognize && !validText(transcript) {
		return errors.New("speech: recognition requires a nonblank transcript of at most 4000 bytes")
	}
	if request.Operation == Speak && transcript != "" {
		return errors.New("speech: synthesis cannot return a transcript")
	}
	return nil
}

func validText(text string) bool {
	return uitext.Multiline(text, MaxTextBytes) && strings.TrimSpace(text) != ""
}
