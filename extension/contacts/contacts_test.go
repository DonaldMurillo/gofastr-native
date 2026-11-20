package contacts_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/extension"
	"github.com/DonaldMurillo/gofastr-native/extension/contacts"
)

func TestRegisteredContacts(t *testing.T) {
	definition := contacts.Definition()
	registry, err := extension.New(definition)
	if err != nil {
		t.Fatal(err)
	}
	if !registry.Allows("contacts/v1") || definition.Manifest.Android.Factory != "GofastrContactsExtension" || definition.Manifest.HasIOS() {
		t.Fatal("incorrect manifest")
	}
	if _, err := extension.New(definition, definition); err == nil {
		t.Fatal("duplicate registration accepted")
	}
	operation, err := contacts.Pick(contacts.Request{Title: "Choose contact", Kind: contacts.Phone})
	if err != nil {
		t.Fatal(err)
	}
	if capability, err := registry.Validate(*operation); err != nil || capability != "contacts/v1" {
		t.Fatalf("validate: %s %v", capability, err)
	}
	document := files.Document{Name: "Contact.json", MediaType: "application/json", Data: []byte(`{"name":"Ada","phone":"123","email":""}`)}
	if err := definition.Accept(*operation, document); err != nil {
		t.Fatal(err)
	}
	document.Data = []byte(`{}`)
	if definition.Accept(*operation, document) == nil {
		t.Fatal("accepted empty contact")
	}
	for _, payload := range []string{"", `{}`, `{"title":"Pick","kind":"phone","extra":true}`, `{"title":"Pick","kind":"phone"} {}`} {
		bad := *operation
		bad.Payload = payload
		if _, err := registry.Validate(bad); err == nil {
			t.Errorf("accepted %q", payload)
		}
		if definition.Accept(bad, document) == nil {
			t.Error("invalid request accepted")
		}
	}
	if _, err := contacts.Pick(contacts.Request{}); err == nil {
		t.Fatal("invalid Pick accepted")
	}
}
