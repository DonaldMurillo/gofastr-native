package push

import (
	"strings"
	"testing"
)

func TestRequestAndState(t *testing.T) {
	for _, operation := range []string{Register, Unregister, Status, Next} {
		request := Request{Operation: operation}
		if err := request.Accept(State{Permission: "notDetermined"}); err != nil {
			t.Fatal(err)
		}
	}
	if (Request{}).Validate() == nil {
		t.Fatal("empty operation accepted")
	}
	if (Request{Operation: "other"}).Validate() == nil {
		t.Fatal("unknown operation accepted")
	}
	request := Request{Operation: Register}
	for _, permission := range []string{"authorized", "denied", "provisional", "ephemeral"} {
		if err := request.Accept(State{Permission: permission, Registered: true, Token: "00abcdef", Provider: ProviderAPNs, Environment: "development"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, state := range []State{
		{}, {Permission: "unknown"}, {Permission: "authorized", Token: "abcd", Provider: ProviderAPNs},
		{Permission: "authorized", Registered: true, Token: "ABCDEF", Provider: ProviderAPNs},
		{Permission: "authorized", Registered: true, Token: "a", Provider: ProviderAPNs},
		{Permission: "authorized", Registered: true, Token: "zz", Provider: ProviderAPNs},
		{Permission: "authorized", Registered: true, Token: strings.Repeat("ab", 257), Provider: ProviderAPNs},
		{Permission: "authorized", Environment: "sandbox"},
		// A token must say which service issued it.
		{Permission: "authorized", Registered: true, Token: "aabb"},
		{Permission: "authorized", Registered: true, Token: "aabb", Provider: "gcm"},
		// FCM tokens are not hex; APNs environments do not apply to FCM.
		{Permission: "authorized", Registered: true, Token: "cZ1:APA91b-token_value", Provider: ProviderAPNs},
		{Permission: "authorized", Registered: true, Token: "bad token", Provider: ProviderFCM},
		{Permission: "authorized", Provider: ProviderFCM, Environment: "production"},
	} {
		if request.Accept(state) == nil {
			t.Errorf("accepted invalid state: %+v", state)
		}
	}
	if (Request{Operation: Unregister}).Accept(State{Permission: "authorized", Registered: true}) == nil {
		t.Fatal("unregister retained registration")
	}
	if err := (Request{Operation: Status}).Accept(State{Permission: "authorized", Registered: true, Token: strings.Repeat("ab", 256), Provider: ProviderAPNs, Environment: "production"}); err != nil {
		t.Fatal(err)
	}
	fcm := State{Permission: "authorized", Registered: true, Token: "cZ1:APA91b-token_value", Provider: ProviderFCM}
	if err := request.Accept(fcm); err != nil {
		t.Fatal(err)
	}
	if (Request{Operation: Next}).Accept(fcm) == nil {
		t.Fatal("next returned a token")
	}
}

func TestInboxBoundaries(t *testing.T) {
	id := strings.Repeat("a", 32)
	for _, request := range []Request{{Operation: Register}, {Operation: Unregister}, {Operation: Status}, {Operation: Next}, {Operation: Acknowledge, Identifier: id}} {
		if err := request.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, request := range []Request{{}, {Operation: "subscribe"}, {Operation: Register, Identifier: id}, {Operation: Acknowledge}, {Operation: Acknowledge, Identifier: "../inbox"}} {
		if request.Validate() == nil {
			t.Fatalf("accepted invalid request %+v", request)
		}
	}
	message := Message{Identifier: id, Title: "Order shipped", Body: "Tap to track", Route: "/orders/7", Data: map[string]string{"order": "7"}}
	state := State{Permission: "authorized", Registered: true, Provider: ProviderFCM, Message: &message}
	if err := (Request{Operation: Next}).Accept(state); err != nil {
		t.Fatal(err)
	}
	if (Request{Operation: Acknowledge, Identifier: id}).Accept(state) == nil {
		t.Fatal("acknowledgement returned content")
	}
	for _, mutate := range []func(*Message){
		func(m *Message) { m.Identifier = "untrusted" },
		func(m *Message) { m.Route = "https://example.com/orders" },
		func(m *Message) { m.Route = "//example.com" },
		func(m *Message) { m.Title = "bad\x00title" },
		func(m *Message) { m.Data = map[string]string{"": "x"} },
		func(m *Message) { m.Data = map[string]string{"big": strings.Repeat("x", MaxDataBytes)} },
	} {
		invalid := message
		mutate(&invalid)
		if invalid.Validate() == nil {
			t.Fatalf("accepted invalid message %+v", invalid)
		}
		if (Request{Operation: Next}).Accept(State{Permission: "authorized", Provider: ProviderFCM, Message: &invalid}) == nil {
			t.Fatalf("next accepted invalid message %+v", invalid)
		}
	}
}
