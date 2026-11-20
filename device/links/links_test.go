package links

import (
	"strings"
	"testing"
)

func TestOpenValidate(t *testing.T) {
	for _, value := range []string{
		"https://example.org", "http://localhost:8080/health", "HTTPS://example.org/a%20b?q=x%2By#section",
		"http://[::1]:8080/", "https://example.org/" + strings.Repeat("x", 4076),
	} {
		if err := (Open{URL: value}).Validate(); err != nil {
			t.Errorf("rejected valid URL (%d bytes): %v", len(value), err)
		}
	}
	for _, value := range []string{
		"", "/relative", "//example.org", "https:example.org", "https:///path",
		"intent://example.org", "javascript:alert(1)", "file:///path", "content://example.org",
		"https://foo_bar.org", "https://!.org", "https://example.123", "https://bücher.example",
		"https://user@example.org", "https://example.org:0", "https://example.org:", "https://example.org:65536",
		"https://example.org/a b", "https://example.org/a\\b", "https://example.org/?q=%GG",
		"https://example.org/a\n", "https://example.org/" + strings.Repeat("x", 4077),
	} {
		if err := (Open{URL: value}).Validate(); err == nil {
			t.Errorf("accepted invalid URL (%d bytes): %.100q", len(value), value)
		}
	}
}
