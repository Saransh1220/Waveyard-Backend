package domain

import (
	"testing"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/shared/money"
	"github.com/stretchr/testify/require"
)

func TestCheckoutQuoteRoundsOnceAndEnforcesCheckoutFloor(t *testing.T) {
	priceID, specID, optionID := uuid.New(), uuid.New(), uuid.New()
	offer := money.SelectedPriceOffer{
		PriceID: &priceID, Revision: 1, AmountMinor: 1005, Currency: "USD", IsPurchasable: true,
	}
	coupon := &CheckoutCoupon{ID: uuid.New(), Revision: 2}
	percent, err := NewCheckoutQuote(specID, optionID, "USD", offer, coupon, 1000, 0)
	require.NoError(t, err)
	require.Equal(t, 101, percent.DiscountMinor)
	require.Equal(t, 904, percent.PayableMinor)

	fixed, err := NewCheckoutQuote(specID, optionID, "USD", offer, coupon, 0, 955)
	require.NoError(t, err)
	require.Equal(t, 50, fixed.PayableMinor)
	require.NotEqual(t, percent.ID, fixed.ID)

	regular, err := NewCheckoutQuote(specID, optionID, "USD", offer, nil, 0, 0)
	require.NoError(t, err)
	for _, savings := range []int{956, 1005} {
		aboveLimit, err := NewCheckoutQuote(specID, optionID, "USD", offer, coupon, 0, savings)
		require.NoError(t, err)
		require.Equal(t, regular, aboveLimit)
	}

	roundedToZero, err := NewCheckoutQuote(specID, optionID, "USD", offer, coupon, 1, 0)
	require.NoError(t, err)
	require.Zero(t, roundedToZero.DiscountMinor)
	require.Nil(t, roundedToZero.Coupon)
	require.Equal(t, offer.AmountMinor, roundedToZero.PayableMinor)
}

func TestCheckoutQuoteStillRejectsBasePriceBelowFloor(t *testing.T) {
	priceID := uuid.New()
	offer := money.SelectedPriceOffer{
		PriceID: &priceID, Revision: 1, AmountMinor: 99, Currency: "INR", IsPurchasable: true,
	}
	_, err := NewCheckoutQuote(uuid.New(), uuid.New(), "INR", offer, nil, 0, 0)
	require.ErrorIs(t, err, money.ErrPriceUnavailable)

	offer.AmountMinor = 1000
	coupon := &CheckoutCoupon{ID: uuid.New(), Revision: 2}
	exactFloor, err := NewCheckoutQuote(uuid.New(), uuid.New(), "INR", offer, coupon, 9000, 0)
	require.NoError(t, err)
	require.Equal(t, 100, exactFloor.PayableMinor)

	belowFloor, err := NewCheckoutQuote(uuid.New(), uuid.New(), "INR", offer, coupon, 9500, 0)
	require.NoError(t, err)
	require.Equal(t, 1000, belowFloor.PayableMinor)
	require.Zero(t, belowFloor.DiscountMinor)
	require.Zero(t, belowFloor.DiscountPercentBPS)
	require.Nil(t, belowFloor.Coupon)
}
