package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"strings"
	"testing"
)

func TestLocationPurpose(t *testing.T) {
	application := notesApp(t).forIOS()
	for _, purpose := range []string{"", `Show "my position" & nearby pins.`} {
		application.LocationUsage = purpose
		plist, err := render("templates/ios/Info.plist.tmpl", application)
		if err != nil {
			t.Fatal(err)
		}
		wellFormed(t, "location purpose", plist)
		if bytes.Contains(plist, []byte("NSLocationWhenInUseUsageDescription")) != (purpose != "") {
			t.Fatal("incorrect location permission packaging")
		}
		var escaped bytes.Buffer
		if err := xml.EscapeText(&escaped, []byte(purpose)); err != nil {
			t.Fatal(err)
		}
		if purpose != "" && !bytes.Contains(plist, escaped.Bytes()) {
			t.Fatal("unescaped purpose")
		}
	}
	for _, purpose := range []string{strings.Repeat("x", 513), "bad\x00purpose", "bad\npurpose", string([]byte{0xff})} {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"build", "--platform", "ios-sim", "--pkg", notesPkg, "--location-usage", purpose, "--dry-run"}, &stdout, &stderr)
		if code != 2 || !strings.Contains(stderr.String(), "--location-usage") || stdout.Len() != 0 {
			t.Fatalf("unsafe purpose reached build: %d %s %s", code, &stdout, &stderr)
		}
	}
}
