package ios

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-native/device/calendar"
)

func TestCalendarSwiftContract(t *testing.T) {
	requireSwift(t)
	directory := t.TempDir()
	source := swiftSources(t, directory, Sources, "GofastrCalendar.swift")
	now := time.Date(2026, 1, 1, 12, 0, 0, 123456789, time.UTC)
	requests := []calendar.Request{{Operation: calendar.Status}, {Operation: calendar.Add, Event: &calendar.Event{Title: "Lunch", Start: now, End: now.Add(time.Hour)}}, {Operation: calendar.Read, From: now, To: now.Add(time.Hour)}, {Operation: calendar.Edit, Event: &calendar.Event{Title: "Lunch", Start: now, End: now.Add(time.Hour)}}}
	data, err := json.Marshal(requests)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "wire.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	compileRunChecks(t, directory, "calendar", calendarChecks, "calendar compile", "calendar contract", source, []string{path})
}

const calendarChecks = `
import Foundation
@main struct Checks {
 static func main() throws {
  let requests = try JSONDecoder().decode([MobileCalendarRequest].self, from: Data(contentsOf: URL(fileURLWithPath: CommandLine.arguments[1])))
  for request in requests { try request.validate() }
  precondition(requests[2].effectiveLimit == 20)
  precondition(MobileCalendarResult(authorization: "full").wire["authorization"] as? String == "full")
  precondition(MobileCalendarResult(events: []).wire["events"] != nil)
  try requests[0].accept(MobileCalendarResult(authorization: "full"))
  try requests[1].accept(MobileCalendarResult(identifier: "event-1"))
  try requests[2].accept(MobileCalendarResult(events: []))
  try requests[3].accept(MobileCalendarResult())
  do { try requests[3].accept(MobileCalendarResult(identifier: "event-1")); fatalError("Accepted editor identifier") } catch {}
  do { try requests[0].accept(MobileCalendarResult()); fatalError("Accepted missing authorization") } catch {}
  do { try requests[1].accept(MobileCalendarResult(identifier: "event-1", events: [])); fatalError("Accepted unrelated events") } catch {}
  for wire in [
   #"{"operation":"add"}"#,
   #"{"operation":"edit"}"#,
   #"{"operation":"read","from":"2026-01-01T00:00:00Z","to":"2026-01-01T00:00:00Z"}"#,
   #"{"operation":"status","limit":1}"#,
   #"{"operation":"unknown"}"#
  ] {
   let request = try JSONDecoder().decode(MobileCalendarRequest.self, from: Data(wire.utf8))
   do { try request.validate(); fatalError("Accepted invalid request") } catch {}
  }
  var bad = requests[1]
  bad.event?.title = String(repeating: "x", count: 201)
  do { try bad.validate(); fatalError("Accepted oversized title") } catch {}
 }
}
`
