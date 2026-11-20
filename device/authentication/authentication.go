// Package authentication describes interactive native sign-in operations.
package authentication

import (
	"errors"
	"net/url"
	"strings"
	"unicode"

	"github.com/DonaldMurillo/gofastr-native/internal/uitext"
)

type Operation string

const (
	Apple Operation = "apple"
	OAuth Operation = "oauth"
)

// Request starts one sign-in attempt. For Apple, Nonce is the value sent
// to Apple (for example, a server-provided SHA-256 nonce). For OAuth, the
// caller constructs the authorization URL with PKCE and a fresh State.
type Request struct {
	Operation              Operation `json:"operation"`
	Nonce                  string    `json:"nonce,omitempty"`
	AuthorizationURL       string    `json:"authorizationURL,omitempty"`
	RedirectURI            string    `json:"redirectURI,omitempty"`
	State                  string    `json:"state,omitempty"`
	PreferEphemeralSession bool      `json:"preferEphemeralSession,omitempty"`
}

// Result contains sensitive authorization material, not a verified login.
// Never display or log it. Verify the Apple token or exchange the OAuth
// code with the original PKCE verifier before signing the user in.
type Result struct {
	AuthorizationCode string `json:"authorizationCode"`
	IdentityToken     string `json:"identityToken,omitempty"`
	User              string `json:"user,omitempty"`
	State             string `json:"state,omitempty"`
}

func (r Request) Validate() error {
	switch r.Operation {
	case Apple:
		if token(r.Nonce, 512) && r.AuthorizationURL == "" && r.RedirectURI == "" && r.State == "" && !r.PreferEphemeralSession {
			return nil
		}
	case OAuth:
		auth, redirect := parseURL(r.AuthorizationURL), parseURL(r.RedirectURI)
		if r.Nonce == "" && token(r.State, 512) && auth != nil && auth.Scheme == "https" && auth.Hostname() != "" &&
			redirect != nil && redirect.Scheme != "https" && redirect.Scheme != "http" &&
			redirect.Scheme != "file" && redirect.Port() == "" && redirect.RawQuery == "" && !redirect.ForceQuery &&
			strings.HasPrefix(redirect.Path, "/") {
			return nil
		}
	}
	return errors.New("authentication: invalid sign-in request")
}

func (r Request) Accept(result *Result) error {
	if result != nil && token(result.AuthorizationCode, 8192) {
		switch r.Operation {
		case Apple:
			if token(result.IdentityToken, 16384) && token(result.User, 512) && result.State == "" {
				return nil
			}
		case OAuth:
			if r.State != "" && result.State == r.State && result.IdentityToken == "" && result.User == "" {
				return nil
			}
		}
	}
	return errors.New("authentication: invalid sign-in result")
}

func token(value string, limit int) bool {
	return value != "" && uitext.Plain(value, limit) && strings.IndexFunc(value, unicode.IsSpace) < 0
}

func parseURL(raw string) *url.URL {
	if !token(raw, 8192) || strings.ContainsAny(raw, "#\\") {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Opaque != "" || u.User != nil {
		return nil
	}
	return u
}
