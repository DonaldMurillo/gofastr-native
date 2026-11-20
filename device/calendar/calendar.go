// Package calendar describes user-initiated calendar editor requests.
package calendar

import (
	"errors"
	"time"

	"github.com/DonaldMurillo/gofastr-native/internal/uitext"
)

const MaxSpanMillis int64 = 366 * 24 * 60 * 60 * 1000

// Event contains editor defaults, not permission to write a calendar directly.
type Event struct {
	Title    string `json:"title"`
	Location string `json:"location,omitempty"`
	Notes    string `json:"notes,omitempty"`
	Start    int64  `json:"startMillis"`
	End      int64  `json:"endMillis"`
	AllDay   bool   `json:"allDay"`
}

// New converts times to epoch milliseconds. Validate checks the resulting event.
func New(title string, start, end time.Time) Event {
	return Event{Title: title, Start: start.UnixMilli(), End: end.UnixMilli()}
}

func (event Event) Validate() error {
	if event.Title == "" || !uitext.Plain(event.Title, 200) || !uitext.Plain(event.Location, 200) || !uitext.NoNUL(event.Notes, 4<<10) {
		return errors.New("calendar: invalid title, location or notes")
	}
	if event.Start == 0 || event.Start == (time.Time{}).UnixMilli() || event.End <= event.Start || uint64(event.End)-uint64(event.Start) > uint64(MaxSpanMillis) {
		return errors.New("calendar: event requires a nonzero start and a positive span of at most 366 days")
	}
	if event.AllDay && (event.Start%86400000 != 0 || event.End%86400000 != 0) {
		return errors.New("calendar: all-day boundaries must be UTC midnight")
	}
	return nil
}
