package http

import (
	"context"
	"log"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/catalog/domain"
	promotion "github.com/saransh1220/blueprint-audio/internal/modules/promotion/domain"
	"github.com/saransh1220/blueprint-audio/internal/shared/money"
)

type SpecPromotion struct {
	Discount       *money.CheckoutDiscount `json:"discount"`
	StartingPrice  bool                    `json:"starting_price"`
	SelectLicenses bool                    `json:"select_licenses"`
}

func (h *SpecHandler) WithCheckoutCoupons(reader promotion.CheckoutCouponReader) {
	h.checkoutCoupons = reader
}

func (h *SpecHandler) attachListCheckoutCoupons(ctx context.Context, responses []SpecResponse) {
	if h.checkoutCoupons == nil {
		return
	}
	specs := make([]*SpecResponse, len(responses))
	for i := range responses {
		specs[i] = &responses[i]
	}
	h.attachCheckoutCoupons(ctx, specs)
}

// Attach current offers after serialization caches and batch the lookup for listings.
func (h *SpecHandler) attachCheckoutCoupons(ctx context.Context, responses []*SpecResponse) {
	if h.checkoutCoupons == nil {
		return
	}
	beats := make(map[uuid.UUID][]*SpecResponse)
	ids := make([]uuid.UUID, 0, len(responses))
	for _, spec := range responses {
		if spec.Category != string(domain.CategoryBeat) || spec.ProcessingStatus != string(domain.ProcessingStatusCompleted) {
			continue
		}
		if _, seen := beats[spec.ID]; !seen {
			ids = append(ids, spec.ID)
		}
		beats[spec.ID] = append(beats[spec.ID], spec)
	}
	if len(ids) == 0 {
		return
	}
	coupons, err := h.checkoutCoupons.ReadActiveCoupons(ctx, ids)
	if err != nil {
		log.Printf("[SpecHandler] checkout coupons unavailable: %v", err)
		return
	}
	for id, matches := range coupons {
		for _, spec := range beats[id] {
			spec.attachCheckoutCoupons(matches)
		}
	}
}

func (spec *SpecResponse) attachCheckoutCoupons(coupons []promotion.CheckoutCoupon) {
	if spec.SelectedPrice == nil {
		return
	}
	currency := spec.SelectedPrice.Currency
	var deal *money.CheckoutDiscount
	var fallbackDeal *money.CheckoutDiscount
	var purchasable, discounted int
	lowest := spec.SelectedPrice.AmountMinor
	// Keep the same currency preference as the regular catalog starting price.
	for i := range spec.Licenses {
		license := &spec.Licenses[i]
		offer := license.SelectedPrice
		if offer == nil || !offer.IsPurchasable {
			continue
		}
		purchasable++
		coupon, err := promotion.MatchCheckoutCoupon(coupons, license.ID, offer.Currency)
		if err != nil || coupon == nil {
			continue
		}
		price, err := money.CheckoutAmounts(*offer, coupon.Rules.PercentBPS, coupon.Rules.FixedMinor[offer.Currency])
		if err != nil || price.DiscountMinor == 0 {
			continue
		}
		license.Discount = &price
		if offer.Currency != currency {
			if fallbackDeal == nil {
				fallbackDeal = &price
			}
			continue
		}
		discounted++
		if deal == nil || price.PayableMinor < deal.PayableMinor {
			deal = &price
		}
	}
	if deal != nil {
		spec.Promotion = &SpecPromotion{
			Discount: deal, StartingPrice: deal.PayableMinor <= lowest,
			SelectLicenses: discounted < purchasable,
		}
	} else if fallbackDeal != nil {
		spec.Promotion = &SpecPromotion{Discount: fallbackDeal, SelectLicenses: true}
	}
}
