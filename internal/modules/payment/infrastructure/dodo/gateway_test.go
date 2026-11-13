package dodo

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sign(key []byte, id, ts string, payload []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "." + string(payload)))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestGateway_Name(t *testing.T) {
	assert.Equal(t, domain.ProviderDodo, NewGateway(Config{}).Name())
}

func TestGateway_CreateCheckout(t *testing.T) {
	ctx := context.Background()
	order := &domain.Order{ID: uuid.New(), SpecID: uuid.New(), LicenseType: "Basic", Amount: 1000, Currency: "USD"}
	req := domain.CheckoutRequest{Order: order, SpecTitle: "Track", LicenseName: "Basic"}

	newGateway := func(handler http.HandlerFunc) (*Gateway, func()) {
		ts := httptest.NewServer(handler)
		return NewGateway(Config{APIKey: " key ", ProductID: "prod_1", APIURL: ts.URL + "/", ReturnURL: "http://localhost:4200/dashboard"}), ts.Close
	}

	t.Run("success", func(t *testing.T) {
		var gotAuth string
		var gotBody map[string]any
		g, closeFn := newGateway(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			assert.Equal(t, "/checkouts", r.URL.Path)
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_ = json.NewEncoder(w).Encode(map[string]any{"session_id": "cks_1", "checkout_url": "https://pay.example/cks_1"})
		})
		defer closeFn()

		session, err := g.CreateCheckout(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, "cks_1", session.ProviderRef)
		assert.Equal(t, "https://pay.example/cks_1", session.CheckoutURL)
		assert.Equal(t, "Bearer key", gotAuth)
		assert.Equal(t, "http://localhost:4200/dashboard?order_id="+order.ID.String(), gotBody["return_url"])
		assert.Equal(t, "USD", gotBody["billing_currency"])
		assert.Equal(t, false, gotBody["feature_flags"].(map[string]any)["allow_currency_selection"])
		assert.Equal(t, false, gotBody["feature_flags"].(map[string]any)["allow_discount_code"])
		meta := gotBody["metadata"].(map[string]any)
		assert.Equal(t, order.ID.String(), meta["order_id"])
		assert.Equal(t, "Track", meta["spec_title"])
		cart := gotBody["product_cart"].([]any)[0].(map[string]any)
		assert.Equal(t, "prod_1", cart["product_id"])
		assert.EqualValues(t, 1000, cart["amount"])
	})

	t.Run("not configured", func(t *testing.T) {
		_, err := NewGateway(Config{}).CreateCheckout(ctx, req)
		assert.EqualError(t, err, "dodo payments is not configured")
	})

	t.Run("non-2xx status", func(t *testing.T) {
		g, closeFn := newGateway(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(" upstream broke "))
		})
		defer closeFn()
		_, err := g.CreateCheckout(ctx, req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "status 502")
		assert.Contains(t, err.Error(), "upstream broke")
	})

	t.Run("malformed json", func(t *testing.T) {
		g, closeFn := newGateway(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("{bad")) })
		defer closeFn()
		_, err := g.CreateCheckout(ctx, req)
		assert.Error(t, err)
	})

	t.Run("incomplete response", func(t *testing.T) {
		g, closeFn := newGateway(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"session_id": "cks_1"})
		})
		defer closeFn()
		_, err := g.CreateCheckout(ctx, req)
		assert.EqualError(t, err, "invalid dodo checkout response")
	})

	t.Run("connection failure", func(t *testing.T) {
		g, closeFn := newGateway(func(w http.ResponseWriter, r *http.Request) {})
		closeFn()
		_, err := g.CreateCheckout(ctx, req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "dodo checkout creation failed")
	})
}

func TestGateway_VerifySignature(t *testing.T) {
	payload := []byte(`{"type":"x"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	hdr := func(sig string) map[string]string {
		return map[string]string{"webhook-id": "id", "webhook-timestamp": ts, "webhook-signature": sig}
	}

	g := NewGateway(Config{})
	assert.ErrorIs(t, g.verifySignature(payload, hdr("x")), domain.ErrMissingWebhookSecret)

	g = NewGateway(Config{WebhookKey: "HTTPS://example.com/hook"})
	assert.ErrorContains(t, g.verifySignature(payload, hdr("x")), "not the endpoint URL")

	g = NewGateway(Config{WebhookKey: "plain-secret"})
	assert.ErrorIs(t, g.verifySignature(payload, map[string]string{"webhook-id": "id"}), domain.ErrInvalidSignature)

	// timestamp tolerance exceeded (>300s old)
	oldTS := strconv.FormatInt(time.Now().Unix()-301, 10)
	oldHdr := map[string]string{"webhook-id": "id", "webhook-timestamp": oldTS, "webhook-signature": "v1,abc"}
	assert.ErrorIs(t, g.verifySignature(payload, oldHdr), domain.ErrTimestampToleranceExceeded)

	// timestamp tolerance exceeded (>300s future)
	futureTS := strconv.FormatInt(time.Now().Unix()+301, 10)
	futureHdr := map[string]string{"webhook-id": "id", "webhook-timestamp": futureTS, "webhook-signature": "v1,abc"}
	assert.ErrorIs(t, g.verifySignature(payload, futureHdr), domain.ErrTimestampToleranceExceeded)

	// invalid timestamp format
	badTSHdr := map[string]string{"webhook-id": "id", "webhook-timestamp": "not-a-number", "webhook-signature": "v1,abc"}
	assert.ErrorIs(t, g.verifySignature(payload, badTSHdr), domain.ErrTimestampToleranceExceeded)

	// plain secret, base64 signature with v1 prefix
	assert.NoError(t, g.verifySignature(payload, hdr("v1,"+sign([]byte("plain-secret"), "id", ts, payload))))
	// plain secret, hex signature, unprefixed
	mac := hmac.New(sha256.New, []byte("plain-secret"))
	mac.Write([]byte("id." + ts + "." + string(payload)))
	assert.NoError(t, g.verifySignature(payload, hdr(hex.EncodeToString(mac.Sum(nil)))))
	// wrong signature
	assert.ErrorIs(t, g.verifySignature(payload, hdr("v1,AAAA")), domain.ErrInvalidSignature)

	// whsec with raw (unpadded) base64 key
	raw := []byte("rawkey-bytes!")
	g = NewGateway(Config{WebhookKey: "whsec_" + base64.RawStdEncoding.EncodeToString(raw)})
	assert.NoError(t, g.verifySignature(payload, hdr("v1,"+sign(raw, "id", ts, payload))))

	// whsec that is not valid base64: falls back to the literal remainder as key
	g = NewGateway(Config{WebhookKey: "whsec_***notbase64***"})
	assert.NoError(t, g.verifySignature(payload, hdr("v1,"+sign([]byte("***notbase64***"), "id", ts, payload))))
}

func TestCompareSignature(t *testing.T) {
	assert.True(t, compareSignature("v1,abc", "abc"))
	assert.True(t, compareSignature("v1,zzz v1,abc", "abc"), "space separated list")
	assert.True(t, compareSignature("v1=abc", "abc"))
	assert.True(t, compareSignature("abc", "other", "abc"))
	assert.True(t, compareSignature("  abc  ", "abc"), "trimmed whole-string fallback")
	assert.False(t, compareSignature("v1,nope", "abc"))
	assert.False(t, compareSignature("", "abc"))
	assert.False(t, compareSignature("v1", "v1x"))
}

func TestExtractOrderMetadata(t *testing.T) {
	oid := uuid.New()
	tests := []struct {
		name    string
		data    map[string]any
		wantID  uuid.UUID
		wantPay string
	}{
		{"metadata", map[string]any{"payment_id": "p1", "metadata": map[string]any{"order_id": oid.String()}}, oid, "p1"},
		{"custom_data and id fallback", map[string]any{"id": "p2", "custom_data": map[string]any{"order_id": oid.String()}}, oid, "p2"},
		{"custom_fields", map[string]any{"custom_fields": map[string]any{"order_id": oid.String()}}, uuid.Nil, ""},
		{"top-level order_id", map[string]any{"order_id": oid.String(), "payment_id": 42}, oid, "42"},
		{"invalid metadata falls through", map[string]any{"metadata": map[string]any{"order_id": "nope"}, "order_id": oid.String()}, oid, ""},
		{"nothing", map[string]any{"metadata": "string"}, uuid.Nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, pay := extractOrderMetadata(tt.data)
			assert.Equal(t, tt.wantID, id)
			assert.Equal(t, tt.wantPay, pay)
		})
	}
}

func TestStringFromAny(t *testing.T) {
	assert.Equal(t, "", stringFromAny(nil))
	assert.Equal(t, "s", stringFromAny("s"))
	assert.Equal(t, "12", stringFromAny(12))
	assert.Equal(t, "true", stringFromAny(true))
}

func TestGateway_ParseWebhook(t *testing.T) {
	key := []byte("whk")
	g := NewGateway(Config{WebhookKey: string(key)})
	oid := uuid.New()

	signed := func(t *testing.T, event map[string]any) ([]byte, map[string]string) {
		payload, err := json.Marshal(event)
		require.NoError(t, err)
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		return payload, map[string]string{
			"webhook-id": "evt_1", "webhook-timestamp": ts,
			"webhook-signature": "v1," + sign(key, "evt_1", ts, payload),
		}
	}
	succeeded := func(data map[string]any) map[string]any {
		if data == nil {
			data = map[string]any{}
		}
		data["metadata"] = map[string]any{"order_id": oid.String()}
		return map[string]any{"type": "payment.succeeded", "data": data}
	}

	t.Run("invalid signature", func(t *testing.T) {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		_, err := g.ParseWebhook([]byte(`{}`), map[string]string{"webhook-id": "a", "webhook-timestamp": ts, "webhook-signature": "c"})
		assert.ErrorIs(t, err, domain.ErrInvalidSignature)
	})

	t.Run("invalid json with valid signature", func(t *testing.T) {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		payload := []byte(`{not json`)
		_, err := g.ParseWebhook(payload, map[string]string{
			"webhook-id": "evt_1", "webhook-timestamp": ts,
			"webhook-signature": "v1," + sign(key, "evt_1", ts, payload),
		})
		assert.Error(t, err)
	})

	t.Run("other event types are ignored", func(t *testing.T) {
		p, h := signed(t, map[string]any{"type": "payment.failed", "data": map[string]any{}})
		c, err := g.ParseWebhook(p, h)
		assert.NoError(t, err)
		assert.Nil(t, c)
	})

	t.Run("missing order id", func(t *testing.T) {
		p, h := signed(t, map[string]any{"type": "payment.succeeded", "data": map[string]any{}})
		_, err := g.ParseWebhook(p, h)
		assert.ErrorIs(t, err, domain.ErrOrderNotFound)
	})

	t.Run("success maps to a provider-neutral confirmation", func(t *testing.T) {
		p, h := signed(t, succeeded(map[string]any{
			"payment_id":   "pay_dodo_1",
			"total_amount": 2500,
			"currency":     "USD",
		}))
		c, err := g.ParseWebhook(p, h)
		require.NoError(t, err)
		assert.Equal(t, oid, c.OrderID)
		assert.Equal(t, "pay_dodo_1", c.ProviderPaymentID)
		assert.Equal(t, h["webhook-signature"], c.Signature)
		assert.Equal(t, int64(2500), c.ReportedAmountMinor)
		assert.Equal(t, "USD", c.ReportedCurrency)
		assert.Equal(t, "evt_1", c.EventID)
	})

	t.Run("missing payment id returns ErrMissingPaymentID", func(t *testing.T) {
		p, h := signed(t, succeeded(nil))
		c, err := g.ParseWebhook(p, h)
		assert.Nil(t, c)
		assert.ErrorIs(t, err, domain.ErrMissingPaymentID)
	})

	t.Run("standard-webhooks whsec secret", func(t *testing.T) {
		secretBytes := []byte("dodo-test-webhook-secret")
		g := NewGateway(Config{WebhookKey: "whsec_" + base64.StdEncoding.EncodeToString(secretBytes)})
		payload, err := json.Marshal(succeeded(map[string]any{"payment_id": "pay_dodo_1"}))
		require.NoError(t, err)
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		c, err := g.ParseWebhook(payload, map[string]string{
			"webhook-id": "evt_dodo_1", "webhook-timestamp": ts,
			"webhook-signature": "v1," + sign(secretBytes, "evt_dodo_1", ts, payload),
		})
		require.NoError(t, err)
		assert.Equal(t, "pay_dodo_1", c.ProviderPaymentID)
	})
}

func TestSignedDodoRefundAndTaxEvidence(t *testing.T) {
	const secret = "test-webhook-secret"
	gateway := NewGateway(Config{WebhookKey: secret})
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	orderID := uuid.NewString()
	tests := []struct {
		name    string
		payload map[string]any
		check   func(*domain.PaymentConfirmation)
	}{
		{"taxed capture", map[string]any{"type": "payment.succeeded", "data": map[string]any{
			"payment_id": "pay_taxed", "total_amount": 1180, "tax": 180, "currency": "USD",
			"metadata": map[string]string{"order_id": orderID},
		}}, func(c *domain.PaymentConfirmation) {
			assert.EqualValues(t, 1180, c.ReportedAmountMinor)
			assert.EqualValues(t, 180, c.ReportedTaxMinor)
		}},
		{"external full refund", map[string]any{"type": "refund.succeeded", "data": map[string]any{
			"payment_id": "pay_taxed", "refund_id": "ref_full", "is_partial": false, "amount": nil,
		}}, func(c *domain.PaymentConfirmation) {
			require.NotNil(t, c.Refund)
			assert.True(t, c.Refund.Full)
			assert.Equal(t, domain.RefundStatusSucceeded, c.Refund.Status)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, err := json.Marshal(test.payload)
			require.NoError(t, err)
			eventID := uuid.NewString()
			headers := map[string]string{
				"webhook-id": eventID, "webhook-timestamp": timestamp,
				"webhook-signature": "v1," + sign([]byte(secret), eventID, timestamp, body),
			}
			result, err := gateway.ParseWebhook(body, headers)
			require.NoError(t, err)
			require.NotNil(t, result)
			test.check(result)
			body = append(body, ' ')
			_, err = gateway.ParseWebhook(body, headers)
			require.ErrorIs(t, err, domain.ErrInvalidSignature)
		})
	}
}
