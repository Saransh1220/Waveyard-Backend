package application

import (
	"time"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
	"github.com/saransh1220/blueprint-audio/internal/shared/money"
)

type CreateOrderInput struct {
	UserID              uuid.UUID  `json:"user_id"`
	SpecID              uuid.UUID  `json:"spec_id"`
	LicenseOptionID     uuid.UUID  `json:"license_option_id"`
	Currency            string     `json:"currency"`
	ExpectedPriceID     *uuid.UUID `json:"expected_price_id,omitempty"`
	ExpectedRevision    *int64     `json:"expected_revision,omitempty"`
	ExpectedAmountMinor *int       `json:"expected_amount_minor,omitempty"`
	ExpectedCurrency    *string    `json:"expected_currency,omitempty"`
	IdempotencyKey      string     `json:"idempotency_key,omitempty"`
}

type PriceMismatchError struct {
	Message      string                   `json:"message"`
	CurrentOffer money.SelectedPriceOffer `json:"current_offer"`
}

func (e *PriceMismatchError) Error() string {
	return e.Message
}

type LicenseDownloadsResponse struct {
	LicenseID   string  `json:"license_id"`
	LicenseType string  `json:"license_type"`
	SpecTitle   string  `json:"spec_title"`
	MP3URL      *string `json:"mp3_url,omitempty"`
	WAVURL      *string `json:"wav_url,omitempty"`
	StemsURL    *string `json:"stems_url,omitempty"`
	ExpiresIn   int     `json:"expires_in"` // Standardize on seconds
}

type ProducerOrderDto struct {
	ID              uuid.UUID          `json:"id"`
	Amount          float64            `json:"amount"`
	Currency        string             `json:"currency"`
	Status          domain.OrderStatus `json:"status"`
	CreatedAt       time.Time          `json:"created_at"`
	LicenseType     string             `json:"license_type"`
	BuyerName       string             `json:"buyer_name"`
	BuyerEmail      string             `json:"buyer_email"`
	SpecTitle       string             `json:"spec_title"`
	Provider        string             `json:"provider"`
	RazorpayOrderID *string            `json:"razorpay_order_id,omitempty"`
}

type ProducerOrderResponse struct {
	Orders []ProducerOrderDto `json:"orders"`
	Total  int                `json:"total"`
	Limit  int                `json:"limit"`
	Offset int                `json:"offset"`
}

type OrderLicenseSummary struct {
	ID         uuid.UUID `json:"id"`
	LicenseKey string    `json:"license_key,omitempty"`
}

type OrderStatusResponse struct {
	OrderID           uuid.UUID            `json:"order_id"`
	Status            domain.OrderStatus   `json:"status"`
	Provider          string               `json:"provider"`
	ExpiresAt         time.Time            `json:"expires_at"`
	License           *OrderLicenseSummary `json:"license"`
	RetryAfterSeconds int                  `json:"retry_after_seconds,omitempty"`
}

type CreateRefundInput struct {
	OrderID        uuid.UUID `json:"order_id"`
	AdminID        uuid.UUID `json:"admin_id"`
	IdempotencyKey string    `json:"idempotency_key"`
	Reason         string    `json:"reason"`
	Notes          string    `json:"notes,omitempty"`
}

type OrderRefundResponse struct {
	ID               uuid.UUID `json:"id"`
	OrderID          uuid.UUID `json:"order_id"`
	PaymentID        uuid.UUID `json:"payment_id"`
	Amount           int       `json:"amount"`
	Currency         string    `json:"currency"`
	Status           string    `json:"status"`
	Reason           string    `json:"reason"`
	ProviderRefundID *string   `json:"provider_refund_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}
