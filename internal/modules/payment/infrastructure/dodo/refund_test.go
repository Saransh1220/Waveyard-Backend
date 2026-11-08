package dodo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
				"refund_id": "rfnd_dodo_1",
				"status":    "succeeded",
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
		assert.Equal(t, "rfnd_dodo_1", res.ProviderRefundID)
		assert.Equal(t, domain.RefundStatusSucceeded, res.Status)
	})

	t.Run("successful refund default status pending", func(t *testing.T) {
		g, closeFn := newGateway(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"refund_id": "rfnd_dodo_2",
				"status":    "",
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
