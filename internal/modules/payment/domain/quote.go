package domain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/shared/money"
)

type QuoteReader interface {
	ReadQuote(context.Context, uuid.UUID, uuid.UUID, string) (CheckoutQuote, error)
}

type CheckoutCoupon struct {
	ID       uuid.UUID `json:"id" db:"id"`
	Revision int64     `json:"revision" db:"revision"`
}

type CheckoutQuote struct {
	ID                 string                   `json:"quote_id"`
	SpecID             uuid.UUID                `json:"spec_id"`
	LicenseOptionID    uuid.UUID                `json:"license_option_id"`
	RequestedCurrency  string                   `json:"requested_currency"`
	Offer              money.SelectedPriceOffer `json:"offer"`
	DiscountMinor      int                      `json:"discount_minor"`
	PayableMinor       int                      `json:"payable_minor"`
	Coupon             *CheckoutCoupon          `json:"coupon,omitempty"`
	DiscountPercentBPS int                      `json:"discount_percent_bps,omitempty"`
}

func NewCheckoutQuote(specID, optionID uuid.UUID, currency string, offer money.SelectedPriceOffer, coupon *CheckoutCoupon, basisPoints, fixedMinor int) (CheckoutQuote, error) {
	price, err := money.CheckoutAmounts(offer, basisPoints, fixedMinor)
	if err != nil {
		return CheckoutQuote{}, err
	}
	if price.DiscountMinor == 0 {
		coupon = nil
	}
	quote := CheckoutQuote{
		SpecID: specID, LicenseOptionID: optionID, RequestedCurrency: currency,
		Offer: offer, DiscountMinor: price.DiscountMinor, PayableMinor: price.PayableMinor, Coupon: coupon,
	}
	payload, err := json.Marshal(quote)
	if err != nil {
		return CheckoutQuote{}, err
	}
	hash := sha256.Sum256(payload)
	quote.ID = hex.EncodeToString(hash[:])
	// Display metadata does not change the fingerprint of existing confirmations.
	if coupon != nil {
		quote.DiscountPercentBPS = price.PercentBPS
	}
	return quote, nil
}
