package notesscreens

import (
	"context"
	"fmt"
	"sync"

	"github.com/DonaldMurillo/gofastr-native/device/push"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func pushScreen(permission, token, environment, result string) nativeui.Screen {
	return nativeui.Screen{Title: "Push notifications", List: &nativeui.List{Rows: []nativeui.Row{
		{ID: "push-permission", Title: "Permission", Subtitle: permission, Disabled: true},
		{ID: "push-token", Title: "Push token", Subtitle: token, Disabled: true},
		{ID: "push-environment", Title: "Environment", Subtitle: environment, Disabled: true},
		{ID: "push-result", Title: "Result", Subtitle: result, Disabled: true},
	}}, Actions: []nativeui.Action{
		{ID: "push-register", Label: "Register for push", Primary: true},
		{ID: "push-status", Label: "Push status"},
	}}
}

func pushSettings() []nativeui.Route {
	var mu sync.Mutex
	screen := pushScreen("Not checked", "Not checked", "Not checked", "Not requested")
	render := func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		mu.Lock()
		defer mu.Unlock()
		result := screen
		result.Actions = nil
		return result, nil
	}
	route := nativeui.Route{Path: "/settings/push", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		return pushScreen("Not checked", "Not checked", "Not checked", "Not requested"), nil
	}, Actions: map[string]nativeui.Handle{}, Services: map[string]nativeui.ServiceRespond{}}
	for id, operation := range map[string]string{"push-register": push.Register, "push-status": push.Status} {
		route.Actions[id] = func(context.Context, nativeui.Request) (nativeui.Result, error) {
			return nativeui.Result{Service: &nativeui.ServiceRequest{ID: id, Push: &push.Request{Operation: operation}}}, nil
		}
		route.Services[id] = func(ctx context.Context, request nativeui.Request, result nativeui.ServiceResult) (nativeui.Result, error) {
			mu.Lock()
			defer mu.Unlock()
			outcome := string(result.Status)
			if result.Message != "" {
				outcome += " - " + result.Message
			}
			if result.Status != nativeui.EffectCompleted {
				screen = pushScreen("Not observed", "Unavailable", "Unavailable", outcome)
				return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "push", Route: "/settings/push/result"}}, nil
			}
			value := result.Push
			token, environment := value.Token, value.Environment
			if token == "" {
				// The OS registration flag can stay on after a timed-out
				// Register or a restart; only a delivered token proves APNs
				// registration in this process.
				token = "None received in this process; register to request one"
			}
			if environment == "" {
				environment = "Not readable"
			}
			screen = pushScreen(fmt.Sprintf("%s; OS remote registration: %s", value.Permission, onOff(value.Registered)), token, environment, outcome)
			return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "push", Route: "/settings/push/result"}}, nil
		}
	}
	return []nativeui.Route{route, {Path: "/settings/push/result", Screen: render}}
}

func onOff(value bool) string {
	if value {
		return "on"
	}
	return "off"
}
