package http

import (
	"testing"

	"github.com/google/uuid"
	promotion "github.com/saransh1220/blueprint-audio/internal/modules/promotion/domain"
	"github.com/saransh1220/blueprint-audio/internal/shared/money"
	"github.com/stretchr/testify/require"
)

func TestCatalogDiscountHonorsLicenseAndStartingPrice(t *testing.T) {
	priceID, wavID := uuid.New(), uuid.New()
	mp3 := &money.SelectedPriceOffer{PriceID: &priceID, Currency: "USD", AmountMinor: 800, IsPurchasable: true}
	wav := &money.SelectedPriceOffer{PriceID: &priceID, Currency: "USD", AmountMinor: 1000, IsPurchasable: true}
	for _, tc := range []struct {
		name        string
		basisPoints int
		fixedMinor  int
		starting    bool
		net         int
	}{
		{name: "discounted WAV becomes the starting price", basisPoints: 2500, starting: true, net: 750},
		{name: "cheaper MP3 remains the starting price", fixedMinor: 100, net: 900},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := SpecResponse{
				SelectedPrice: mp3,
				Licenses:      []LicenseResponse{{ID: uuid.New(), SelectedPrice: mp3}, {ID: wavID, SelectedPrice: wav}},
			}
			spec.attachCheckoutCoupons([]promotion.CheckoutCoupon{{
				Rules: promotion.Rules{
					Currencies: []string{"USD"}, PercentBPS: tc.basisPoints, FixedMinor: map[string]int{"USD": tc.fixedMinor},
				},
				Enrollment: promotion.Enrollment{OptionIDs: []uuid.UUID{wavID}},
			}})
			require.Nil(t, spec.Licenses[0].Discount)
			require.NotNil(t, spec.Licenses[1].Discount)
			require.Equal(t, tc.net, spec.Licenses[1].Discount.PayableMinor)
			require.NotNil(t, spec.Promotion)
			require.True(t, spec.Promotion.SelectLicenses)
			require.Equal(t, tc.starting, spec.Promotion.StartingPrice)
			require.Same(t, mp3, spec.SelectedPrice)
		})
	}
}
