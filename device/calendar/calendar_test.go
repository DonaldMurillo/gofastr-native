package calendar

import (
	"strings"
	"testing"
	"time"
)

func TestContract(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	event := Event{Title: "Lunch", Start: now, End: now.Add(time.Hour)}
	add := Request{Operation: Add, Event: &event}
	read := Request{Operation: Read, From: now, To: now.Add(24 * time.Hour)}
	for _, request := range []Request{{Operation: Status}, add, read} {
		if err := request.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	returned := event
	returned.Identifier = "event-1"
	for _, pair := range []struct {
		request Request
		result  Result
	}{
		{Request{Operation: Status}, Result{Authorization: "writeOnly"}},
		{add, Result{Identifier: "event-1"}},
		{read, Result{}},
		{read, Result{Events: []Event{returned}}},
		{read, Result{Events: []Event{returned, returned}}},
	} {
		if err := pair.request.Accept(pair.result); err != nil {
			t.Fatal(err)
		}
	}
	invalid := []Request{{}, {Operation: Add}, {Operation: Status, Event: &event}, {Operation: Read, From: now, To: now}, {Operation: Read, From: now, To: now.Add(367 * 24 * time.Hour)}, {Operation: Read, From: now, To: now.Add(time.Hour), Limit: 101}, {Operation: Read, From: now, To: now.Add(time.Hour), Limit: -1}, {Operation: Add, Event: &event, Limit: 1}}
	for _, request := range invalid {
		if request.Validate() == nil {
			t.Fatalf("accepted %#v", request)
		}
	}
	for _, change := range []func(*Event){
		func(e *Event) { e.Title = "" }, func(e *Event) { e.Title = strings.Repeat("x", 201) },
		func(e *Event) { e.Title = "a\nb" }, func(e *Event) { e.Location = strings.Repeat("x", 201) },
		func(e *Event) { e.Notes = strings.Repeat("x", 2001) }, func(e *Event) { e.End = now.Add(-time.Hour) },
		func(e *Event) { e.Identifier = "unexpected" }, func(e *Event) { e.Start = time.Time{} },
	} {
		bad := event
		change(&bad)
		if (Request{Operation: Add, Event: &bad}).Validate() == nil {
			t.Fatalf("accepted %#v", bad)
		}
	}
	for _, pair := range []struct {
		request Request
		result  Result
	}{
		{Request{Operation: Status}, Result{}}, {Request{Operation: Status}, Result{Authorization: "full", Events: []Event{}}},
		{add, Result{}}, {add, Result{Identifier: "id", Authorization: "full"}},
		{read, Result{Authorization: "full"}}, {read, Result{Events: []Event{event}}},
		{Request{Operation: Read, From: now, To: now.Add(time.Hour), Limit: 1}, Result{Events: []Event{returned, returned}}},
		{Request{Operation: Read, From: now.Add(2 * time.Hour), To: now.Add(3 * time.Hour)}, Result{Events: []Event{returned}}},
	} {
		if pair.request.Accept(pair.result) == nil {
			t.Fatalf("accepted %#v", pair.result)
		}
	}
}
