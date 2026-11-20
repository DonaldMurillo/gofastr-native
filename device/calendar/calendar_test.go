package calendar

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestEventBounds(t *testing.T) {
	base := New("Event", time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2028, 1, 2, 0, 0, 0, 0, time.UTC))
	for name, change := range map[string]func(*Event){
		"empty title": func(e *Event) { e.Title = "" }, "long title": func(e *Event) { e.Title = strings.Repeat("a", 201) },
		"control title": func(e *Event) { e.Title = "a\n" }, "invalid title": func(e *Event) { e.Title = string([]byte{255}) },
		"long location": func(e *Event) { e.Location = strings.Repeat("a", 201) }, "control location": func(e *Event) { e.Location = "\t" },
		"invalid location": func(e *Event) { e.Location = string([]byte{255}) },
		"long notes":       func(e *Event) { e.Notes = strings.Repeat("a", 4097) }, "nul notes": func(e *Event) { e.Notes = "\x00" },
		"invalid notes": func(e *Event) { e.Notes = string([]byte{255}) },
		"zero start":    func(e *Event) { e.Start = 0 }, "zero time": func(e *Event) { e.Start = (time.Time{}).UnixMilli() },
		"equal": func(e *Event) { e.End = e.Start }, "reverse": func(e *Event) { e.End = e.Start - 1 },
		"long span":     func(e *Event) { e.End = e.Start + MaxSpanMillis + 1 },
		"overflow":      func(e *Event) { e.Start = math.MinInt64; e.End = math.MaxInt64 },
		"all-day start": func(e *Event) { e.AllDay = true; e.Start++ },
		"all-day end":   func(e *Event) { e.AllDay = true; e.End++ },
	} {
		t.Run(name, func(t *testing.T) {
			e := base
			change(&e)
			if e.Validate() == nil {
				t.Fatal("invalid event accepted")
			}
		})
	}
	base.Title = strings.Repeat("a", 200)
	base.Location = base.Title
	base.Notes = strings.Repeat("\n", 4096)
	base.AllDay = true
	base.End = base.Start + MaxSpanMillis
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err = json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["startMillis"] != float64(base.Start) || wire["endMillis"] != float64(base.End) {
		t.Fatal(string(data))
	}
	var round Event
	if err = json.Unmarshal(data, &round); err != nil || round != base {
		t.Fatal(round, err)
	}
}
