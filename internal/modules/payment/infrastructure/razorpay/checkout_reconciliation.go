package razorpay

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
)

func (g *Gateway) FindCheckout(ctx context.Context, order *domain.Order) (*domain.CheckoutSession, *domain.PaymentConfirmation, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	receipt := formatReceipt(order.ID)
	result, err := g.client.Order.All(map[string]interface{}{"receipt": receipt, "count": 2}, nil)
	if err != nil {
		return nil, nil, err
	}
	items, _ := result["items"].([]interface{})
	found := false
	for _, item := range items {
		if candidate, ok := item.(map[string]interface{}); ok && candidate["receipt"] == receipt {
			found = true
			break
		}
	}
	// Some accounts return an empty result for the receipt filter. Search a
	// bounded creation window and verify the receipt locally before attaching.
	if !found && !order.CreatedAt.IsZero() {
		for page := 0; page < 5 && !found; page++ {
			result, err := g.client.Order.All(map[string]interface{}{
				"from":  order.CreatedAt.Add(-time.Minute).Unix(),
				"to":    order.ExpiresAt.Add(time.Minute).Unix(),
				"count": 100, "skip": page * 100,
			}, nil)
			if err != nil {
				return nil, nil, err
			}
			pageItems, _ := result["items"].([]interface{})
			for _, item := range pageItems {
				if candidate, ok := item.(map[string]interface{}); ok && candidate["receipt"] == receipt {
					items = []interface{}{candidate}
					found = true
					break
				}
			}
			if len(pageItems) < 100 {
				break
			}
		}
	}
	for _, item := range items {
		if candidate, ok := item.(map[string]interface{}); ok && candidate["receipt"] == receipt {
			return g.reconcileOrder(order, candidate)
		}
	}
	return nil, nil, nil
}

func (g *Gateway) FindCheckoutByReference(ctx context.Context, order *domain.Order, reference string) (*domain.CheckoutSession, *domain.PaymentConfirmation, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if !strings.HasPrefix(reference, "order_") || strings.ContainsAny(reference, "/?#") {
		return nil, nil, domain.ErrProviderMismatch
	}
	candidate, err := g.client.Order.Fetch(reference, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	if candidate["id"] != reference || candidate["receipt"] != formatReceipt(order.ID) {
		return nil, nil, domain.ErrProviderMismatch
	}
	return g.reconcileOrder(order, candidate)
}

func (g *Gateway) reconcileOrder(order *domain.Order, candidate map[string]interface{}) (*domain.CheckoutSession, *domain.PaymentConfirmation, error) {
	id := stringFromAny(candidate["id"])
	amount, _ := candidate["amount"].(float64)
	if id == "" || amount != float64(order.Amount) || !strings.EqualFold(stringFromAny(candidate["currency"]), order.Currency) {
		return nil, nil, domain.ErrAmountMismatch
	}
	switch candidate["status"] {
	case "created", "attempted":
		return &domain.CheckoutSession{ProviderRef: id}, nil, nil
	case "paid":
		payments, err := g.client.Order.Payments(id, nil, nil)
		if err != nil {
			return nil, nil, err
		}
		rows, _ := payments["items"].([]interface{})
		for _, row := range rows {
			payment, ok := row.(map[string]interface{})
			if !ok || payment["status"] != "captured" || payment["order_id"] != id {
				continue
			}
			paymentID := stringFromAny(payment["id"])
			paid, _ := payment["amount"].(float64)
			currency := stringFromAny(payment["currency"])
			if paymentID != "" && paid == float64(order.Amount) && strings.EqualFold(currency, order.Currency) {
				return nil, &domain.PaymentConfirmation{
					OrderID: order.ID, ProviderCheckoutID: id, ProviderPaymentID: paymentID,
					ReportedAmountMinor: int64(paid), ReportedCurrency: currency,
				}, nil
			}
		}
		return nil, nil, errors.New("paid provider order has no matching captured payment")
	}
	return nil, nil, nil
}
