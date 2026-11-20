package background

import (
	"strings"
	"testing"
)

func TestRequest(t *testing.T) {
	valid := []Request{{Operation: Schedule, Identifier: "job", Task: "refresh"}, {Operation: Schedule, Identifier: strings.Repeat("a", 128), Task: "refresh", Periodic: true, IntervalMinutes: 15, Network: "unmetered", RequiresCharging: true}, {Operation: Cancel, Identifier: "job"}, {Operation: Status, Identifier: "job"}}
	for _, r := range valid {
		if err := r.Validate(); err != nil {
			t.Fatal(r, err)
		}
	}
	invalid := []Request{{}, {Operation: Schedule, Identifier: "job"}, {Operation: Schedule, Identifier: "job", Task: "bad task"}, {Operation: Schedule, Identifier: strings.Repeat("a", 129), Task: "ok"}, {Operation: Schedule, Identifier: "job", Task: "ok", DelayMinutes: -1}, {Operation: Schedule, Identifier: "job", Task: "ok", DelayMinutes: MaxMinutes + 1}, {Operation: Schedule, Identifier: "job", Task: "ok", IntervalMinutes: 15}, {Operation: Schedule, Identifier: "job", Task: "ok", Periodic: true, IntervalMinutes: 14}, {Operation: Schedule, Identifier: "job", Task: "ok", Periodic: true, IntervalMinutes: 15, DelayMinutes: 1}, {Operation: Schedule, Identifier: "job", Task: "ok", Network: "wifi"}, {Operation: Cancel, Identifier: "job", Task: "ok"}, {Operation: Status, Identifier: "job", RequiresCharging: true}, {Operation: "other", Identifier: "job"}}
	for _, r := range invalid {
		if r.Validate() == nil {
			t.Errorf("accepted %+v", r)
		}
	}
	for _, r := range valid {
		for _, scheduled := range []bool{false, true} {
			err := r.Accept(State{Scheduled: scheduled})
			want := r.Operation == Status || r.Operation == Schedule && scheduled || r.Operation == Cancel && !scheduled
			if (err == nil) != want {
				t.Errorf("accept %+v %t: %v", r, scheduled, err)
			}
		}
	}
	if (Request{}).Accept(State{}) == nil {
		t.Fatal("accepted invalid request")
	}
}
