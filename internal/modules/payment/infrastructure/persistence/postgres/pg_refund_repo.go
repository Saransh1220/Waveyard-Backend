package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
)

type PgRefundRepository struct {
	db DBTX
}

func NewRefundRepository(db *sqlx.DB) domain.RefundRepository {
	return NewRefundRepositoryWithDB(db)
}

func NewRefundRepositoryWithDB(db DBTX) domain.RefundRepository {
	return &PgRefundRepository{db: db}
}

func (r *PgRefundRepository) ReserveRefund(ctx context.Context, ref *domain.OrderRefund) (*domain.OrderRefund, bool, error) {
	if ref.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return nil, false, err
		}
		ref.ID = id
	}
	if ref.CreatedAt.IsZero() {
		ref.CreatedAt = time.Now()
	}
	ref.UpdatedAt = time.Now()
	if ref.Status == "" {
		ref.Status = domain.RefundStatusRequested
	}

	query := `
		INSERT INTO order_refunds (
			id, order_id, payment_id, user_id, admin_id,
			idempotency_key, amount, currency, provider,
			provider_refund_id, provider_payment_id, reason, notes,
			status, failure_reason, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17
		)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id, order_id, payment_id, user_id, admin_id,
			idempotency_key, amount, currency, provider,
			provider_refund_id, provider_payment_id, reason, notes,
			status, failure_reason, created_at, updated_at
	`

	var existing domain.OrderRefund
	var provRefundID, notes, failReason sql.NullString

	err := r.db.QueryRowContext(ctx, query,
		ref.ID, ref.OrderID, ref.PaymentID, ref.UserID, ref.AdminID,
		ref.IdempotencyKey, ref.Amount, ref.Currency, ref.Provider,
		ref.ProviderRefundID, ref.ProviderPaymentID, ref.Reason, ref.Notes,
		ref.Status, ref.FailureReason, ref.CreatedAt, ref.UpdatedAt,
	).Scan(
		&existing.ID, &existing.OrderID, &existing.PaymentID, &existing.UserID, &existing.AdminID,
		&existing.IdempotencyKey, &existing.Amount, &existing.Currency, &existing.Provider,
		&provRefundID, &existing.ProviderPaymentID, &existing.Reason, &notes,
		&existing.Status, &failReason, &existing.CreatedAt, &existing.UpdatedAt,
	)

	if err == nil {
		if provRefundID.Valid {
			existing.ProviderRefundID = &provRefundID.String
		}
		if notes.Valid {
			existing.Notes = &notes.String
		}
		if failReason.Valid {
			existing.FailureReason = &failReason.String
		}
		return &existing, true, nil
	}

	if errors.Is(err, sql.ErrNoRows) {
		found, getErr := r.GetByIdempotencyKey(ctx, ref.IdempotencyKey)
		if getErr != nil {
			return nil, false, getErr
		}
		return found, false, nil
	}

	return nil, false, err
}

func (r *PgRefundRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.OrderRefund, error) {
	query := `
		SELECT id, order_id, payment_id, user_id, admin_id,
			idempotency_key, amount, currency, provider,
			provider_refund_id, provider_payment_id, reason, notes,
			status, failure_reason, created_at, updated_at
		FROM order_refunds WHERE id = $1
	`
	return scanRefund(r.db.QueryRowContext(ctx, query, id))
}

func (r *PgRefundRepository) GetByOrderID(ctx context.Context, orderID uuid.UUID) ([]domain.OrderRefund, error) {
	query := `
		SELECT id, order_id, payment_id, user_id, admin_id,
			idempotency_key, amount, currency, provider,
			provider_refund_id, provider_payment_id, reason, notes,
			status, failure_reason, created_at, updated_at
		FROM order_refunds WHERE order_id = $1 ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refunds []domain.OrderRefund
	for rows.Next() {
		ref, err := scanRefund(rows)
		if err != nil {
			return nil, err
		}
		refunds = append(refunds, *ref)
	}
	return refunds, rows.Err()
}

func (r *PgRefundRepository) GetByIdempotencyKey(ctx context.Context, key string) (*domain.OrderRefund, error) {
	query := `
		SELECT id, order_id, payment_id, user_id, admin_id,
			idempotency_key, amount, currency, provider,
			provider_refund_id, provider_payment_id, reason, notes,
			status, failure_reason, created_at, updated_at
		FROM order_refunds WHERE idempotency_key = $1
	`
	return scanRefund(r.db.QueryRowContext(ctx, query, key))
}

func (r *PgRefundRepository) GetByProviderRefundID(ctx context.Context, provider, providerRefundID string) (*domain.OrderRefund, error) {
	query := `
		SELECT id, order_id, payment_id, user_id, admin_id,
			idempotency_key, amount, currency, provider,
			provider_refund_id, provider_payment_id, reason, notes,
			status, failure_reason, created_at, updated_at
		FROM order_refunds WHERE provider = $1 AND provider_refund_id = $2
	`
	return scanRefund(r.db.QueryRowContext(ctx, query, provider, providerRefundID))
}

func (r *PgRefundRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string, providerRefundID *string, failureReason *string) error {
	query := `
		UPDATE order_refunds
		SET status = $2,
			provider_refund_id = COALESCE($3, provider_refund_id),
			failure_reason = COALESCE($4, failure_reason),
			updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.db.ExecContext(ctx, query, id, status, providerRefundID, failureReason)
	return err
}

func scanRefund(row rowScanner) (*domain.OrderRefund, error) {
	var ref domain.OrderRefund
	var provRefundID, notes, failReason sql.NullString

	err := row.Scan(
		&ref.ID, &ref.OrderID, &ref.PaymentID, &ref.UserID, &ref.AdminID,
		&ref.IdempotencyKey, &ref.Amount, &ref.Currency, &ref.Provider,
		&provRefundID, &ref.ProviderPaymentID, &ref.Reason, &notes,
		&ref.Status, &failReason, &ref.CreatedAt, &ref.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if provRefundID.Valid {
		ref.ProviderRefundID = &provRefundID.String
	}
	if notes.Valid {
		ref.Notes = &notes.String
	}
	if failReason.Valid {
		ref.FailureReason = &failReason.String
	}
	return &ref, nil
}
