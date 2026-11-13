package domain

import (
	"context"

	"github.com/google/uuid"
)

// Names stored in orders.provider and payments.provider.
const (
	ProviderRazorpay = "razorpay"
	ProviderDodo     = "dodo"
)

// Gateway is the strategy every payment provider implements: how to start a
// checkout. How a payment is later confirmed differs per provider, so that
// lives in the optional ClientConfirmer, WebhookConfirmer, and Refunder interfaces.
type Gateway interface {
	Name() string
	CreateCheckout(ctx context.Context, req CheckoutRequest) (CheckoutSession, error)
}

// CheckoutFinder recovers an existing checkout or capture using provider reads.
type CheckoutFinder interface {
	FindCheckout(ctx context.Context, order *Order) (*CheckoutSession, *PaymentConfirmation, error)
}

// CheckoutReferenceFinder verifies an operator-supplied provider reference.
type CheckoutReferenceFinder interface {
	FindCheckoutByReference(ctx context.Context, order *Order, reference string) (*CheckoutSession, *PaymentConfirmation, error)
}

type CheckoutRequest struct {
	Order       *Order
	SpecTitle   string
	LicenseName string
}

type CheckoutSession struct {
	// ProviderRef is the gateway's id for this checkout (Razorpay order id,
	// Dodo checkout session id). It is stored in orders.provider_checkout_id.
	ProviderRef string
	// CheckoutURL is set by gateways that host the checkout page.
	CheckoutURL string
}

// PaymentConfirmation is the provider-neutral result of a successful payment.
type PaymentConfirmation struct {
	OrderID             uuid.UUID
	ProviderCheckoutID  string
	ProviderPaymentID   string
	ReportedAmountMinor int64
	ReportedTaxMinor    int64
	ReportedCurrency    string
	Signature           string
	Method              *string
	Email               *string
	EventID             string
	Refund              *RefundConfirmation
}

// RefundConfirmation contains facts from an authenticated provider response.
type RefundConfirmation struct {
	ProviderRefundID string
	Amount           int
	Currency         string
	Full             bool
	Status           string
	IntentKey        string
}

// ClientConfirmer is implemented by gateways where the browser reports the
// payment back to us (Razorpay: payment id plus signature).
type ClientConfirmer interface {
	ConfirmClientPayment(ctx context.Context, order *Order, providerPaymentID, signature string) (*PaymentConfirmation, error)
}

// WebhookConfirmer is implemented by gateways that call us when a payment
// completes (Dodo). A nil confirmation with a nil error means the event is
// valid but needs no action.
type WebhookConfirmer interface {
	ParseWebhook(payload []byte, headers map[string]string) (*PaymentConfirmation, error)
}

type RefundRequest struct {
	IdempotencyKey    string
	OrderID           uuid.UUID
	ProviderPaymentID string
	AmountMinor       int
	Currency          string
	Reason            string
}

type RefundResult struct {
	ProviderRefundID string
	Status           string // "succeeded", "pending", "failed"
	FailureReason    *string
}

// Refunder is implemented by gateways that support server-initiated refunds.
type Refunder interface {
	Refund(ctx context.Context, req RefundRequest) (*RefundResult, error)
}
