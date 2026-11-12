package postgres

import (
	"context"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
)

func (r *PgRefundRepository) LockPayment(ctx context.Context, paymentID uuid.UUID) error {
	var id uuid.UUID
	return r.db.GetContext(ctx, &id, `SELECT id FROM payments WHERE id = $1 FOR UPDATE`, paymentID)
}

func (r *PgRefundRepository) HasProviderRefund(ctx context.Context, paymentID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.GetContext(ctx, &exists, `SELECT EXISTS (
		SELECT 1 FROM payment_provider_refunds WHERE payment_id = $1 AND status <> 'failed'
	)`, paymentID)
	return exists, err
}

func (r *PgRefundRepository) RecordProviderRefund(ctx context.Context, paymentID uuid.UUID, provider string, refund domain.RefundConfirmation) (int, error) {
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO payment_provider_refunds(provider, provider_refund_id, payment_id, amount, status)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (provider, provider_refund_id) DO UPDATE SET status = CASE
			WHEN payment_provider_refunds.status = 'succeeded' OR EXCLUDED.status = 'succeeded' THEN 'succeeded'
			WHEN payment_provider_refunds.status = 'failed' THEN 'failed'
			ELSE EXCLUDED.status END
		WHERE payment_provider_refunds.payment_id = EXCLUDED.payment_id
		  AND payment_provider_refunds.amount = EXCLUDED.amount
	`, provider, refund.ProviderRefundID, paymentID, refund.Amount, refund.Status)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if rows == 0 {
		return 0, domain.ErrAmountMismatch
	}
	var total int
	err = r.db.GetContext(ctx, &total, `SELECT COALESCE(SUM(amount), 0)
		FROM payment_provider_refunds WHERE payment_id = $1 AND status = 'succeeded'`, paymentID)
	return total, err
}
