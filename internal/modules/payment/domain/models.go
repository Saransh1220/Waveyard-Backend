package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type OrderStatus string

const (
	OrderStatusPending    OrderStatus = "pending"
	OrderStatusProcessing OrderStatus = "processing"
	OrderStatusPaid       OrderStatus = "paid"
	OrderStatusFailed     OrderStatus = "failed"
	OrderStatusCancelled  OrderStatus = "cancelled"
	OrderStatusRefunded   OrderStatus = "refunded"
)

type PaymentStatus string

const (
	PaymentStatusPending  PaymentStatus = "pending"
	PaymentStatusCaptured PaymentStatus = "captured"
	PaymentStatusFailed   PaymentStatus = "failed"
	PaymentStatusRefunded PaymentStatus = "refunded"
)

const (
	CheckoutStateReserved    = "reserved"
	CheckoutStateClaiming    = "claiming"
	CheckoutStateReady       = "ready"
	CheckoutStateFailed      = "failed"
	CheckoutStateReconciling = "reconciling"
)

const (
	WebhookEventStatusQueued      = "queued"
	WebhookEventStatusProcessing  = "processing"
	WebhookEventStatusCompleted   = "completed"
	WebhookEventStatusFailed      = "failed"
	WebhookEventStatusNeedsReview = "needs_review"
	WebhookEventStatusIgnored     = "ignored"
)

const (
	RefundStatusRequested   = "requested"
	RefundStatusSubmitting  = "submitting"
	RefundStatusPending     = "pending"
	RefundStatusSucceeded   = "succeeded"
	RefundStatusFailed      = "failed"
	RefundStatusNeedsReview = "needs_review"
)

type Order struct {
	ID                 uuid.UUID      `json:"id" db:"id"`
	UserID             uuid.UUID      `json:"user_id" db:"user_id"`
	SpecID             uuid.UUID      `json:"spec_id" db:"spec_id"`
	LicenseType        string         `json:"license_type" db:"license_type"`
	Amount             int            `json:"amount" db:"amount"`
	Currency           string         `json:"currency" db:"currency"`
	RazorpayOrderID    *string        `json:"razorpay_order_id,omitempty" db:"-"`
	Provider           string         `json:"provider" db:"provider"`
	ProviderCheckoutID *string        `json:"provider_checkout_id,omitempty" db:"provider_checkout_id"`
	ProviderPaymentID  *string        `json:"provider_payment_id,omitempty" db:"provider_payment_id"`
	CheckoutURL        *string        `json:"checkout_url,omitempty" db:"checkout_url"`
	Status             OrderStatus    `json:"status" db:"status"`
	Notes              map[string]any `json:"notes,omitempty" db:"notes"`
	ReportCurrency     *string        `json:"report_currency,omitempty" db:"report_currency"`
	ReportFXRate       *string        `json:"report_fx_rate,omitempty" db:"report_fx_rate"`
	ReportFXAsOf       *time.Time     `json:"report_fx_as_of,omitempty" db:"report_fx_as_of"`
	ReportFXFetchedAt  *time.Time     `json:"report_fx_fetched_at,omitempty" db:"report_fx_fetched_at"`
	ReportFXRateID     *int64         `json:"report_fx_rate_id,omitempty" db:"report_fx_rate_id"`
	CreatedAt          time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at" db:"updated_at"`
	ExpiresAt          time.Time      `json:"expires_at" db:"expires_at"`
}

type OrderFXSnapshot struct {
	ReportCurrency    string
	ReportFXRate      *string
	ReportFXRateID    *int64
	ReportFXAsOf      *time.Time
	ReportFXFetchedAt *time.Time
}

type ReportingRateSnapshotter interface {
	Snapshot(ctx context.Context, orderCurrency string) (*OrderFXSnapshot, error)
}

// FillClientFields sets the derived fields the frontend expects.
func (o *Order) FillClientFields() {
	o.RazorpayOrderID = nil
	if o.Provider == ProviderRazorpay && o.ProviderCheckoutID != nil && *o.ProviderCheckoutID != "" {
		id := *o.ProviderCheckoutID
		o.RazorpayOrderID = &id
	}
}

type OrderWithBuyer struct {
	Order
	BuyerName  string `json:"buyer_name" db:"buyer_name"`
	BuyerEmail string `json:"buyer_email" db:"buyer_email"`
	SpecTitle  string `json:"spec_title" db:"spec_title"`
}

type Payment struct {
	ID                uuid.UUID     `json:"id" db:"id"`
	OrderID           uuid.UUID     `json:"order_id" db:"order_id"`
	Provider          string        `json:"provider" db:"provider"`
	ProviderPaymentID string        `json:"provider_payment_id" db:"provider_payment_id"`
	ProviderSignature string        `json:"provider_signature" db:"provider_signature"`
	Amount            int           `json:"amount" db:"amount"`
	Currency          string        `json:"currency" db:"currency"`
	Status            PaymentStatus `json:"status" db:"status"`
	Method            *string       `json:"method,omitempty" db:"method"`
	Bank              *string       `json:"bank,omitempty" db:"bank"`
	Wallet            *string       `json:"wallet,omitempty" db:"wallet"`
	VPA               *string       `json:"vpa,omitempty" db:"vpa"`
	CardNetwork       *string       `json:"card_network,omitempty" db:"card_network"`
	CardLast4         *string       `json:"card_last4,omitempty" db:"card_last4"`
	Email             *string       `json:"email,omitempty" db:"email"`
	Contact           *string       `json:"contact,omitempty" db:"contact"`
	ErrorCode         *string       `json:"error_code,omitempty" db:"error_code"`
	ErrorDescription  *string       `json:"error_description,omitempty" db:"error_description"`
	CapturedAt        *time.Time    `json:"captured_at,omitempty" db:"captured_at"`
	CreatedAt         time.Time     `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time     `json:"updated_at" db:"updated_at"`
}

type License struct {
	ID               uuid.UUID  `json:"id" db:"id"`
	OrderID          uuid.UUID  `json:"order_id" db:"order_id"`
	UserID           uuid.UUID  `json:"user_id" db:"user_id"`
	SpecID           uuid.UUID  `json:"spec_id" db:"spec_id"`
	LicenseOptionID  uuid.UUID  `json:"license_option_id" db:"license_option_id"`
	LicenseType      string     `json:"license_type" db:"license_type"`
	PurchasePrice    int        `json:"purchase_price" db:"purchase_price"`
	Currency         string     `json:"currency" db:"currency"`
	LicenseKey       string     `json:"license_key" db:"license_key"`
	IsActive         bool       `json:"is_active" db:"is_active"`
	IsRevoked        bool       `json:"is_revoked" db:"is_revoked"`
	RevokedReason    *string    `json:"revoked_reason,omitempty" db:"revoked_reason"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty" db:"revoked_at"`
	DownloadsCount   int        `json:"downloads_count" db:"downloads_count"`
	LastDownloadedAt *time.Time `json:"last_downloaded_at,omitempty" db:"last_downloaded_at"`
	IssuedAt         time.Time  `json:"issued_at" db:"issued_at"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at" db:"updated_at"`

	// Joined fields
	SpecTitle string  `json:"spec_title" db:"spec_title"`
	SpecImage *string `json:"spec_image" db:"spec_image"`
}

// OrderIdempotencyKey represents a buyer's intention to initiate checkout
type OrderIdempotencyKey struct {
	ID                 uuid.UUID  `json:"id" db:"id"`
	UserID             uuid.UUID  `json:"user_id" db:"user_id"`
	IdempotencyKey     string     `json:"idempotency_key" db:"idempotency_key"`
	RequestHash        string     `json:"request_hash" db:"request_hash"`
	OrderID            *uuid.UUID `json:"order_id,omitempty" db:"order_id"`
	CheckoutState      string     `json:"checkout_state" db:"checkout_state"`
	Provider           string     `json:"provider" db:"provider"`
	ProviderCheckoutID *string    `json:"provider_checkout_id,omitempty" db:"provider_checkout_id"`
	CheckoutURL        *string    `json:"checkout_url,omitempty" db:"checkout_url"`
	AttemptGeneration  int        `json:"attempt_generation" db:"attempt_generation"`
	LeaseExpiresAt     *time.Time `json:"lease_expires_at,omitempty" db:"lease_expires_at"`
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at" db:"updated_at"`
}

// PaymentWebhookEvent records an incoming webhook envelope in the durable inbox
type PaymentWebhookEvent struct {
	ID                 uuid.UUID         `json:"id" db:"id"`
	Provider           string            `json:"provider" db:"provider"`
	EventID            string            `json:"event_id" db:"event_id"`
	EventType          string            `json:"event_type" db:"event_type"`
	OrderID            *uuid.UUID        `json:"order_id,omitempty" db:"order_id"`
	ProviderCheckoutID *string           `json:"provider_checkout_id,omitempty" db:"provider_checkout_id"`
	ProviderPaymentID  *string           `json:"provider_payment_id,omitempty" db:"provider_payment_id"`
	ReportedAmount     *int              `json:"reported_amount,omitempty" db:"reported_amount"`
	ReportedCurrency   *string           `json:"reported_currency,omitempty" db:"reported_currency"`
	Payload            []byte            `json:"payload" db:"payload"`
	Headers            map[string]string `json:"headers,omitempty" db:"headers"`
	Status             string            `json:"status" db:"status"`
	Attempts           int               `json:"attempts" db:"attempts"`
	MaxAttempts        int               `json:"max_attempts" db:"max_attempts"`
	LastError          *string           `json:"last_error,omitempty" db:"last_error"`
	WorkerID           *string           `json:"worker_id,omitempty" db:"worker_id"`
	LockedAt           *time.Time        `json:"locked_at,omitempty" db:"locked_at"`
	LeaseExpiresAt     *time.Time        `json:"lease_expires_at,omitempty" db:"lease_expires_at"`
	NextRetryAt        time.Time         `json:"next_retry_at" db:"next_retry_at"`
	CreatedAt          time.Time         `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at" db:"updated_at"`
}

type WebhookEventFilter struct {
	Provider string
	Status   string
	OrderID  *uuid.UUID
}

// OrderRefund records an admin full refund operation
type OrderRefund struct {
	ID                uuid.UUID  `json:"id" db:"id"`
	OrderID           uuid.UUID  `json:"order_id" db:"order_id"`
	PaymentID         uuid.UUID  `json:"payment_id" db:"payment_id"`
	UserID            uuid.UUID  `json:"user_id" db:"user_id"`
	AdminID           uuid.UUID  `json:"admin_id" db:"admin_id"`
	IdempotencyKey    string     `json:"idempotency_key" db:"idempotency_key"`
	Amount            int        `json:"amount" db:"amount"`
	Currency          string     `json:"currency" db:"currency"`
	Provider          string     `json:"provider" db:"provider"`
	ProviderRefundID  *string    `json:"provider_refund_id,omitempty" db:"provider_refund_id"`
	ProviderPaymentID string     `json:"provider_payment_id" db:"provider_payment_id"`
	Reason            string     `json:"reason" db:"reason"`
	Notes             *string    `json:"notes,omitempty" db:"notes"`
	Status            string     `json:"status" db:"status"`
	FailureReason     *string    `json:"failure_reason,omitempty" db:"failure_reason"`
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at" db:"updated_at"`
}

// Repositories

type OrderRepository interface {
	Create(ctx context.Context, order *Order) error
	GetByID(ctx context.Context, id uuid.UUID) (*Order, error)
	GetByProviderCheckoutID(ctx context.Context, provider, checkoutID string) (*Order, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status OrderStatus) error
	AttachCheckoutDetails(ctx context.Context, id uuid.UUID, providerCheckoutID, checkoutURL string) error
	MarkPaidIfPayable(ctx context.Context, id uuid.UUID, providerPaymentID string) (bool, error)
	MarkRefunded(ctx context.Context, id uuid.UUID) error
	ListByUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]Order, error)
	ListByProducer(ctx context.Context, producerID uuid.UUID, limit, offset int) ([]OrderWithBuyer, int, error)
}

type PaymentRepository interface {
	Create(ctx context.Context, payment *Payment) error
	GetByID(ctx context.Context, id uuid.UUID) (*Payment, error)
	GetByOrderID(ctx context.Context, orderID uuid.UUID) (*Payment, error)
	GetByProviderPaymentID(ctx context.Context, provider, providerPaymentID string) (*Payment, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status PaymentStatus) error
}

type LicenseRepository interface {
	Create(ctx context.Context, license *License) error
	GetByID(ctx context.Context, id uuid.UUID) (*License, error)
	GetByOrderID(ctx context.Context, orderID uuid.UUID) (*License, error)
	ListByUser(ctx context.Context, userID uuid.UUID, limit, offset int, search, licenseType string) ([]License, int, error)
	IncrementDownloads(ctx context.Context, id uuid.UUID) error
	Revoke(ctx context.Context, id uuid.UUID, reason string) error
}

type IdempotencyRepository interface {
	ReserveKey(ctx context.Context, key *OrderIdempotencyKey) (existing *OrderIdempotencyKey, created bool, err error)
	GetByUserAndKey(ctx context.Context, userID uuid.UUID, key string) (*OrderIdempotencyKey, error)
	Claim(ctx context.Context, id uuid.UUID, currentGen int, leaseDuration time.Duration) (bool, int, error)
	AttachCheckoutResult(ctx context.Context, id uuid.UUID, gen int, providerCheckoutID, checkoutURL, state string) error
	MarkFailed(ctx context.Context, id uuid.UUID, gen int, state string) error
}

type WebhookEventRepository interface {
	InsertEvent(ctx context.Context, event *PaymentWebhookEvent) (existing *PaymentWebhookEvent, inserted bool, err error)
	GetByID(ctx context.Context, id uuid.UUID) (*PaymentWebhookEvent, error)
	ClaimNextBatch(ctx context.Context, workerID string, batchSize int, leaseDuration time.Duration) ([]PaymentWebhookEvent, error)
	MarkCompleted(ctx context.Context, id uuid.UUID) error
	MarkFailed(ctx context.Context, id uuid.UUID, lastErr string, retryAfter time.Duration, status string) error
	ListEvents(ctx context.Context, filter WebhookEventFilter, limit, offset int) ([]PaymentWebhookEvent, int, error)
	Requeue(ctx context.Context, id uuid.UUID) error
}

type RefundRepository interface {
	ReserveRefund(ctx context.Context, refund *OrderRefund) (existing *OrderRefund, created bool, err error)
	GetByID(ctx context.Context, id uuid.UUID) (*OrderRefund, error)
	GetByOrderID(ctx context.Context, orderID uuid.UUID) ([]OrderRefund, error)
	GetByIdempotencyKey(ctx context.Context, key string) (*OrderRefund, error)
	GetByProviderRefundID(ctx context.Context, provider, providerRefundID string) (*OrderRefund, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status string, providerRefundID *string, failureReason *string) error
}

type PaymentTx interface {
	OrderRepo() OrderRepository
	PaymentRepo() PaymentRepository
	LicenseRepo() LicenseRepository
	IdempotencyRepo() IdempotencyRepository
	RefundRepo() RefundRepository
}

type TxRunner interface {
	WithTx(ctx context.Context, fn func(tx PaymentTx) error) error
}
