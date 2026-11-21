package push

import (
	"strings"
	"testing"
)

func TestRequestAndState(t *testing.T) {
	for _, operation := range []string{Register, Unregister, Status} {
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
		if err := request.Accept(State{Permission: permission, Registered: true, Token: "00abcdef", Environment: "development"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, state := range []State{
		{}, {Permission: "unknown"}, {Permission: "authorized", Token: "abcd"},
		{Permission: "authorized", Registered: true, Token: "ABCDEF"},
		{Permission: "authorized", Registered: true, Token: "a"},
		{Permission: "authorized", Registered: true, Token: "zz"},
		{Permission: "authorized", Registered: true, Token: strings.Repeat("ab", 257)},
		{Permission: "authorized", Environment: "sandbox"},
	} {
		if request.Accept(state) == nil {
			t.Errorf("accepted invalid state: %+v", state)
		}
	}
	if (Request{Operation: Unregister}).Accept(State{Permission: "authorized", Registered: true}) == nil {
		t.Fatal("unregister retained registration")
	}
	if err := (Request{Operation: Status}).Accept(State{Permission: "authorized", Registered: true, Token: strings.Repeat("ab", 256), Environment: "production"}); err != nil {
		t.Fatal(err)
	}
}
