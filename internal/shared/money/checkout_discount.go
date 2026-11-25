package money

// CheckoutDiscount describes an automatic saving on one purchasable license.
type CheckoutDiscount struct {
	Currency      string `json:"currency"`
	OriginalMinor int    `json:"original_minor"`
	DiscountMinor int    `json:"discount_minor"`
	PayableMinor  int    `json:"payable_minor"`
	PercentBPS    int    `json:"percent_bps,omitempty"`
}

func CheckoutAmounts(offer SelectedPriceOffer, basisPoints, fixedMinor int) (CheckoutDiscount, error) {
	minimum := 50 // Conservative USD checkout floor.
	if offer.Currency == CurrencyINR {
		minimum = 100
	}
	if !offer.IsPurchasable || offer.PriceID == nil || offer.AmountMinor < minimum {
		return CheckoutDiscount{}, ErrPriceUnavailable
	}
	savings := max(0, fixedMinor)
	if basisPoints > 0 {
		savings = int((int64(offer.AmountMinor)*int64(basisPoints) + 5000) / 10000)
	}
	if savings > offer.AmountMinor-minimum {
		// The undiscounted offer is valid even when the coupon cannot be applied.
		savings, basisPoints = 0, 0
	}
	return CheckoutDiscount{
		Currency: offer.Currency, OriginalMinor: offer.AmountMinor, DiscountMinor: savings,
		PayableMinor: offer.AmountMinor - savings, PercentBPS: basisPoints,
	}, nil
}
