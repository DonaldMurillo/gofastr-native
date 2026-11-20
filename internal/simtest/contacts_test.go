package simtest

import (
	"context"
	"github.com/DonaldMurillo/gofastr-native/render/webview/mobileui"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"testing"
)

func TestNotesContactsPickerWebView(t *testing.T) {
	ctx, service, userContext, traffic := deviceNotesBrowser(t, mobileui.IOS)
	note, err := service.Save(userContext, "", "Contact picker fixture", "Saved text")
	if err != nil {
		t.Fatal(err)
	}
	check := deviceBrowserCheck(t, ctx)
	check(chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(`
 window.__contactStatus = 'completed';
 window.__contactRequests = [];
 window.__gofastrDeviceServices = {
 capabilities: async () => ({'contacts/v1':'available'}),
 perform: async request => {
 window.__contactRequests.push(request);
 if(window.__contactStatus !== 'completed') return {status:window.__contactStatus};
 const contacts=[{id:'opaque-1',givenName:'Ada',familyName:'Lovelace',phones:[{label:'mobile',value:'+123'}]}];
 if(request.contacts.multiple) contacts.push({id:'opaque-2',givenName:'Grace',familyName:'Hopper',phones:[{label:'work',value:'+456'}]});
 return {status:'completed',contacts:{contacts}};
 }
 };`).Do(ctx)
		return err
	}), chromedp.Navigate(origin+"/notes/"+note.ID), waitForFileScreen("/notes/"+note.ID))
	perform := func(label string) {
		t.Helper()
		check(clickMobileAction(label), chromedp.WaitVisible("[data-gfw-service-perform]", chromedp.ByQuery), chromedp.Click("[data-gfw-service-perform]", chromedp.ByQuery), chromedp.Poll("!document.querySelector('[data-gfw-service]')", nil))
	}
	perform("Pick contact")
	check(chromedp.Poll("document.body.innerText.includes('Picked: Ada Lovelace +123') && window.__contactRequests.length === 1 && !window.__contactRequests[0].contacts.multiple", nil), chromedp.Evaluate("window.__contactStatus='cancelled'", nil))
	perform("Pick contact")
	check(chromedp.Poll("document.body.innerText.includes('Device action cancelled') && window.__contactRequests.length === 2", nil), chromedp.Evaluate("window.__contactStatus='completed'", nil))
	perform("Pick contacts")
	check(chromedp.Poll("document.body.innerText.includes('Picked: Ada Lovelace +123, Grace Hopper +456') && window.__contactRequests[2].contacts.multiple", nil), chromedp.Poll("document.querySelector('textarea[name=body]').value === 'Saved text'", nil))
	assertDeviceTraffic(t, traffic)
}
