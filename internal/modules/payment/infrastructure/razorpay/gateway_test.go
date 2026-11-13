package razorpay

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestGateway(t *testing.T, handler http.HandlerFunc) *Gateway {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return NewGateway(Config{KeyID: "key", KeySecret: "secret", BaseURL: ts.URL})
}

func TestGateway_Name(t *testing.T) {
	assert.Equal(t, domain.ProviderRazorpay, NewGateway(Config{}).Name())
}

func TestFormatReceipt(t *testing.T) {
	receipt := formatReceipt(uuid.MustParse("018f0f61-8f17-7b1d-b3c2-dc681d7b6adf"))
	assert.Equal(t, "order_018f0f618f177b1db3c2dc681d7b6adf", receipt)
	assert.Len(t, receipt, 38)
	assert.NotContains(t, receipt, "-")
}

func TestGateway_CreateCheckout(t *testing.T) {
	order := &domain.Order{ID: uuid.New(), Amount: 9900, Currency: "INR"}

	t.Run("sends amount, currency and a razorpay-safe receipt", func(t *testing.T) {
		var body map[string]any
		g := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/v1/orders", r.URL.Path)
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "order_local_1"})
		})

		session, err := g.CreateCheckout(context.Background(), domain.CheckoutRequest{Order: order})
		require.NoError(t, err)
		assert.Equal(t, "order_local_1", session.ProviderRef)
		assert.Empty(t, session.CheckoutURL, "razorpay checkout opens in the browser, no hosted url")
		assert.EqualValues(t, 9900, body["amount"])
		assert.Equal(t, "INR", body["currency"])
		assert.Regexp(t, "^order_[0-9a-f]{32}$", body["receipt"])
	})

	t.Run("API error", func(t *testing.T) {
		g := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"description":"nope"}}`))
		})
		_, err := g.CreateCheckout(context.Background(), domain.CheckoutRequest{Order: order})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "razorpay order creation failed")
	})

	t.Run("empty id in response", func(t *testing.T) {
		g := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": ""})
		})
		_, err := g.CreateCheckout(context.Background(), domain.CheckoutRequest{Order: order})
		assert.EqualError(t, err, "invalid razorpay order response")
	})
}

func TestGateway_ConfirmClientPayment(t *testing.T) {
	ctx := context.Background()
	const rzpOrder, paymentID = "order_local_2", "pay_local_1"

	paymentHandler := func(status string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/payments/"+paymentID {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": paymentID, "status": status, "method": "card", "email": "buyer@example.com",
			})
		}
	}
	order := func(ref *string) *domain.Order {
		return &domain.Order{ID: uuid.New(), Provider: domain.ProviderRazorpay, ProviderCheckoutID: ref}
	}
	ref := rzpOrder

	t.Run("captured payment returns a confirmation", func(t *testing.T) {
		g := newTestGateway(t, paymentHandler("captured"))
		o := order(&ref)
		c, err := g.ConfirmClientPayment(ctx, o, paymentID, g.sign(rzpOrder, paymentID))
		require.NoError(t, err)
		assert.Equal(t, o.ID, c.OrderID)
		assert.Equal(t, paymentID, c.ProviderPaymentID)
		assert.Equal(t, g.sign(rzpOrder, paymentID), c.Signature)
		require.NotNil(t, c.Method)
		assert.Equal(t, "card", *c.Method)
		require.NotNil(t, c.Email)
		assert.Equal(t, "buyer@example.com", *c.Email)
	})

	t.Run("status is matched case-insensitively", func(t *testing.T) {
		g := newTestGateway(t, paymentHandler("CAPTURED"))
		_, err := g.ConfirmClientPayment(ctx, order(&ref), paymentID, g.sign(rzpOrder, paymentID))
		assert.NoError(t, err)
	})

	t.Run("order without a checkout id", func(t *testing.T) {
		g := newTestGateway(t, paymentHandler("captured"))
		_, err := g.ConfirmClientPayment(ctx, order(nil), paymentID, "sig")
		assert.EqualError(t, err, "invalid order state")
		empty := ""
		_, err = g.ConfirmClientPayment(ctx, order(&empty), paymentID, "sig")
		assert.EqualError(t, err, "invalid order state")
	})

	t.Run("bad signature is rejected before any API call", func(t *testing.T) {
		called := false
		g := newTestGateway(t, func(http.ResponseWriter, *http.Request) { called = true })
		_, err := g.ConfirmClientPayment(ctx, order(&ref), paymentID, "wrong")
		assert.ErrorIs(t, err, domain.ErrInvalidSignature)
		assert.False(t, called)
	})

	t.Run("fetch error", func(t *testing.T) {
		g := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"description":"bad"}}`))
		})
		_, err := g.ConfirmClientPayment(ctx, order(&ref), paymentID, g.sign(rzpOrder, paymentID))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch payment")
	})

	t.Run("not captured", func(t *testing.T) {
		g := newTestGateway(t, paymentHandler("authorized"))
		_, err := g.ConfirmClientPayment(ctx, order(&ref), paymentID, g.sign(rzpOrder, paymentID))
		assert.ErrorIs(t, err, domain.ErrPaymentNotCaptured)
	})
}

func TestGateway_SignIsDeterministicAndKeyed(t *testing.T) {
	a := NewGateway(Config{KeySecret: "one"})
	b := NewGateway(Config{KeySecret: "two"})
	assert.Equal(t, a.sign("order_1", "pay_1"), a.sign("order_1", "pay_1"))
	assert.NotEqual(t, a.sign("order_1", "pay_1"), b.sign("order_1", "pay_1"))
	assert.NotEqual(t, a.sign("order_1", "pay_1"), a.sign("order_1", "pay_2"))
}

func TestGateway_ParseWebhook(t *testing.T) {
	secret := "whsec_test_razorpay_secret"
	g := NewGateway(Config{WebhookSecret: secret})

	signBody := func(body []byte) string {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		return hex.EncodeToString(mac.Sum(nil))
	}

	t.Run("missing webhook secret returns ErrMissingWebhookSecret", func(t *testing.T) {
		gNoSec := NewGateway(Config{})
		_, err := gNoSec.ParseWebhook([]byte("{}"), map[string]string{"x-razorpay-signature": "any"})
		assert.ErrorIs(t, err, domain.ErrMissingWebhookSecret)
	})

	t.Run("missing signature header returns ErrInvalidSignature", func(t *testing.T) {
		_, err := g.ParseWebhook([]byte("{}"), map[string]string{})
		assert.ErrorIs(t, err, domain.ErrInvalidSignature)
	})

	t.Run("invalid signature returns ErrInvalidSignature", func(t *testing.T) {
		body := []byte(`{"event":"payment.captured"}`)
		_, err := g.ParseWebhook(body, map[string]string{"x-razorpay-signature": "wrong_hex"})
		assert.ErrorIs(t, err, domain.ErrInvalidSignature)
	})

	t.Run("malformed json returns error", func(t *testing.T) {
		body := []byte(`{not-json`)
		sig := signBody(body)
		_, err := g.ParseWebhook(body, map[string]string{"x-razorpay-signature": sig})
		assert.Error(t, err)
	})

	t.Run("non payment.captured event is ignored", func(t *testing.T) {
		body := []byte(`{"event":"payment.authorized","payload":{"payment":{"entity":{"id":"pay_123"}}}}`)
		sig := signBody(body)
		c, err := g.ParseWebhook(body, map[string]string{"x-razorpay-signature": sig})
		assert.NoError(t, err)
		assert.Nil(t, c)
	})

	t.Run("missing payment id returns ErrMissingPaymentID", func(t *testing.T) {
		body := []byte(`{"event":"payment.captured","payload":{"payment":{"entity":{"order_id":"order_rzp_1","amount":1000,"currency":"INR"}}}}`)
		sig := signBody(body)
		c, err := g.ParseWebhook(body, map[string]string{"x-razorpay-signature": sig})
		assert.Nil(t, c)
		assert.ErrorIs(t, err, domain.ErrMissingPaymentID)
	})

	t.Run("missing order id returns error", func(t *testing.T) {
		body := []byte(`{"event":"payment.captured","payload":{"payment":{"entity":{"id":"pay_123","amount":1000,"currency":"INR"}}}}`)
		sig := signBody(body)
		c, err := g.ParseWebhook(body, map[string]string{"x-razorpay-signature": sig})
		assert.Nil(t, c)
		assert.ErrorIs(t, err, domain.ErrOrderNotFound)
	})

	t.Run("valid payment.captured parses confirmation", func(t *testing.T) {
		body := []byte(`{
			"event":"payment.captured",
			"payload":{
				"payment":{
					"entity":{
						"id":"pay_valid_123",
						"order_id":"order_rzp_valid_456",
						"amount":49900,
						"currency":"INR",
						"method":"upi",
						"email":"buyer@example.com"
					}
				}
			}
		}`)
		sig := signBody(body)
		c, err := g.ParseWebhook(body, map[string]string{
			"x-razorpay-signature": sig,
			"x-razorpay-event-id":  "evt_rzp_789",
		})
		require.NoError(t, err)
		require.NotNil(t, c)
		assert.Equal(t, "pay_valid_123", c.ProviderPaymentID)
		assert.Equal(t, "order_rzp_valid_456", c.ProviderCheckoutID)
		assert.Equal(t, int64(49900), c.ReportedAmountMinor)
		assert.Equal(t, "INR", c.ReportedCurrency)
		assert.Equal(t, sig, c.Signature)
		assert.Equal(t, "evt_rzp_789", c.EventID)
		require.NotNil(t, c.Method)
		assert.Equal(t, "upi", *c.Method)
		require.NotNil(t, c.Email)
		assert.Equal(t, "buyer@example.com", *c.Email)
	})
}

func TestSignedRazorpayExternalRefundWithEmptyNotes(t *testing.T) {
	const secret = "test-webhook-secret"
	gateway := NewGateway(Config{WebhookSecret: secret})
	payload := []byte(`{"event":"refund.processed","payload":{"refund":{"entity":{"id":"rfnd_example","payment_id":"pay_example","amount":1000,"currency":"INR","notes":[]}}}}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	headers := map[string]string{"x-razorpay-signature": hex.EncodeToString(mac.Sum(nil))}
	result, err := gateway.ParseWebhook(payload, headers)
	require.NoError(t, err)
	require.NotNil(t, result.Refund)
	assert.Equal(t, "pay_example", result.ProviderPaymentID)
	assert.Equal(t, domain.RefundStatusSucceeded, result.Refund.Status)
	assert.Equal(t, 1000, result.Refund.Amount)
}
