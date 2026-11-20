package notesnative

import (
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/push"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func TestPushSettings(t *testing.T) {
	startApp(t)
	if _, err := Render("/settings/push", ""); err != nil {
		t.Fatal(err)
	}
	connection := hello(t, "uikit", map[nativeui.Capability]nativeui.Support{nativeui.CapabilityPush: nativeui.SupportAvailable})
	for _, outcome := range []nativeui.ServiceResult{
		{Status: nativeui.EffectCompleted, Push: &push.State{Permission: "authorized", Registered: true, Token: "aabb"}},
		{Status: nativeui.EffectCompleted, Push: &push.State{Permission: "notDetermined"}},
		{Status: nativeui.EffectDenied},
		{Status: nativeui.EffectFailed},
	} {
		opened := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/settings/push"})
		if opened.Error != nil {
			t.Fatal(opened.Error)
		}
		shown := filesAction(t, nativeui.Command{Connection: connection, Instance: opened.Instance, RequestID: "register", Action: "push-register"})
		if shown.Response.Service.Push.Operation != push.Register {
			t.Fatal("wrong push operation")
		}
		result := serviceResult(t, nativeui.Command{Connection: connection, Instance: opened.Instance, RequestID: "registered"}, shown.ServiceID, outcome)
		destination := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: result.Response.Navigation.Route})
		if destination.Error != nil {
			t.Fatal(destination.Error)
		}
		rows := destination.Screen.List.Rows
		if rows[0].ID != "push-permission" || rows[1].ID != "push-token" {
			t.Fatal("unstable result IDs")
		}
		if outcome.Push != nil && outcome.Push.Token != "" && rows[1].Subtitle != outcome.Push.Token {
			t.Fatal("token not displayed")
		}
	}
}
