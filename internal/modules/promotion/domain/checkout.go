package domain

import (
	"context"
	"slices"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/shared/money"
)

type CheckoutCoupon struct {
	ID         uuid.UUID
	Revision   int64
	Rules      Rules
	Enrollment Enrollment
}

type CheckoutCouponReader interface {
	ReadActiveCoupons(context.Context, []uuid.UUID) (map[uuid.UUID][]CheckoutCoupon, error)
}

func MatchCheckoutCoupon(coupons []CheckoutCoupon, optionID uuid.UUID, currency string) (*CheckoutCoupon, error) {
	var match *CheckoutCoupon
	for i := range coupons {
		coupon := &coupons[i]
		if !slices.Contains(coupon.Rules.Currencies, currency) ||
			(!coupon.Enrollment.AllPaid && !slices.Contains(coupon.Enrollment.OptionIDs, optionID)) {
			continue
		}
		if match != nil {
			return nil, money.ErrPriceUnavailable
		}
		match = coupon
	}
	return match, nil
}
