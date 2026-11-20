package contacts

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/files"
)

func TestContactPolicy(t *testing.T) {
	phone := Request{Title: "Choose contact", Kind: Phone}
	document := files.Document{Name: "Contact.json", MediaType: "application/json", Data: []byte(`{"name":"Ada","phone":"+1 555","email":""}`)}
	contact, err := phone.Contact(document)
	if err != nil || contact.Name != "Ada" || contact.Phone != "+1 555" {
		t.Fatalf("contact: %+v %v", contact, err)
	}
	for _, body := range []string{"", `null`, `{}`, `{"phone":""}`, `{"phone":"123","email":"a@b"}`, `{"phone":"123","extra":1}`, `{"phone":"123"} {}`, `{"phone":"a\nb"}`, `{"phone":"` + strings.Repeat("1", 257) + `"}`, `{"phone":"123","name":"` + strings.Repeat("x", 513) + `"}`, strings.Repeat(" ", MaxBytes+1)} {
		bad := document
		bad.Data = []byte(body)
		if err := phone.Accept(bad); err == nil {
			t.Errorf("accepted invalid JSON %q", body)
		}
	}
	for _, change := range []func(*files.Document){func(d *files.Document) { d.Name = "other.json" }, func(d *files.Document) { d.MediaType = "text/plain" }, func(d *files.Document) { d.Name = "../Contact.json" }} {
		bad := document
		change(&bad)
		if phone.Accept(bad) == nil {
			t.Error("accepted invalid document metadata")
		}
	}
	email := Request{Title: "Email", Kind: Email}
	if email.Accept(document) == nil {
		t.Error("accepted phone for email request")
	}
	document.Data = []byte(`{"name":"Ada","phone":"","email":"ada@example.org"}`)
	if err := email.Accept(document); err != nil {
		t.Fatal(err)
	}
	if phone.Accept(document) == nil {
		t.Error("accepted email for phone request")
	}
	for _, request := range []Request{{}, {Title: "Pick", Kind: "unknown"}, {Title: strings.Repeat("x", 201), Kind: Phone}} {
		if request.Validate() == nil || request.Accept(document) == nil {
			t.Errorf("accepted invalid request: %+v", request)
		}
	}
}
