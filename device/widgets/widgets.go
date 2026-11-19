// Package widgets describes app-published home-screen summaries. The app owns
// their content; the OS displays the latest snapshot without starting Go.
package widgets

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"unicode"

	"github.com/DonaldMurillo/gofastr-native/internal/uitext"
)

const (
	Publish          = "publish"
	Clear            = "clear"
	Status           = "status"
	MaxItems         = 4
	MaxSnapshotBytes = 16 << 10
)

// Request publishes, removes, or inspects the app's single widget snapshot.
// Multiple installed widgets display the same snapshot.
type Request struct {
	Operation string    `json:"operation"`
	Snapshot  *Snapshot `json:"snapshot,omitempty"`
}

// Snapshot is a public home-screen summary. Never include secrets here.
// A tap opens Route through the app's usual navigation and authorization rules.
type Snapshot struct {
	Title string `json:"title"`
	Text  string `json:"text,omitempty"`
	Route string `json:"route"`
	Items []Item `json:"items,omitempty"`
}

// Item is summary text. Widget taps open the snapshot's route.
type Item struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
}

// State reports whether a snapshot is stored. It does not prove that a widget
// is installed or that the OS has displayed the latest snapshot.
type State struct {
	Published bool `json:"published"`
}

func (request Request) Validate() error {
	switch request.Operation {
	case Publish:
		if request.Snapshot == nil {
			return errors.New("widgets: publishing requires a snapshot")
		}
		return request.Snapshot.Validate()
	case Clear, Status:
		if request.Snapshot != nil {
			return errors.New("widgets: only publishing accepts a snapshot")
		}
		return nil
	default:
		return errors.New("widgets: unsupported operation")
	}
}

func (snapshot Snapshot) Validate() error {
	if !uitext.Plain(snapshot.Title, 200) || snapshot.Title == "" || !uitext.Multiline(snapshot.Text, 2048) || !localRoute(snapshot.Route) || len(snapshot.Items) > MaxItems {
		return errors.New("widgets: invalid snapshot text, route, or item count")
	}
	seen := map[string]bool{}
	for _, item := range snapshot.Items {
		if item.ID == "" || !uitext.Token(item.ID, 64) || seen[item.ID] || item.Title == "" || !uitext.Plain(item.Title, 200) || !uitext.Multiline(item.Subtitle, 512) {
			return errors.New("widgets: invalid or duplicate item")
		}
		seen[item.ID] = true
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil || len(encoded) > MaxSnapshotBytes {
		return errors.New("widgets: snapshot exceeds 16 KiB")
	}
	return nil
}

// Accept checks completed outcomes against the requested state change.
func (request Request) Accept(state State) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if request.Operation == Publish && !state.Published || request.Operation == Clear && state.Published {
		return errors.New("widgets: result does not match the requested change")
	}
	return nil
}

func localRoute(route string) bool {
	parsed, err := url.ParseRequestURI(route)
	if err != nil || !uitext.Plain(route, 2048) || strings.ContainsFunc(route, unicode.IsSpace) || !strings.HasPrefix(route, "/") || strings.HasPrefix(route, "//") || strings.ContainsAny(route, "#\\") || parsed.IsAbs() || parsed.Host != "" {
		return false
	}
	// A single decoded path must not introduce authority, backslashes, controls,
	// or traversal that changes the destination after native URL parsing.
	decoded, decodeErr := url.PathUnescape(route)
	if decodeErr != nil || strings.Contains(strings.ToLower(parsed.RawPath), "%2f") || strings.Contains(parsed.Path, "//") || strings.ContainsAny(parsed.Path, "\\?#") || !uitext.Plain(decoded, 2048) {
		return false
	}
	for _, part := range strings.Split(parsed.Path, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}
