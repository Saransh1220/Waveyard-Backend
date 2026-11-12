// Package razorpay is the Razorpay implementation of the payment gateway
// strategy: server-side order creation, client-reported confirmation and webhook confirmation.
package razorpay

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	rzp "github.com/razorpay/razorpay-go"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
)

type Config struct {
	KeyID         string
	KeySecret     string
	WebhookSecret string
	// BaseURL overrides the Razorpay API root. Tests point it at httptest.
	BaseURL string
}

type Gateway struct {
	client        *rzp.Client
	secret        string
	webhookSecret string
}

var (
	_ domain.Gateway          = (*Gateway)(nil)
	_ domain.ClientConfirmer  = (*Gateway)(nil)
	_ domain.WebhookConfirmer = (*Gateway)(nil)
	_ domain.Refunder         = (*Gateway)(nil)
)

func NewGateway(cfg Config) *Gateway {
	client := rzp.NewClient(cfg.KeyID, cfg.KeySecret)
	if cfg.BaseURL != "" {
		client.Request.BaseURL = cfg.BaseURL
	}
	return &Gateway{
		client:        client,
		secret:        cfg.KeySecret,
		webhookSecret: cfg.WebhookSecret,
	}
}

func (g *Gateway) Name() string { return domain.ProviderRazorpay }

func (g *Gateway) CreateCheckout(_ context.Context, req domain.CheckoutRequest) (domain.CheckoutSession, error) {
	order, err := g.client.Order.Create(map[string]interface{}{
		"amount":   req.Order.Amount,
		"currency": req.Order.Currency,
		"receipt":  formatReceipt(req.Order.ID),
	}, nil)
	if err != nil {
		return domain.CheckoutSession{}, fmt.Errorf("razorpay order creation failed: %w", err)
	}
	id, ok := order["id"].(string)
	if !ok || id == "" {
		return domain.CheckoutSession{}, errors.New("invalid razorpay order response")
	}
	return domain.CheckoutSession{ProviderRef: id}, nil
}

func (g *Gateway) ConfirmClientPayment(_ context.Context, order *domain.Order, paymentID, signature string) (*domain.PaymentConfirmation, error) {
	if order.ProviderCheckoutID == nil || *order.ProviderCheckoutID == "" {
		return nil, errors.New("invalid order state")
	}
	if !hmac.Equal([]byte(g.sign(*order.ProviderCheckoutID, paymentID)), []byte(signature)) {
		return nil, domain.ErrInvalidSignature
	}

	payment, err := g.client.Payment.Fetch(paymentID, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch payment: %w", err)
	}
	status, _ := payment["status"].(string)
	if strings.ToLower(status) != "captured" {
		return nil, domain.ErrPaymentNotCaptured
	}

	c := &domain.PaymentConfirmation{
		OrderID:            order.ID,
		ProviderCheckoutID: *order.ProviderCheckoutID,
		ProviderPaymentID:  paymentID,
		Signature:          signature,
	}
	if amountVal, ok := payment["amount"]; ok && amountVal != nil {
		switch v := amountVal.(type) {
		case float64:
			c.ReportedAmountMinor = int64(v)
		case int64:
			c.ReportedAmountMinor = v
		case int:
			c.ReportedAmountMinor = int64(v)
		}
	}
	if currencyVal, ok := payment["currency"].(string); ok {
		c.ReportedCurrency = strings.ToUpper(strings.TrimSpace(currencyVal))
	}
	if method, ok := payment["method"].(string); ok {
		c.Method = &method
	}
	if email, ok := payment["email"].(string); ok {
		c.Email = &email
	}
	return c, nil
}

func (g *Gateway) ParseWebhook(payload []byte, headers map[string]string) (*domain.PaymentConfirmation, error) {
	secret := strings.TrimSpace(g.webhookSecret)
	if secret == "" {
		return nil, domain.ErrMissingWebhookSecret
	}

	signature := getHeader(headers, "x-razorpay-signature")
	if signature == "" {
		return nil, domain.ErrInvalidSignature
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(strings.TrimSpace(expectedSignature)), []byte(strings.TrimSpace(signature))) {
		return nil, domain.ErrInvalidSignature
	}

	var event struct {
		Event   string `json:"event"`
		Payload struct {
			Refund struct {
				Entity struct {
					ID        string          `json:"id"`
					PaymentID string          `json:"payment_id"`
					Amount    int             `json:"amount"`
					Currency  string          `json:"currency"`
					Notes     json.RawMessage `json:"notes"`
				} `json:"entity"`
			} `json:"refund"`
			Payment struct {
				Entity map[string]any `json:"entity"`
			} `json:"payment"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil, domain.ErrMalformedPayload
	}

	if event.Event == "refund.created" || event.Event == "refund.processed" || event.Event == "refund.failed" {
		refund := event.Payload.Refund.Entity
		if refund.ID == "" || refund.PaymentID == "" || refund.Amount <= 0 || refund.Amount > 1<<31-1 || refund.Currency == "" {
			return nil, domain.ErrMalformedPayload
		}
		status := domain.RefundStatusPending
		switch event.Event {
		case "refund.processed":
			status = domain.RefundStatusSucceeded
		case "refund.failed":
			status = domain.RefundStatusFailed
		}
		var notes map[string]string
		_ = json.Unmarshal(refund.Notes, &notes)
		return &domain.PaymentConfirmation{
			EventID: getHeader(headers, "x-razorpay-event-id"), ProviderPaymentID: refund.PaymentID,
			Refund: &domain.RefundConfirmation{
				ProviderRefundID: refund.ID, Amount: refund.Amount, Currency: refund.Currency,
				Status: status, IntentKey: notes["idempotency_key"],
			},
		}, nil
	}

	// Canonical event: payment.captured
	if event.Event != "payment.captured" {
		return nil, nil // valid event we do not act on
	}

	entity := event.Payload.Payment.Entity
	if len(entity) == 0 {
		return nil, domain.ErrMalformedPayload
	}

	paymentID := stringFromAny(entity["id"])
	if paymentID == "" {
		return nil, domain.ErrMissingPaymentID
	}

	providerOrderID := stringFromAny(entity["order_id"])
	if providerOrderID == "" {
		return nil, domain.ErrOrderNotFound
	}

	var amountMinor int64
	if amountVal, ok := entity["amount"]; ok && amountVal != nil {
		switch v := amountVal.(type) {
		case float64:
			amountMinor = int64(v)
		case int64:
			amountMinor = v
		case int:
			amountMinor = int64(v)
		case string:
			if parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				amountMinor = parsed
			}
		}
	}

	currency := strings.ToUpper(strings.TrimSpace(stringFromAny(entity["currency"])))

	c := &domain.PaymentConfirmation{
		ProviderCheckoutID:  providerOrderID,
		ProviderPaymentID:   paymentID,
		ReportedAmountMinor: amountMinor,
		ReportedCurrency:    currency,
		Signature:           signature,
		EventID:             getHeader(headers, "x-razorpay-event-id"),
	}

	if method := stringFromAny(entity["method"]); method != "" {
		c.Method = &method
	}
	if email := stringFromAny(entity["email"]); email != "" {
		c.Email = &email
	}

	return c, nil
}

func (g *Gateway) Refund(_ context.Context, req domain.RefundRequest) (*domain.RefundResult, error) {
	extraHeaders := map[string]string{}
	if req.IdempotencyKey != "" {
		extraHeaders["X-Refund-Idempotency"] = req.IdempotencyKey
	}
	body := map[string]interface{}{
		"notes": map[string]interface{}{
			"order_id":        req.OrderID.String(),
			"reason":          req.Reason,
			"idempotency_key": req.IdempotencyKey,
		},
	}
	res, err := g.client.Payment.Refund(req.ProviderPaymentID, int(req.AmountMinor), body, extraHeaders)
	if err != nil {
		return nil, fmt.Errorf("razorpay refund failed: %w", err)
	}
	amount, _ := res["amount"].(float64)
	if stringFromAny(res["payment_id"]) != req.ProviderPaymentID || amount != float64(req.AmountMinor) ||
		!strings.EqualFold(stringFromAny(res["currency"]), req.Currency) {
		return nil, errors.New("razorpay returned an unverified full refund")
	}
	refundID := stringFromAny(res["id"])
	status := strings.ToLower(stringFromAny(res["status"]))
	if status == "processed" {
		status = domain.RefundStatusSucceeded
	} else if status == "" {
		status = domain.RefundStatusPending
	}
	return &domain.RefundResult{
		ProviderRefundID: refundID,
		Status:           status,
	}, nil
}

func (g *Gateway) sign(orderID, paymentID string) string {
	h := hmac.New(sha256.New, []byte(g.secret))
	h.Write([]byte(orderID + "|" + paymentID))
	return hex.EncodeToString(h.Sum(nil))
}

func getHeader(headers map[string]string, key string) string {
	for k, v := range headers {
		if strings.EqualFold(k, key) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	if str, ok := value.(string); ok {
		return str
	}
	return fmt.Sprintf("%v", value)
}

func formatReceipt(id uuid.UUID) string {
	return "order_" + strings.ReplaceAll(id.String(), "-", "")
}
