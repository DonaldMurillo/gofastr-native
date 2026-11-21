package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/DonaldMurillo/gofastr-native/device/push"
	"github.com/DonaldMurillo/gofastr-native/internal/uitext"
)

// Android stores push messages before Go sees them, and Go rejects a whole
// inbox entry it cannot validate. Such an entry stays first in the inbox and
// can never be acknowledged, so the Java sanitizers must only produce values
// Go accepts. Run the real helpers from Push.java and check their output with
// the Go rules.
func TestAndroidPushInboxValuesPassGoValidation(t *testing.T) {
	javac, err := exec.LookPath("javac")
	if err != nil {
		t.Skip("JDK unavailable")
	}
	java, err := exec.LookPath("java")
	if err != nil {
		t.Skip("Java runtime unavailable")
	}
	data, err := os.ReadFile("../../render/android/Push.java")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	var helpers []string
	for _, name := range []string{"localRoute", "hex", "utf8", "plain", "bounded", "wellFormed"} {
		// A one-line method ends on its own line; otherwise read to the
		// method's closing brace.
		method := regexp.MustCompile(`(?m)^    (?:private )?static \w+ ` + name + `\([^\n]*}\n`).FindString(source)
		if method == "" {
			method = regexp.MustCompile(`(?ms)^    (?:private )?static \w+ ` + name + `\(.*?^    }\n`).FindString(source)
		}
		if method == "" {
			t.Fatalf("Push.java has no %s helper", name)
		}
		helpers = append(helpers, method)
	}
	// javaString writes s as a Java literal of UTF-16 code units, so a test
	// input can hold a lone surrogate.
	javaString := func(units []uint16) string {
		var b strings.Builder
		b.WriteByte('"')
		for _, unit := range units {
			fmt.Fprintf(&b, `\u%04x`, unit)
		}
		b.WriteByte('"')
		return b.String()
	}
	emoji := utf16.Encode([]rune("😀"))
	title := append(utf16.Encode([]rune(strings.Repeat("a", 198))), emoji...)
	lone := append(utf16.Encode([]rune("/notes/")), emoji[0])
	routes := [][]uint16{
		utf16.Encode([]rune("/notes/1")), utf16.Encode([]rune("/a%20b?c=1")), utf16.Encode([]rune("/a\x01b")),
		utf16.Encode([]rune("/a\x7fb")), utf16.Encode([]rune("/a%zz")), utf16.Encode([]rune("/a%4")), utf16.Encode([]rune("/a%")),
		utf16.Encode([]rune("/a%１１")), utf16.Encode([]rune("//host")), lone,
	}
	texts := [][]uint16{title, append(utf16.Encode([]rune("ab")), emoji[1]), append(utf16.Encode([]rune(strings.Repeat("é", 99))), emoji...)}
	var calls strings.Builder
	for _, route := range routes {
		calls.WriteString("        print(localRoute(" + javaString(route) + "));\n")
	}
	for _, text := range texts {
		calls.WriteString("        print(plain(bounded(" + javaString(text) + ",200)));\n")
	}
	harness := "public class PushInboxTest {\n" + strings.Join(helpers, "") + `
    static void print(String value) {
        StringBuilder out = new StringBuilder();
        for (int i = 0; i < value.length(); i++) out.append(i == 0 ? "" : " ").append((int) value.charAt(i));
        System.out.println(out);
    }
    public static void main(String[] args) {
` + calls.String() + "    }\n}\n"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "PushInboxTest.java"), []byte(harness), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(javac, "-d", dir, filepath.Join(dir, "PushInboxTest.java")).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(java, "-cp", dir, "PushInboxTest").Output()
	if err != nil {
		t.Fatal(err)
	}
	var results []string
	for _, line := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		var units []uint16
		for _, field := range strings.Fields(line) {
			unit, err := strconv.Atoi(field)
			if err != nil {
				t.Fatal(err)
			}
			units = append(units, uint16(unit))
		}
		// The Go binding decodes UTF-16 the same way: lone surrogates become U+FFFD.
		results = append(results, string(utf16.Decode(units)))
	}
	if len(results) != len(routes)+len(texts) {
		t.Fatalf("got %d results:\n%s", len(results), out)
	}
	for i, route := range results[:len(routes)] {
		if route != "" && !push.ValidRoute(route) {
			t.Errorf("Java kept route %q that Go rejects", route)
		}
		if want := i < 2; (route != "") != want {
			t.Errorf("route %d: got %q, want kept=%t", i, route, want)
		}
	}
	for _, text := range results[len(routes):] {
		if !uitext.Plain(text, 200) {
			t.Errorf("Java title %q (%d bytes) fails Go's bound", text, len(text))
		}
	}
	if results[len(routes)] != strings.Repeat("a", 198) {
		t.Errorf("a title is not cut before a surrogate pair: %q", results[len(routes)])
	}
}
