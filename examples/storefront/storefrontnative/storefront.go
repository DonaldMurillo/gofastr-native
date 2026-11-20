// Package storefrontnative demonstrates native StoreKit 2 services.
package storefrontnative

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr-native/device/iap"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

const (
	unlockID  = "dev.gofastr.storefront.unlock"
	monthlyID = "dev.gofastr.storefront.monthly"
)

var lifecycle sync.RWMutex
var runtime *nativeui.Runtime
var app *nativeui.App

func Start(string) error {
	lifecycle.Lock()
	defer lifecycle.Unlock()
	if runtime != nil {
		return errors.New("storefront: already started")
	}
	screens, err := newApp()
	if err != nil {
		return err
	}
	r, err := nativeui.NewRuntime(screens, nativeui.RuntimeOptions{})
	if err != nil {
		return err
	}
	app, runtime = screens, r
	return nil
}

func Stop() error {
	lifecycle.Lock()
	defer lifecycle.Unlock()
	app, runtime = nil, nil
	return nil
}

func Root() string { return "/store" }

func Command(payload string) (string, error) {
	lifecycle.RLock()
	defer lifecycle.RUnlock()
	if runtime == nil {
		return "", errors.New("storefront: not started")
	}
	return runtime.CommandJSON(context.Background(), payload)
}

func Render(route, values string) (string, error) {
	lifecycle.RLock()
	defer lifecycle.RUnlock()
	if app == nil {
		return "", errors.New("storefront: not started")
	}
	return app.RenderJSON(context.Background(), route, values)
}

func Dispatch(route, action, values string) (string, error) {
	lifecycle.RLock()
	defer lifecycle.RUnlock()
	if app == nil {
		return "", errors.New("storefront: not started")
	}
	return app.DispatchJSON(context.Background(), route, action, values)
}

func newApp() (*nativeui.App, error) {
	var mu sync.Mutex
	var observedID string
	finished := make(map[string]bool)
	operations := []struct {
		path, title, action, text string
		request                   iap.Request
	}{
		{"products", "Product catalog", "Load products", "Read product names and localized prices from StoreKit.", iap.Request{Operation: iap.Products, ProductIDs: []string{unlockID, monthlyID}}},
		{"unlock", "Demo unlock", "Buy unlock", "Buy the non-consumable demo unlock using the system purchase sheet.", iap.Request{Operation: iap.Purchase, ProductID: unlockID}},
		{"monthly", "Demo monthly", "Subscribe", "Buy the monthly demo subscription using the system purchase sheet.", iap.Request{Operation: iap.Purchase, ProductID: monthlyID}},
		{"entitlements", "Current entitlements", "Check access", "Read verified purchases that currently grant access.", iap.Request{Operation: iap.Entitlements}},
		{"subscription", "Subscription status", "Check status", "Read the renewal status for the demo subscription group.", iap.Request{Operation: iap.SubscriptionStatus, ProductID: monthlyID}},
		{"updates", "Transaction updates", "Read updates", "Read unfinished transactions and recently observed updates.", iap.Request{Operation: iap.Updates}},
		{"finish", "Finish transaction", "Finish", "Acknowledge the last verified purchase or update observed in this demo. Apps should deliver purchases before finishing.", iap.Request{Operation: iap.Finish}},
		{"restore", "Restore purchases", "Restore", "Ask the App Store to sync purchases for the current account. This may ask you to sign in.", iap.Request{Operation: iap.Sync}},
	}
	routes := []nativeui.Route{{Path: Root(), Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		list := &nativeui.List{}
		for _, entry := range operations {
			list.Rows = append(list.Rows, nativeui.Row{ID: entry.path, Title: entry.title, Destination: Root() + "/" + entry.path})
		}
		return nativeui.Screen{Title: "StoreKit Demo", List: list}, nil
	}}}
	for _, entry := range operations {
		routes = append(routes, nativeui.Route{
			Path: Root() + "/" + entry.path,
			Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
				return nativeui.Screen{Title: entry.title, Text: entry.text, Actions: []nativeui.Action{{ID: "perform", Label: entry.action, Primary: true}}}, nil
			},
			Actions: map[string]nativeui.Handle{"perform": func(context.Context, nativeui.Request) (nativeui.Result, error) {
				request := entry.request
				if request.Operation == iap.Finish {
					mu.Lock()
					request.TransactionID = observedID
					mu.Unlock()
					if request.TransactionID == "" {
						return nativeui.Result{Announcement: "No transaction has been observed."}, nil
					}
				}
				return nativeui.Result{Service: &nativeui.ServiceRequest{ID: "store", IAP: &request}}, nil
			}},
			Services: map[string]nativeui.ServiceRespond{"store": func(_ context.Context, request nativeui.Request, outcome nativeui.ServiceResult) (nativeui.Result, error) {
				message := outcome.Message
				if outcome.Status != nativeui.EffectCompleted {
					if message == "" {
						message = "App Store operation: " + string(outcome.Status) + "."
					}
					return nativeui.Result{Announcement: message}, nil
				}
				result := outcome.IAP
				switch request.Service.IAP.Operation {
				case iap.Products:
					var prices []string
					for _, product := range result.Products {
						prices = append(prices, product.DisplayName+" "+product.DisplayPrice)
					}
					message = fmt.Sprintf("Products (%d): %s", len(prices), strings.Join(prices, ", "))
				case iap.Purchase:
					message = "Purchase pending."
					if result.Transaction != nil {
						mu.Lock()
						if !finished[result.Transaction.ID] {
							observedID = result.Transaction.ID
						}
						mu.Unlock()
						message = "Purchased: " + result.Transaction.ProductID
					}
				case iap.Entitlements, iap.Updates:
					label := "Entitlements"
					if request.Service.IAP.Operation == iap.Updates {
						label = "Updates"
						mu.Lock()
						for i := len(result.Transactions) - 1; i >= 0; i-- {
							if id := result.Transactions[i].ID; !finished[id] {
								observedID = id
								break
							}
						}
						mu.Unlock()
					}
					var products []string
					for _, transaction := range result.Transactions {
						products = append(products, transaction.ProductID)
					}
					message = fmt.Sprintf("%s (%d): %s", label, len(products), strings.Join(products, ", "))
				case iap.SubscriptionStatus:
					var states []string
					for _, status := range result.Subscriptions {
						states = append(states, string(status.State))
					}
					message = fmt.Sprintf("Subscription statuses (%d): %s", len(states), strings.Join(states, ", "))
				case iap.Finish:
					mu.Lock()
					id := request.Service.IAP.TransactionID
					finished[id] = true
					if observedID == id {
						observedID = ""
					}
					mu.Unlock()
					message = "Finished transaction."
				case iap.Sync:
					message = "Purchases synced."
				}
				return nativeui.Result{Announcement: message}, nil
			}},
		})
	}
	return nativeui.New(Root(), routes...)
}
