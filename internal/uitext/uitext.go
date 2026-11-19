// Package uitext holds the bounded-text predicates shared by the
// device-service validation boundaries.
package uitext

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Plain reports whether value is valid UTF-8, at most maximum bytes long,
// and free of control characters.
func Plain(value string, maximum int) bool {
	return utf8.ValidString(value) && len(value) <= maximum && !strings.ContainsFunc(value, unicode.IsControl)
}

// Multiline is Plain but permits tabs and newlines.
func Multiline(value string, maximum int) bool {
	return utf8.ValidString(value) && len(value) <= maximum && !strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) && r != '\n' && r != '\t'
	})
}

// NoNUL reports whether value is valid UTF-8, at most maximum bytes long,
// and free of NUL bytes; other control characters such as newlines pass.
func NoNUL(value string, maximum int) bool {
	return utf8.ValidString(value) && len(value) <= maximum && !strings.ContainsRune(value, '\x00')
}

// Token reports whether value is at most maximum bytes long and uses only
// letters, digits or . _ : - so it is safe to log and compare.
func Token(value string, maximum int) bool {
	return len(value) <= maximum && !strings.ContainsFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:-", r))
	})
}
