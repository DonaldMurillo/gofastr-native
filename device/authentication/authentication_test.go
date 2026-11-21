package authentication

import (
	"strings"
	"testing"
)

func TestRequestValidation(t *testing.T) {
	good := Request{Operation: OAuth, AuthorizationURL: "https://example.com/authorize", RedirectURI: "dev.example.app:/oauth", State: "fresh_state"}
	for _, request := range []Request{good, {Operation: Apple, Nonce: "fresh_nonce"}} {
		if err := request.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, tweak := range []func(*Request){
		func(r *Request) { r.AuthorizationURL = "http://example.com/authorize" },
		func(r *Request) { r.AuthorizationURL = "https://user@example.com/" },
		func(r *Request) { r.RedirectURI = "https://example.com/oauth" },
		func(r *Request) { r.RedirectURI = "dev.example.app:/oauth?code=x" },
		func(r *Request) { r.RedirectURI = "dev.example.app:/oauth#fragment" },
		func(r *Request) { r.RedirectURI = "dev.example.app:opaque" },
		func(r *Request) { r.State = "" },
		func(r *Request) { r.State = "white space" },
		func(r *Request) { r.Nonce = "wrong-operation" },
		func(r *Request) { r.Operation = Apple },
	} {
		bad := good
		tweak(&bad)
		if bad.Validate() == nil {
			t.Errorf("accepted invalid request: %+v", bad)
		}
	}
}

func TestResultValidation(t *testing.T) {
	apple := Request{Operation: Apple, Nonce: "nonce"}
	if err := apple.Accept(&Result{AuthorizationCode: "code", IdentityToken: "token", User: "user"}); err != nil {
		t.Fatal(err)
	}
	if apple.Accept(&Result{AuthorizationCode: "code"}) == nil {
		t.Fatal("accepted missing Apple credentials")
	}
	oauth := Request{Operation: OAuth, State: "state"}
	if err := oauth.Accept(&Result{AuthorizationCode: "code", State: "state"}); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []*Result{
		nil, {AuthorizationCode: "code"}, {AuthorizationCode: "code", State: "wrong"},
		{AuthorizationCode: "code", State: "state", IdentityToken: "unrelated"},
		{AuthorizationCode: strings.Repeat("x", 8193), State: "state"},
	} {
		if oauth.Accept(invalid) == nil {
			t.Error("accepted invalid OAuth result")
		}
	}
}
