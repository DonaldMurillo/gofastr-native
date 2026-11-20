package featuresnative

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func TestEveryDemoActionUsesValidSharedServiceContracts(t *testing.T) {
	app, err := newApp()
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/features", "/features/location", "/features/inbox", "/features/secure", "/features/widgets"} {
		screen, err := app.Render(context.Background(), route, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, action := range screen.Actions {
			response, err := app.Dispatch(context.Background(), route, action.ID, nil)
			if err != nil {
				t.Fatalf("%s/%s: %v", route, action.ID, err)
			}
			if response.Service != nil {
				if err := response.Service.Validate(); err != nil {
					t.Fatalf("%s/%s: %v", route, action.ID, err)
				}
			}
		}
	}
}

func TestLocationFailureMessageReachesStatus(t *testing.T) {
	for _, outcome := range []struct {
		status        nativeui.EffectStatus
		message, want string
	}{
		{nativeui.EffectFailed, "Location services are off.", "Location services are off."},
		{nativeui.EffectFailed, "No location fix arrived in time.", "No location fix arrived in time."},
		{nativeui.EffectDenied, "", "Device result: denied"},
	} {
		t.Run(outcome.want, func(t *testing.T) {
			app, err := newApp()
			if err != nil {
				t.Fatal(err)
			}
			runtime, err := nativeui.NewRuntime(app, nativeui.RuntimeOptions{})
			if err != nil {
				t.Fatal(err)
			}
			command := func(c nativeui.Command) nativeui.Reply {
				t.Helper()
				c.Version = nativeui.ProtocolVersion
				payload, err := json.Marshal(c)
				if err != nil {
					t.Fatal(err)
				}
				raw, err := runtime.CommandJSON(context.Background(), string(payload))
				if err != nil {
					t.Fatal(err)
				}
				var reply nativeui.Reply
				if err := json.Unmarshal([]byte(raw), &reply); err != nil {
					t.Fatal(err)
				}
				if reply.Error != nil {
					t.Fatal(reply.Error)
				}
				return reply
			}
			hello := command(nativeui.Command{Kind: "hello", Hello: &nativeui.Hello{Minimum: 1, Maximum: 1, Renderer: "android", Capabilities: map[nativeui.Capability]nativeui.Support{nativeui.CapabilityNavigation: nativeui.SupportAvailable, nativeui.CapabilityEditor: nativeui.SupportAvailable, nativeui.CapabilityLocation: nativeui.SupportAvailable}}})
			opened := command(nativeui.Command{Kind: "open", Connection: hello.Connection, Route: "/features/location"})
			c := nativeui.Command{Kind: "action", Connection: hello.Connection, Instance: opened.Instance, Revision: 1, Action: "precise", RequestID: "read"}
			command(c)
			c.Kind = "complete"
			shown := command(c)
			c.Kind, c.RequestID, c.ServiceID = "service-result", "observed", shown.ServiceID
			c.ServiceResult = &nativeui.ServiceResult{Status: outcome.status, Message: outcome.message}
			if got := command(c).Response.Announcement; got != outcome.want {
				t.Fatalf("announcement = %q, want %q", got, outcome.want)
			}
		})
	}
}
