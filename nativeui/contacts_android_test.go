package nativeui

import (
	"encoding/json"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/contacts"
)

// TestAndroidContactsWire pins the JSON render/android/Contacts.java settles:
// one contact, the display name in givenName, an empty familyName and one
// labeled value for the single requested field.
func TestAndroidContactsWire(t *testing.T) {
	phone := ServiceRequest{ID: "contact", Contacts: &contacts.Request{Fields: []contacts.Field{contacts.Phone}}}
	email := ServiceRequest{ID: "contact", Contacts: &contacts.Request{Fields: []contacts.Field{contacts.Email}}}
	decode := func(body string) ServiceResult {
		t.Helper()
		var result ServiceResult
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	phoneResult := decode(`{"status":"completed","contacts":{"contacts":[{"id":"0r1-ABC","givenName":"Ada Lovelace","familyName":"","phones":[{"label":"Mobile","value":"+1 555 0100"}]}]}}`)
	if err := phone.accept(phoneResult); err != nil {
		t.Fatalf("phone: %v", err)
	}
	if email.accept(phoneResult) == nil {
		t.Fatal("phone row accepted for an email request")
	}
	emailResult := decode(`{"status":"completed","contacts":{"contacts":[{"id":"42","givenName":"Ada","familyName":"","emails":[{"label":"Work","value":"ada@example.org"}]}]}}`)
	if err := email.accept(emailResult); err != nil {
		t.Fatalf("email: %v", err)
	}
	for _, body := range []string{
		`{"status":"cancelled"}`,
		`{"status":"unsupported","message":"Android picks one phone number or one email address."}`,
		`{"status":"denied","message":"Access was denied."}`,
		`{"status":"failed","message":"The device operation failed."}`,
	} {
		if err := phone.accept(decode(body)); err != nil {
			t.Errorf("%s: %v", body, err)
		}
	}
}
