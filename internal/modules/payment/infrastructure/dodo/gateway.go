// Package dodo is the Dodo Payments implementation of the payment gateway
// strategy: hosted checkout session, then a signed webhook on completion.
package dodo

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
)

type Config struct {
	APIKey     string
	ProductID  string
	WebhookKey string
	APIURL     string
	// ReturnURL is where Dodo sends the buyer after checkout.
	ReturnURL  string
	HTTPClient *http.Client
}

type Gateway struct {
	cfg    Config
	client *http.Client
}

var (
	_ domain.Gateway          = (*Gateway)(nil)
	_ domain.WebhookConfirmer = (*Gateway)(nil)
	_ domain.Refunder         = (*Gateway)(nil)
)

func NewGateway(cfg Config) *Gateway {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Gateway{cfg: cfg, client: client}
}

func (g *Gateway) Name() string { return domain.ProviderDodo }

func (g *Gateway) CreateCheckout(ctx context.Context, req domain.CheckoutRequest) (domain.CheckoutSession, error) {
	apiKey := strings.TrimSpace(g.cfg.APIKey)
	productID := strings.TrimSpace(g.cfg.ProductID)
	if apiKey == "" || productID == "" {
		return domain.CheckoutSession{}, errors.New("dodo payments is not configured")
	}
	baseURL := strings.TrimRight(g.cfg.APIURL, "/")
	if baseURL == "" {
		baseURL = "https://test.dodopayments.com"
	}

	order := req.Order
	metadata := map[string]any{
		"order_id":     order.ID.String(),
		"spec_id":      order.SpecID.String(),
		"license_type": order.LicenseType,
		"spec_title":   req.SpecTitle,
		"license_name": req.LicenseName,
	}

	returnURL := g.cfg.ReturnURL
	if returnURL == "" {
		returnURL = "/payment/return"
	}
	if parsed, err := url.Parse(returnURL); err == nil {
		q := parsed.Query()
		q.Set("order_id", order.ID.String())
		parsed.RawQuery = q.Encode()
		returnURL = parsed.String()
	} else {
		if strings.Contains(returnURL, "?") {
			returnURL = returnURL + "&order_id=" + order.ID.String()
		} else {
			returnURL = returnURL + "?order_id=" + order.ID.String()
		}
	}

	body := map[string]any{
		"product_cart": []map[string]any{{
			"product_id": productID,
			"quantity":   1,
			"amount":     order.Amount,
		}},
		"return_url":       returnURL,
		"metadata":         metadata,
		"billing_currency": order.Currency,
		"feature_flags": map[string]bool{
			"allow_currency_selection": false,
			"allow_discount_code":      false,
			"allow_editing_addons":     false,
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return domain.CheckoutSession{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/checkouts", bytes.NewReader(payload))
	if err != nil {
		return domain.CheckoutSession{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	res, err := g.client.Do(httpReq)
	if err != nil {
		return domain.CheckoutSession{}, fmt.Errorf("dodo checkout creation failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return domain.CheckoutSession{}, fmt.Errorf("dodo checkout creation failed with status %d: %s", res.StatusCode, strings.TrimSpace(string(msg)))
	}
	var decoded struct {
		SessionID   string  `json:"session_id"`
		CheckoutURL *string `json:"checkout_url"`
	}
	if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
		return domain.CheckoutSession{}, err
	}
	if decoded.SessionID == "" || decoded.CheckoutURL == nil || *decoded.CheckoutURL == "" {
		return domain.CheckoutSession{}, errors.New("invalid dodo checkout response")
	}
	return domain.CheckoutSession{ProviderRef: decoded.SessionID, CheckoutURL: *decoded.CheckoutURL}, nil
}

func (g *Gateway) Refund(ctx context.Context, req domain.RefundRequest) (*domain.RefundResult, error) {
	apiKey := strings.TrimSpace(g.cfg.APIKey)
	if apiKey == "" {
		return nil, errors.New("dodo payments is not configured")
	}
	baseURL := strings.TrimRight(g.cfg.APIURL, "/")
	if baseURL == "" {
		baseURL = "https://test.dodopayments.com"
	}

	body := map[string]any{
		"payment_id": req.ProviderPaymentID,
		"reason":     req.Reason,
		"metadata": map[string]any{
			"order_id":        req.OrderID.String(),
			"idempotency_key": req.IdempotencyKey,
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/refunds", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	res, err := g.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("dodo refund failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return nil, fmt.Errorf("dodo refund failed with status %d: %s", res.StatusCode, strings.TrimSpace(string(msg)))
	}

	var decoded struct {
		RefundID  string `json:"refund_id"`
		PaymentID string `json:"payment_id"`
		Partial   *bool  `json:"is_partial"`
		Status    string `json:"status"`
	}
	if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
		return nil, err
	}

	if decoded.PaymentID != req.ProviderPaymentID || decoded.Partial == nil || *decoded.Partial {
		return nil, errors.New("dodo returned an unverified full refund")
	}
	status := strings.ToLower(strings.TrimSpace(decoded.Status))
	if status == "succeeded" {
		status = domain.RefundStatusSucceeded
	} else if status == "review" {
		status = domain.RefundStatusNeedsReview
	} else if status == "" {
		status = domain.RefundStatusPending
	}

	return &domain.RefundResult{
		ProviderRefundID: decoded.RefundID,
		Status:           status,
	}, nil
}

func (g *Gateway) ParseWebhook(payload []byte, headers map[string]string) (*domain.PaymentConfirmation, error) {
	if err := g.verifySignature(payload, headers); err != nil {
		return nil, err
	}

	var event struct {
		Type string         `json:"type"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil, domain.ErrMalformedPayload
	}
	if event.Type == "refund.succeeded" || event.Type == "refund.failed" {
		refundID := stringFromAny(event.Data["refund_id"])
		paymentID := stringFromAny(event.Data["payment_id"])
		partial, hasPartial := event.Data["is_partial"].(bool)
		amount := extractAmountMinor(event.Data)
		if refundID == "" || paymentID == "" || !hasPartial || amount < 0 || amount > 1<<31-1 ||
			(partial && (amount == 0 || extractCurrency(event.Data) == "")) {
			return nil, domain.ErrMalformedPayload
		}
		metadata, _ := event.Data["metadata"].(map[string]any)
		return &domain.PaymentConfirmation{
			EventID: headers["webhook-id"], ProviderPaymentID: paymentID,
			Refund: &domain.RefundConfirmation{
				ProviderRefundID: refundID, Amount: int(amount), Currency: extractCurrency(event.Data),
				Full: !partial, Status: strings.TrimPrefix(event.Type, "refund."),
				IntentKey: stringFromAny(metadata["idempotency_key"]),
			},
		}, nil
	}
	if event.Type != "payment.succeeded" {
		return nil, nil
	}

	orderID, paymentID := extractOrderMetadata(event.Data)
	if orderID == uuid.Nil {
		return nil, domain.ErrOrderNotFound
	}
	if paymentID == "" {
		return nil, domain.ErrMissingPaymentID
	}

	amountMinor := extractAmountMinor(event.Data)
	taxMinor, _ := event.Data["tax"].(float64)
	currency := extractCurrency(event.Data)

	return &domain.PaymentConfirmation{
		OrderID:             orderID,
		ProviderCheckoutID:  stringFromAny(event.Data["checkout_session_id"]),
		ProviderPaymentID:   paymentID,
		ReportedAmountMinor: amountMinor,
		ReportedTaxMinor:    int64(taxMinor),
		ReportedCurrency:    currency,
		Signature:           headers["webhook-signature"],
		EventID:             headers["webhook-id"],
	}, nil
}

func (g *Gateway) verifySignature(payload []byte, headers map[string]string) error {
	secret := strings.TrimSpace(g.cfg.WebhookKey)
	if secret == "" {
		return domain.ErrMissingWebhookSecret
	}
	if strings.HasPrefix(strings.ToLower(secret), "http://") || strings.HasPrefix(strings.ToLower(secret), "https://") {
		return errors.New("DODO_PAYMENTS_WEBHOOK_KEY must be the Dodo webhook signing secret, not the endpoint URL")
	}
	webhookID := headers["webhook-id"]
	timestamp := headers["webhook-timestamp"]
	signature := headers["webhook-signature"]
	if webhookID == "" || timestamp == "" || signature == "" {
		return domain.ErrInvalidSignature
	}

	ts, err := strconv.ParseInt(strings.TrimSpace(timestamp), 10, 64)
	if err != nil {
		return domain.ErrTimestampToleranceExceeded
	}
	now := time.Now().Unix()
	diff := now - ts
	if diff < -300 || diff > 300 {
		return domain.ErrTimestampToleranceExceeded
	}

	message := webhookID + "." + timestamp + "." + string(payload)
	secretKeys := [][]byte{[]byte(secret)}
	if encoded, ok := strings.CutPrefix(secret, "whsec_"); ok {
		secretKeys = append(secretKeys, []byte(encoded))
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			decoded, err = base64.RawStdEncoding.DecodeString(encoded)
		}
		if err == nil {
			secretKeys = append(secretKeys, decoded)
		}
	}

	for _, secretKey := range secretKeys {
		mac := hmac.New(sha256.New, secretKey)
		mac.Write([]byte(message))
		sum := mac.Sum(nil)
		if compareSignature(signature, base64.StdEncoding.EncodeToString(sum), hex.EncodeToString(sum)) {
			return nil
		}
	}
	return domain.ErrInvalidSignature
}

func compareSignature(received string, expected ...string) bool {
	parts := strings.FieldsFunc(received, func(r rune) bool {
		return r == ' ' || r == ','
	})
	for _, part := range parts {
		part = strings.TrimSpace(strings.TrimPrefix(part, "v1="))
		if strings.EqualFold(part, "v1") || part == "" {
			continue
		}
		for _, candidate := range expected {
			if hmac.Equal([]byte(part), []byte(candidate)) {
				return true
			}
		}
	}
	for _, candidate := range expected {
		if hmac.Equal([]byte(strings.TrimSpace(received)), []byte(candidate)) {
			return true
		}
	}
	return false
}

func extractOrderMetadata(data map[string]any) (uuid.UUID, string) {
	if data == nil {
		return uuid.Nil, ""
	}
	paymentID := stringFromAny(data["payment_id"])
	if paymentID == "" {
		paymentID = stringFromAny(data["id"])
	}

	for _, containerKey := range []string{"metadata", "custom_data"} {
		if container, ok := data[containerKey].(map[string]any); ok && container != nil {
			if idStr := stringFromAny(container["order_id"]); idStr != "" {
				if id, err := uuid.Parse(idStr); err == nil {
					return id, paymentID
				}
			}
		}
	}

	if idStr := stringFromAny(data["order_id"]); idStr != "" {
		if id, err := uuid.Parse(idStr); err == nil {
			return id, paymentID
		}
	}

	return uuid.Nil, paymentID
}

func extractAmountMinor(data map[string]any) int64 {
	candidates := []string{"total_amount", "amount", "payment_amount"}
	for _, k := range candidates {
		if val, ok := data[k]; ok && val != nil {
			switch v := val.(type) {
			case float64:
				return int64(v)
			case int64:
				return v
			case int:
				return int64(v)
			case string:
				if parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
					return parsed
				}
			}
		}
	}
	return 0
}

func extractCurrency(data map[string]any) string {
	candidates := []string{"currency", "currency_code"}
	for _, k := range candidates {
		if val, ok := data[k]; ok && val != nil {
			if s, ok := val.(string); ok && strings.TrimSpace(s) != "" {
				return strings.ToUpper(strings.TrimSpace(s))
			}
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
