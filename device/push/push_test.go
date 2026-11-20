package push

import (
	"strings"
	"testing"
)

func TestRequestAndMessageBoundaries(t *testing.T) {
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
	token := "cZ1:APA91b-token_value"
	if err := (Request{Operation: Register}).Accept(Result{Token: token}); err != nil {
		t.Fatal(err)
	}
	if (Request{Operation: Register}).Accept(Result{}) == nil {
		t.Fatal("registration without a token")
	}
	if (Request{Operation: Unregister}).Accept(Result{Token: token}) == nil || (Request{Operation: Status}).Accept(Result{Token: "bad token"}) == nil {
		t.Fatal("accepted an unexpected or unsafe token")
	}
	message := Message{Identifier: id, Title: "Order shipped", Body: "Tap to track", Route: "/orders/7", Data: map[string]string{"order": "7"}}
	if err := (Request{Operation: Next}).Accept(Result{Message: &message}); err != nil {
		t.Fatal(err)
	}
	if (Request{Operation: Acknowledge, Identifier: id}).Accept(Result{Message: &message}) == nil {
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
	}
}
