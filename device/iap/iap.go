// Package iap describes explicit iOS StoreKit 2 device service operations.
package iap

import (
	"errors"
	"strconv"
	"strings"
)

type Operation string

const (
	Products           Operation = "products"
	Purchase           Operation = "purchase"
	Sync               Operation = "sync"
	Entitlements       Operation = "entitlements"
	Updates            Operation = "updates"
	Finish             Operation = "finish"
	SubscriptionStatus Operation = "subscriptionStatus"
)

const MaxProductIDs = 100

type Request struct {
	Operation     Operation `json:"operation"`
	ProductIDs    []string  `json:"productIds,omitempty"`
	ProductID     string    `json:"productId,omitempty"`
	TransactionID string    `json:"transactionId,omitempty"`
}

type ProductType string

const (
	Consumable         ProductType = "consumable"
	NonConsumable      ProductType = "nonConsumable"
	AutoRenewable      ProductType = "autoRenewable"
	NonRenewing        ProductType = "nonRenewing"
	UnknownProductType ProductType = "unknown"
)

type Product struct {
	ID           string      `json:"id"`
	Type         ProductType `json:"type"`
	DisplayName  string      `json:"displayName"`
	Description  string      `json:"description"`
	DisplayPrice string      `json:"displayPrice"`
}

// Transaction is verified locally by StoreKit. IDs are strings to preserve
// all 64 bits on the wire. Dates are Unix milliseconds; zero means absent.
type Transaction struct {
	ID          string      `json:"id"`
	OriginalID  string      `json:"originalId"`
	ProductID   string      `json:"productId"`
	Type        ProductType `json:"type"`
	PurchasedAt int64       `json:"purchasedAt"`
	ExpiresAt   int64       `json:"expiresAt,omitempty"`
	RevokedAt   int64       `json:"revokedAt,omitempty"`
	IsUpgraded  bool        `json:"isUpgraded"`
}

type PurchaseState string

const (
	Purchased PurchaseState = "purchased"
	Pending   PurchaseState = "pending"
)

type RenewalState string

const (
	Subscribed     RenewalState = "subscribed"
	Expired        RenewalState = "expired"
	BillingRetry   RenewalState = "billingRetry"
	GracePeriod    RenewalState = "gracePeriod"
	Revoked        RenewalState = "revoked"
	UnknownRenewal RenewalState = "unknown"
)

type Subscription struct {
	State       RenewalState `json:"state"`
	AutoRenews  bool         `json:"autoRenews"`
	Transaction Transaction  `json:"transaction"`
}

type Result struct {
	Products      []Product      `json:"products,omitempty"`
	PurchaseState PurchaseState  `json:"purchaseState,omitempty"`
	Transaction   *Transaction   `json:"transaction,omitempty"`
	Transactions  []Transaction  `json:"transactions,omitempty"`
	Subscriptions []Subscription `json:"subscriptions,omitempty"`
}

func (r Request) Validate() error {
	valid := false
	switch r.Operation {
	case Products:
		valid = r.ProductID == "" && r.TransactionID == "" && len(r.ProductIDs) > 0 && len(r.ProductIDs) <= MaxProductIDs
		seen := make(map[string]bool)
		if valid {
			for _, id := range r.ProductIDs {
				if !validProduct(id) || seen[id] {
					return errors.New("iap: invalid or repeated product ID")
				}
				seen[id] = true
			}
		}
	case Purchase, SubscriptionStatus:
		valid = len(r.ProductIDs) == 0 && validProduct(r.ProductID) && r.TransactionID == ""
	case Finish:
		valid = len(r.ProductIDs) == 0 && r.ProductID == "" && validTransactionID(r.TransactionID)
	case Sync, Entitlements, Updates:
		valid = len(r.ProductIDs) == 0 && r.ProductID == "" && r.TransactionID == ""
	}
	if !valid {
		return errors.New("iap: invalid operation or arguments")
	}
	return nil
}

func (r Request) Accept(result Result) error {
	if err := r.Validate(); err != nil {
		return err
	}
	invalid := errors.New("iap: result does not match the operation")
	if (r.Operation != Products && len(result.Products) != 0) ||
		(r.Operation != Purchase && (result.PurchaseState != "" || result.Transaction != nil)) ||
		(r.Operation != Entitlements && r.Operation != Updates && len(result.Transactions) != 0) ||
		(r.Operation != SubscriptionStatus && len(result.Subscriptions) != 0) {
		return invalid
	}
	switch r.Operation {
	case Products:
		requested := make(map[string]bool, len(r.ProductIDs))
		for _, id := range r.ProductIDs {
			requested[id] = true
		}
		for _, product := range result.Products {
			if !requested[product.ID] || !validType(product.Type) || product.DisplayName == "" || product.DisplayPrice == "" {
				return invalid
			}
			delete(requested, product.ID)
		}
	case Purchase:
		if result.PurchaseState == Pending && result.Transaction == nil {
			return nil
		}
		if result.PurchaseState != Purchased || result.Transaction == nil || !validTransaction(*result.Transaction) || result.Transaction.ProductID != r.ProductID {
			return invalid
		}
	case Entitlements, Updates:
		seen := make(map[string]bool, len(result.Transactions))
		for _, transaction := range result.Transactions {
			if !validTransaction(transaction) || seen[transaction.ID] || (r.Operation == Entitlements && (transaction.RevokedAt != 0 || transaction.IsUpgraded || transaction.Type == Consumable)) {
				return invalid
			}
			seen[transaction.ID] = true
		}
	case SubscriptionStatus:
		for _, subscription := range result.Subscriptions {
			if !validTransaction(subscription.Transaction) || subscription.Transaction.Type != AutoRenewable {
				return invalid
			}
			switch subscription.State {
			case Subscribed, Expired, BillingRetry, GracePeriod, Revoked, UnknownRenewal:
			default:
				return invalid
			}
		}
	}
	return nil
}

func validProduct(id string) bool {
	return len(id) > 0 && len(id) <= 100 && strings.IndexFunc(id, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._", r))
	}) < 0
}

func validTransactionID(id string) bool {
	number, err := strconv.ParseUint(id, 10, 64)
	return err == nil && number > 0 && strconv.FormatUint(number, 10) == id
}

func validType(kind ProductType) bool {
	switch kind {
	case Consumable, NonConsumable, AutoRenewable, NonRenewing, UnknownProductType:
		return true
	}
	return false
}

func validTransaction(t Transaction) bool {
	return validTransactionID(t.ID) && validTransactionID(t.OriginalID) && validProduct(t.ProductID) &&
		validType(t.Type) && t.PurchasedAt > 0 && t.ExpiresAt >= 0 && t.RevokedAt >= 0
}
