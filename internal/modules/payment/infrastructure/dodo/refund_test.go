package dodo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_Refund(t *testing.T) {
	ctx := context.Background()
	oid := uuid.New()

	newGateway := func(handler http.HandlerFunc) (*Gateway, func()) {
		ts := httptest.NewServer(handler)
		return NewGateway(Config{APIKey: "key_123", APIURL: ts.URL + "/"}), ts.Close
	}

	t.Run("unconfigured api key", func(t *testing.T) {
		g := NewGateway(Config{})
		_, err := g.Refund(ctx, domain.RefundRequest{OrderID: oid})
		assert.EqualError(t, err, "dodo payments is not configured")
	})

	t.Run("successful refund succeeded", func(t *testing.T) {
		var gotAuth string
		var gotBody map[string]any

		g, closeFn := newGateway(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			assert.Equal(t, "/refunds", r.URL.Path)
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"refund_id":  "rfnd_dodo_1",
				"payment_id": "pay_dodo_1",
				"is_partial": false,
				"status":     "succeeded",
			})
		})
		defer closeFn()

		res, err := g.Refund(ctx, domain.RefundRequest{
			OrderID:           oid,
			ProviderPaymentID: "pay_dodo_1",
			AmountMinor:       2000,
			Currency:          "USD",
			IdempotencyKey:    "ref-idemp-1",
			Reason:            "fraudulent",
		})
		require.NoError(t, err)
		assert.Equal(t, "Bearer key_123", gotAuth)
		assert.NotContains(t, gotBody, "amount")
		assert.Equal(t, "rfnd_dodo_1", res.ProviderRefundID)
		assert.Equal(t, domain.RefundStatusSucceeded, res.Status)
	})

	t.Run("successful refund default status pending", func(t *testing.T) {
		g, closeFn := newGateway(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"refund_id":  "rfnd_dodo_2",
				"payment_id": "pay_dodo_2",
				"is_partial": false,
				"status":     "",
			})
		})
		defer closeFn()

		res, err := g.Refund(ctx, domain.RefundRequest{
			OrderID:           oid,
			ProviderPaymentID: "pay_dodo_2",
			AmountMinor:       2000,
		})
		require.NoError(t, err)
		assert.Equal(t, "rfnd_dodo_2", res.ProviderRefundID)
		assert.Equal(t, domain.RefundStatusPending, res.Status)
	})

	t.Run("provider returns 400 error", func(t *testing.T) {
		g, closeFn := newGateway(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("insufficient balance"))
		})
		defer closeFn()

		_, err := g.Refund(ctx, domain.RefundRequest{
			OrderID:           oid,
			ProviderPaymentID: "pay_dodo_3",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "insufficient balance")
	})
}

func TestGatewayFindCheckoutUsesCapturedPaymentMetadata(t *testing.T) {
	order := &domain.Order{ID: uuid.New(), Amount: 1000, Currency: "USD", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/payments", r.URL.Path)
		assert.Equal(t, "succeeded", r.URL.Query().Get("status"))
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{
			"payment_id": "pay_recovered", "checkout_session_id": "cks_recovered", "total_amount": 1180, "tax": 180,
			"currency": "USD", "status": "succeeded", "metadata": map[string]string{"order_id": order.ID.String()},
		}}})
	}))
	defer server.Close()
	gateway := NewGateway(Config{APIKey: "test-key", APIURL: server.URL})
	session, capture, err := gateway.FindCheckout(context.Background(), order)
	require.NoError(t, err)
	require.Nil(t, session)
	require.NotNil(t, capture)
	assert.Equal(t, "pay_recovered", capture.ProviderPaymentID)
	assert.Equal(t, "cks_recovered", capture.ProviderCheckoutID)
	assert.EqualValues(t, 1180, capture.ReportedAmountMinor)
	assert.EqualValues(t, 180, capture.ReportedTaxMinor)
}
