package speechnative

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/speech"
)

func TestSpeechDemo(t *testing.T) {
	ctx := context.Background()
	app, err := newApp()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Render(ctx, Root(), nil); err != nil {
		t.Fatal(err)
	}
	for action, operation := range map[string]speech.Operation{"speak": speech.Speak, "recognize": speech.Recognize} {
		response, err := app.Dispatch(ctx, Root(), action, map[string]string{"text": "Hello"})
		if err != nil || response.Service == nil || response.Service.Speech.Operation != operation {
			t.Fatalf("%s: %+v, %v", action, response, err)
		}
	}
}
