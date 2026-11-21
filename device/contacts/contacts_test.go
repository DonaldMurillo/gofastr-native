package contacts

import (
	"strings"
	"testing"
)

func TestRequestValidation(t *testing.T) {
	for _, r := range []Request{{}, {Multiple: true, Fields: []Field{Phone, Email, Postal, Organization}}} {
		if err := r.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []Request{{Fields: []Field{"unknown"}}, {Fields: []Field{Phone, Phone}}} {
		if r.Validate() == nil {
			t.Fatal("accepted invalid fields")
		}
	}
}
func TestAccept(t *testing.T) {
	base := Contact{ID: "opaque", GivenName: "Ada", FamilyName: "Lovelace"}
	for _, tc := range []struct {
		name      string
		request   Request
		selection *Selection
		valid     bool
	}{
		{"single", Request{}, &Selection{Contacts: []Contact{base}}, true},
		{"nil", Request{}, nil, false}, {"empty", Request{}, &Selection{}, false},
		{"multiple", Request{Multiple: true}, &Selection{Contacts: []Contact{base, base}}, true},
		{"single-overflow", Request{}, &Selection{Contacts: []Contact{base, base}}, false},
		{"limit", Request{Multiple: true}, &Selection{Contacts: make([]Contact, 51)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if (tc.request.Accept(tc.selection) == nil) != tc.valid {
				t.Fatal("unexpected acceptance")
			}
		})
	}
	r := Request{Fields: []Field{Phone, Email, Postal, Organization}}
	c := base
	c.Phones = []Labeled{{Label: "mobile", Value: "+123"}}
	c.Emails = []Labeled{{Value: "a@example.com"}}
	c.Postal = []string{"Street\nCity"}
	c.Organization = "Company"
	if err := r.Accept(&Selection{Contacts: []Contact{c}}); err != nil {
		t.Fatal(err)
	}
	if (Request{}).Accept(&Selection{Contacts: []Contact{c}}) == nil {
		t.Fatal("accepted unrequested fields")
	}
	for _, mutate := range []func(*Contact){
		func(c *Contact) { c.ID = "" }, func(c *Contact) { c.GivenName = strings.Repeat("x", 513) },
		func(c *Contact) { c.FamilyName = "bad\x00" }, func(c *Contact) { c.Phones = make([]Labeled, 21) },
		func(c *Contact) { c.Emails = []Labeled{{Label: "bad\n"}} }, func(c *Contact) { c.Postal = []string{strings.Repeat("x", 2049)} },
	} {
		c := base
		mutate(&c)
		if r.Accept(&Selection{Contacts: []Contact{c}}) == nil {
			t.Fatal("accepted invalid contact")
		}
	}
}
