package deviceinfo

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRequestPolicy(t *testing.T) {
	for _, operation := range []Operation{ReadInfo, ReadBattery} {
		if err := (Request{Operation: operation}).Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, operation := range []Operation{"", "status", "subscribe"} {
		if (Request{Operation: operation}).Validate() == nil {
			t.Fatalf("accepted %q", operation)
		}
	}
}

func TestResultPolicy(t *testing.T) {
	info := Info{Platform: "android", Manufacturer: "Google", Model: "Pixel", OSVersion: "14", APILevel: 34, Locale: "en-US"}
	battery := Battery{State: UnknownState, PowerSource: UnknownSource}
	for _, test := range []struct {
		request Request
		result  Result
	}{
		{Request{ReadInfo}, Result{Info: &info}},
		{Request{ReadInfo}, Result{Info: &Info{Platform: "ios", Locale: "zh-Hant-TW"}}},
		{Request{ReadBattery}, Result{Battery: &battery}},
		{Request{ReadBattery}, Result{Battery: &Battery{LevelKnown: true, Level: 100, State: Full, PowerSource: AC}}},
	} {
		if err := test.request.Accept(test.result); err != nil {
			t.Fatal(err)
		}
	}
	for _, op := range []Operation{ReadInfo, ReadBattery, "invalid"} {
		for _, result := range []Result{{}, {Info: &info, Battery: &battery}} {
			if (Request{op}).Accept(result) == nil {
				t.Fatalf("accepted %s %+v", op, result)
			}
		}
	}
	if (Request{ReadInfo}).Accept(Result{Battery: &battery}) == nil || (Request{ReadBattery}).Accept(Result{Info: &info}) == nil {
		t.Fatal("accepted wrong snapshot")
	}
	for _, mutate := range []func(*Info){
		func(i *Info) { i.Platform = "web" }, func(i *Info) { i.APILevel = -1 }, func(i *Info) { i.Platform = "ios" },
		func(i *Info) { i.Model = "bad\nmodel" }, func(i *Info) { i.AppVersion = strings.Repeat("a", 129) },
		func(i *Info) { i.Locale = "" }, func(i *Info) { i.Locale = "en_US" }, func(i *Info) { i.Manufacturer = "\x00" },
	} {
		bad := info
		mutate(&bad)
		if (Request{ReadInfo}).Accept(Result{Info: &bad}) == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	for _, bad := range []Battery{
		{Level: -1, State: UnknownState, PowerSource: None}, {LevelKnown: true, Level: 101, State: Full, PowerSource: AC},
		{Level: 42, State: Charging, PowerSource: USB}, {State: "", PowerSource: None}, {State: Discharging, PowerSource: "solar"},
	} {
		if (Request{ReadBattery}).Accept(Result{Battery: &bad}) == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	for _, state := range []State{Charging, Discharging, Full, NotCharging, UnknownState} {
		for _, source := range []PowerSource{AC, USB, Wireless, None, UnknownSource} {
			if err := (Request{ReadBattery}).Accept(Result{Battery: &Battery{LevelKnown: true, State: state, PowerSource: source}}); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestWirePreservesZeroAndFalse(t *testing.T) {
	encoded, _ := json.Marshal(Result{Battery: &Battery{State: UnknownState, PowerSource: None}})
	if string(encoded) != `{"battery":{"levelKnown":false,"level":0,"state":"unknown","powerSource":"none","powerSave":false}}` {
		t.Fatal(string(encoded))
	}
	var decoded Result
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := (Request{ReadBattery}).Accept(decoded); err != nil {
		t.Fatal(err)
	}
	encoded, _ = json.Marshal(Info{Platform: "ios", Locale: "en"})
	if !strings.Contains(string(encoded), `"apiLevel":0`) || !strings.Contains(string(encoded), `"appVersion":""`) {
		t.Fatal(string(encoded))
	}
}
