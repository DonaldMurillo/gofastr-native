package network

import "testing"

func TestStatus(t *testing.T) {
	request := Request{Operation: Status}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, transport := range []string{"wifi", "cellular", "ethernet", "vpn", "other"} {
		for _, metered := range []bool{false, true} {
			for _, validated := range []bool{false, true} {
				if err := request.Accept(Result{Connected: true, Transport: transport, Metered: metered, Validated: validated}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if err := request.Accept(Result{Transport: "none"}); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"", "subscribe", "STATUS"} {
		bad := Request{Operation: operation}
		if bad.Validate() == nil || bad.Accept(Result{Transport: "none"}) == nil {
			t.Fatal("accepted operation", operation)
		}
	}
	for _, result := range []Result{{}, {Transport: "unknown"}, {Transport: "wifi"}, {Connected: true, Transport: "none"}, {Connected: true, Transport: "unknown"}, {Transport: "none", Metered: true}, {Transport: "none", Validated: true}} {
		if request.Accept(result) == nil {
			t.Fatalf("accepted %#v", result)
		}
	}
}
