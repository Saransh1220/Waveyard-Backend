package razorpay

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_Refund(t *testing.T) {
	oid := uuid.New()

	t.Run("successful refund processed", func(t *testing.T) {
		g := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/v1/payments/pay_123/refund", r.URL.Path)
			assert.Equal(t, "idemp-key-1", r.Header.Get("X-Refund-Idempotency"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":     "rfnd_rzp_1",
				"status": "processed",
			})
		})

		res, err := g.Refund(context.Background(), domain.RefundRequest{
			OrderID:           oid,
			ProviderPaymentID: "pay_123",
			AmountMinor:       1000,
			Currency:          "INR",
			IdempotencyKey:    "idemp-key-1",
			Reason:            "fraudulent",
		})
		require.NoError(t, err)
		assert.Equal(t, "rfnd_rzp_1", res.ProviderRefundID)
		assert.Equal(t, domain.RefundStatusSucceeded, res.Status)
	})

	t.Run("successful refund pending default status", func(t *testing.T) {
		g := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":     "rfnd_rzp_2",
				"status": "",
			})
		})

		res, err := g.Refund(context.Background(), domain.RefundRequest{
			OrderID:           oid,
			ProviderPaymentID: "pay_123",
			AmountMinor:       1000,
		})
		require.NoError(t, err)
		assert.Equal(t, "rfnd_rzp_2", res.ProviderRefundID)
		assert.Equal(t, domain.RefundStatusPending, res.Status)
	})

	t.Run("gateway failure", func(t *testing.T) {
		g := newTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"description": "bad request"}})
		})

		_, err := g.Refund(context.Background(), domain.RefundRequest{
			OrderID:           oid,
			ProviderPaymentID: "pay_123",
		})
		require.Error(t, err)
	})
}
