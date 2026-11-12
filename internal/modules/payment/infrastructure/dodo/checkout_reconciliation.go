package dodo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
)

func (g *Gateway) FindCheckout(ctx context.Context, order *domain.Order) (*domain.CheckoutSession, *domain.PaymentConfirmation, error) {
	// The provider exposes checkout status by session ID, but its list API
	// only exposes payments. Correlate captured payments by signed account data.
	apiKey := strings.TrimSpace(g.cfg.APIKey)
	if apiKey == "" {
		return nil, nil, fmt.Errorf("dodo payments is not configured")
	}
	baseURL := strings.TrimRight(g.cfg.APIURL, "/")
	if baseURL == "" {
		baseURL = "https://test.dodopayments.com"
	}
	query := url.Values{
		"status": {"succeeded"}, "page_size": {"100"}, "currency": {order.Currency},
		"created_at_gte": {order.CreatedAt.UTC().Add(-time.Minute).Format(time.RFC3339)},
		"created_at_lte": {order.ExpiresAt.UTC().Add(24 * time.Hour).Format(time.RFC3339)},
	}
	if g.cfg.ProductID != "" {
		query.Set("product_id", g.cfg.ProductID)
	}
	for page := 0; page < 5; page++ {
		query.Set("page_number", strconv.Itoa(page))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/payments?"+query.Encode(), nil)
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		res, err := g.client.Do(req)
		if err != nil {
			return nil, nil, err
		}
		if res.StatusCode != http.StatusOK {
			res.Body.Close()
			return nil, nil, fmt.Errorf("dodo payment lookup returned status %d", res.StatusCode)
		}
		var result struct {
			Items []checkoutPayment `json:"items"`
		}
		err = json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&result)
		res.Body.Close()
		if err != nil {
			return nil, nil, err
		}
		for _, payment := range result.Items {
			if payment.Metadata["order_id"] == order.ID.String() {
				return payment.confirm(order)
			}
		}
		if len(result.Items) < 100 {
			break
		}
	}
	return nil, nil, nil
}

type checkoutPayment struct {
	PaymentID  string            `json:"payment_id"`
	CheckoutID string            `json:"checkout_session_id"`
	Amount     int64             `json:"total_amount"`
	Tax        int64             `json:"tax"`
	Currency   string            `json:"currency"`
	Status     string            `json:"status"`
	Metadata   map[string]string `json:"metadata"`
}

func (payment checkoutPayment) confirm(order *domain.Order) (*domain.CheckoutSession, *domain.PaymentConfirmation, error) {
	if payment.Status != "succeeded" || payment.PaymentID == "" {
		return nil, nil, nil
	}
	if payment.Tax < 0 || payment.Amount-payment.Tax != int64(order.Amount) || !strings.EqualFold(payment.Currency, order.Currency) {
		return nil, nil, domain.ErrAmountMismatch
	}
	return nil, &domain.PaymentConfirmation{
		OrderID: order.ID, ProviderCheckoutID: payment.CheckoutID,
		ProviderPaymentID: payment.PaymentID, ReportedAmountMinor: payment.Amount,
		ReportedTaxMinor: payment.Tax, ReportedCurrency: payment.Currency,
	}, nil
}

func (g *Gateway) FindCheckoutByReference(ctx context.Context, order *domain.Order, reference string) (*domain.CheckoutSession, *domain.PaymentConfirmation, error) {
	if !strings.HasPrefix(reference, "pay_") || strings.ContainsAny(reference, "/?#") {
		return nil, nil, domain.ErrProviderMismatch
	}
	key := strings.TrimSpace(g.cfg.APIKey)
	if key == "" {
		return nil, nil, fmt.Errorf("dodo payments is not configured")
	}
	baseURL := strings.TrimRight(g.cfg.APIURL, "/")
	if baseURL == "" {
		baseURL = "https://test.dodopayments.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/payments/"+url.PathEscape(reference), nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := g.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("dodo payment lookup returned status %d", res.StatusCode)
	}
	var payment checkoutPayment
	if err := json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&payment); err != nil {
		return nil, nil, err
	}
	if payment.PaymentID != reference || payment.Metadata["order_id"] != order.ID.String() {
		return nil, nil, domain.ErrProviderMismatch
	}
	return payment.confirm(order)
}
