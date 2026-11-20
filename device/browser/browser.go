// Package browser describes a user-initiated in-app web browser.
package browser

import (
	"errors"
	"net/url"
	"strings"
	"unicode"

	"github.com/DonaldMurillo/gofastr-native/internal/uitext"
)

// Request opens an HTTP or HTTPS page until the user dismisses it.
type Request struct {
	URL          string `json:"url"`
	ReaderMode   bool   `json:"readerMode,omitempty"`
	DismissLabel string `json:"dismissLabel,omitempty"`
}

// Validate rejects credentials, non-web schemes and unbounded or non-plain text.
func (request Request) Validate() error {
	parsed, err := url.Parse(request.URL)
	if err != nil || !uitext.Plain(request.URL, 2048) || strings.ContainsFunc(request.URL, unicode.IsSpace) || parsed == nil || !parsed.IsAbs() || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return errors.New("browser: an absolute HTTP or HTTPS URL without credentials is required")
	}
	switch request.DismissLabel {
	case "", "done", "close", "cancel":
		return nil
	default:
		return errors.New("browser: invalid dismiss label")
	}
}
