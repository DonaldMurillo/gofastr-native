package iap

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRequestValidation(t *testing.T) {
	for _, r := range []Request{
		{Operation: Products, ProductIDs: []string{"demo.unlock", "DEMO_123"}},
		{Operation: Purchase, ProductID: "demo.unlock"},
		{Operation: SubscriptionStatus, ProductID: "demo.monthly"},
		{Operation: Finish, TransactionID: "0"},
		{Operation: Finish, TransactionID: "18446744073709551615"},
		{Operation: Sync}, {Operation: Entitlements}, {Operation: Updates},
	} {
		if err := r.Validate(); err != nil {
			t.Errorf("%+v: %v", r, err)
		}
	}
	for _, r := range []Request{
		{}, {Operation: "restore"},
		{Operation: Products}, {Operation: Products, ProductIDs: []string{"a", "a"}},
		{Operation: Products, ProductIDs: []string{"a", "a/"}},
		{Operation: Products, ProductIDs: make([]string, MaxProductIDs+1)},
		{Operation: Purchase, ProductID: strings.Repeat("a", 101)},
		{Operation: Purchase, ProductID: "é"},
		{Operation: Purchase, ProductID: "a", TransactionID: "1"},
		{Operation: Finish, TransactionID: "01"},
		{Operation: Finish},
		{Operation: Finish, TransactionID: "18446744073709551616"},
		{Operation: Entitlements, ProductIDs: []string{"a"}},
	} {
		if err := r.Validate(); err == nil {
			t.Errorf("accepted %+v", r)
		}
	}
}

func TestTypedResultValidation(t *testing.T) {
	transaction := Transaction{ID: "18446744073709551615", OriginalID: "1", ProductID: "demo.unlock",
		Type: NonConsumable, PurchasedAt: 1700000000000}
	purchase := Request{Operation: Purchase, ProductID: transaction.ProductID}
	success := Result{PurchaseState: Purchased, Transaction: &transaction}
	raw, err := json.Marshal(success)
	if err != nil || !strings.Contains(string(raw), `"id":"18446744073709551615"`) {
		t.Fatalf("transaction ID is not lossless: %s (%v)", raw, err)
	}
	var decoded Result
	if err := json.Unmarshal(raw, &decoded); err != nil || purchase.Accept(decoded) != nil || *decoded.Transaction != transaction {
		t.Fatalf("round trip: %+v (%v)", decoded, err)
	}
	if err := purchase.Accept(Result{PurchaseState: Pending}); err != nil {
		t.Fatal(err)
	}
	for _, outcome := range []Result{{}, {PurchaseState: Purchased}, {PurchaseState: Pending, Transaction: &transaction},
		{PurchaseState: Purchased, Transaction: &transaction, Transactions: []Transaction{transaction}}} {
		if err := purchase.Accept(outcome); err == nil {
			t.Errorf("accepted contradictory purchase outcome %+v", outcome)
		}
	}
	if err := (Request{Operation: Purchase, ProductID: "other"}).Accept(success); err == nil {
		t.Fatal("accepted transaction for the wrong product")
	}
	catalog := Request{Operation: Products, ProductIDs: []string{"demo.unlock", "demo.missing"}}
	product := Product{ID: "demo.unlock", Type: NonConsumable, DisplayName: "Unlock", DisplayPrice: "$0.99"}
	if err := catalog.Accept(Result{Products: []Product{product}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Accept(Result{Products: []Product{product, product}}); err == nil {
		t.Fatal("accepted duplicate products")
	}
	transaction.RevokedAt = transaction.PurchasedAt
	observed := Result{Transactions: []Transaction{transaction}}
	if (Request{Operation: Updates}).Accept(observed) != nil || (Request{Operation: Entitlements}).Accept(observed) == nil {
		t.Fatal("a revocation is an update, not an entitlement")
	}
	transaction.Type = AutoRenewable
	transaction.ID, transaction.OriginalID = "0", "0"
	status := Request{Operation: SubscriptionStatus, ProductID: "demo.monthly"}
	if err := status.Accept(Result{Subscriptions: []Subscription{{State: GracePeriod, AutoRenews: true, Transaction: transaction}}}); err != nil {
		t.Fatal(err)
	}
	if err := (Request{Operation: Updates}).Accept(Result{Transactions: []Transaction{transaction}}); err != nil {
		t.Fatal(err)
	}
}
