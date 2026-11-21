package storefrontnative

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/iap"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func TestFinishSkipsCachedAcknowledgedUpdates(t *testing.T) {
	app, err := newApp()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := nativeui.NewRuntime(app, nativeui.RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	send := func(command nativeui.Command) nativeui.Reply {
		t.Helper()
		command.Version = nativeui.ProtocolVersion
		reply := runtime.Handle(context.Background(), command)
		if reply.Error != nil {
			t.Fatal(reply.Error)
		}
		return reply
	}
	hello := send(nativeui.Command{Kind: "hello", Hello: &nativeui.Hello{Minimum: 1, Maximum: 1, Renderer: "uikit", Capabilities: map[nativeui.Capability]nativeui.Support{
		nativeui.CapabilityNavigation: nativeui.SupportAvailable,
		nativeui.CapabilityEditor:     nativeui.SupportAvailable,
		nativeui.CapabilityIAP:        nativeui.SupportAvailable,
	}}})
	perform := func(route, wantID string, result *iap.Result) {
		t.Helper()
		opened := send(nativeui.Command{Kind: "open", Connection: hello.Connection, Route: Root() + "/" + route})
		command := nativeui.Command{Kind: "action", Connection: hello.Connection, Instance: opened.Instance, Revision: opened.Revision, Action: "perform", RequestID: "start"}
		send(command)
		command.Kind = "complete"
		shown := send(command)
		if result == nil {
			if shown.Response == nil || shown.Response.Service != nil || shown.Response.Announcement != "No transaction has been observed." {
				t.Fatalf("expected no transaction left to finish, got %+v", shown.Response)
			}
			return
		}
		if shown.Response.Service == nil || shown.Response.Service.IAP == nil || shown.Response.Service.IAP.TransactionID != wantID {
			t.Fatalf("%s: want transaction ID %q, got %+v", route, wantID, shown.Response.Service)
		}
		command.Kind, command.RequestID, command.ServiceID = "service-result", "observed", shown.ServiceID
		command.ServiceResult = &nativeui.ServiceResult{Status: nativeui.EffectCompleted, IAP: result}
		send(command)
		command.Kind = "complete"
		send(command)
	}
	updates := &iap.Result{Transactions: []iap.Transaction{
		{ID: "1", OriginalID: "1", ProductID: unlockID, Type: iap.NonConsumable, PurchasedAt: 1},
		{ID: "2", OriginalID: "2", ProductID: monthlyID, Type: iap.AutoRenewable, PurchasedAt: 1},
	}}
	perform("updates", "", updates)
	perform("finish", "2", &iap.Result{})
	perform("updates", "", updates)
	perform("finish", "1", &iap.Result{})
	perform("updates", "", updates)
	perform("finish", "", nil)
}
