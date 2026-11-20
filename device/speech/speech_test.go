package speech

import (
	"strings"
	"testing"
)

func TestValidateRequest(t *testing.T) {
	for _, request := range []Request{
		{Operation: Speak, Text: "A café.\nAnother line."},
		{Operation: Speak, Text: strings.Repeat("é", MaxTextBytes/2), Language: "fr-CA"},
		{Operation: Recognize},
		{Operation: Recognize, Language: "zh-Hant-TW"},
	} {
		if err := request.Validate(); err != nil {
			t.Errorf("valid request (%s, %s): %v", request.Operation, request.Language, err)
		}
	}
	for _, request := range []Request{
		{}, {Operation: "play", Text: "Hello"}, {Operation: Speak},
		{Operation: Speak, Text: " \t\n\u00a0"},
		{Operation: Speak, Text: "hello\x00"},
		{Operation: Speak, Text: string([]byte{0xff})},
		{Operation: Speak, Text: strings.Repeat("é", MaxTextBytes/2+1)},
		{Operation: Recognize, Text: "not a recognition input"},
		{Operation: Recognize, Language: "en_US"},
		{Operation: Recognize, Language: "en-"},
		{Operation: Recognize, Language: "en\x00"},
		{Operation: Recognize, Language: strings.Repeat("x", 65)},
	} {
		if request.Validate() == nil {
			t.Errorf("accepted invalid request (%s, %q, %d bytes)", request.Operation, request.Language, len(request.Text))
		}
	}
}

func TestValidateTranscript(t *testing.T) {
	recognition := Request{Operation: Recognize}
	if err := recognition.Accept("Bonjour!"); err != nil {
		t.Fatal(err)
	}
	for _, transcript := range []string{"", "\t ", "bad\x00", strings.Repeat("é", MaxTextBytes/2+1)} {
		if recognition.Accept(transcript) == nil {
			t.Errorf("accepted invalid transcript (%d bytes)", len(transcript))
		}
	}
	synthesis := Request{Operation: Speak, Text: "Hello!"}
	if synthesis.Accept("") != nil || synthesis.Accept("hello") == nil {
		t.Fatal("synthesis must not return a transcript")
	}
}
