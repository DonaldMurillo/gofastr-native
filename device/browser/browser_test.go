package browser

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	for _, value := range []string{"https://example.com", "http://localhost:8080/path?q=1#section", "https://example.com/" + strings.Repeat("a", 2028)} {
		for _, label := range []string{"", "done", "close", "cancel"} {
			if err := (Request{URL: value, ReaderMode: true, DismissLabel: label}).Validate(); err != nil {
				t.Errorf("%q: %v", value, err)
			}
		}
	}
	for _, value := range []string{"", "/relative", "//example.com", "file:///etc/passwd", "javascript:alert(1)", "https:///path", "https://user:pass@example.com", "https://@example.com", "https://example.com/\n", "https://example.com/a b", "https://example.com/\xff", "https://example.com/" + strings.Repeat("a", 2048)} {
		if err := (Request{URL: value}).Validate(); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	if err := (Request{URL: "https://example.com", DismissLabel: "other"}).Validate(); err == nil {
		t.Fatal("accepted unknown label")
	}
}
