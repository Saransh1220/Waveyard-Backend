package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
)

type PgWebhookRepository struct {
	db DBTX
}

func NewWebhookRepository(db *sqlx.DB) domain.WebhookEventRepository {
	return NewWebhookRepositoryWithDB(db)
}

func NewWebhookRepositoryWithDB(db DBTX) domain.WebhookEventRepository {
	return &PgWebhookRepository{db: db}
}

func (r *PgWebhookRepository) InsertEvent(ctx context.Context, e *domain.PaymentWebhookEvent) (*domain.PaymentWebhookEvent, bool, error) {
	if e.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return nil, false, err
		}
		e.ID = id
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	e.UpdatedAt = time.Now()
	if e.Status == "" {
		e.Status = domain.WebhookEventStatusQueued
	}
	if e.MaxAttempts <= 0 {
		e.MaxAttempts = 5
	}
	if e.NextRetryAt.IsZero() {
		e.NextRetryAt = time.Now()
	}

	headersJSON, err := json.Marshal(e.Headers)
	if err != nil {
		headersJSON = []byte("{}")
	}

	query := `
		INSERT INTO payment_webhook_events (
			id, provider, event_id, event_type, order_id,
			provider_checkout_id, provider_payment_id, reported_amount, reported_currency,
			payload, headers, status, attempts, max_attempts,
			last_error, worker_id, locked_at, lease_expires_at, next_retry_at,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21
		)
		ON CONFLICT (provider, event_id) DO NOTHING
		RETURNING id, provider, event_id, event_type, order_id,
			provider_checkout_id, provider_payment_id, reported_amount, reported_currency,
			payload, headers, status, attempts, max_attempts,
			last_error, worker_id, locked_at, lease_expires_at, next_retry_at,
			created_at, updated_at
	`

	res := r.db.QueryRowContext(ctx, query,
		e.ID, e.Provider, e.EventID, e.EventType, e.OrderID,
		e.ProviderCheckoutID, e.ProviderPaymentID, e.ReportedAmount, e.ReportedCurrency,
		e.Payload, headersJSON, e.Status, e.Attempts, e.MaxAttempts,
		e.LastError, e.WorkerID, e.LockedAt, e.LeaseExpiresAt, e.NextRetryAt,
		e.CreatedAt, e.UpdatedAt,
	)

	existing, scanErr := scanWebhookEvent(res)
	if scanErr == nil && existing != nil {
		return existing, true, nil
	}

	if errors.Is(scanErr, sql.ErrNoRows) || (scanErr == nil && existing == nil) {
		// Event already exists, look it up
		foundQuery := `
			SELECT id, provider, event_id, event_type, order_id,
				provider_checkout_id, provider_payment_id, reported_amount, reported_currency,
				payload, headers, status, attempts, max_attempts,
				last_error, worker_id, locked_at, lease_expires_at, next_retry_at,
				created_at, updated_at
			FROM payment_webhook_events
			WHERE provider = $1 AND event_id = $2
		`
		found, getErr := scanWebhookEvent(r.db.QueryRowContext(ctx, foundQuery, e.Provider, e.EventID))
		if getErr != nil {
			return nil, false, getErr
		}
		return found, false, nil
	}

	return nil, false, scanErr
}

func (r *PgWebhookRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.PaymentWebhookEvent, error) {
	query := `
		SELECT id, provider, event_id, event_type, order_id,
			provider_checkout_id, provider_payment_id, reported_amount, reported_currency,
			payload, headers, status, attempts, max_attempts,
			last_error, worker_id, locked_at, lease_expires_at, next_retry_at,
			created_at, updated_at
		FROM payment_webhook_events
		WHERE id = $1
	`
	return scanWebhookEvent(r.db.QueryRowContext(ctx, query, id))
}

func (r *PgWebhookRepository) ClaimNextBatch(ctx context.Context, workerID string, batchSize int, leaseDuration time.Duration) ([]domain.PaymentWebhookEvent, error) {
	leaseSeconds := int(leaseDuration.Seconds())
	if leaseSeconds <= 0 {
		leaseSeconds = 60
	}
	if batchSize <= 0 {
		batchSize = 10
	}

	query := fmt.Sprintf(`
		WITH exhausted_events AS (
			UPDATE payment_webhook_events
			SET status = 'needs_review', last_error = 'processing lease expired after final attempt',
				locked_at = NULL, lease_expires_at = NULL, updated_at = NOW()
			WHERE status = 'processing' AND lease_expires_at <= NOW() AND attempts >= max_attempts
		), due_events AS (
			SELECT id
			FROM payment_webhook_events
			WHERE status IN ('queued', 'failed', 'processing')
			  AND next_retry_at <= NOW()
			  AND attempts < max_attempts
			  AND (lease_expires_at IS NULL OR lease_expires_at <= NOW())
			ORDER BY next_retry_at ASC
			LIMIT %d
			FOR UPDATE SKIP LOCKED
		)
		UPDATE payment_webhook_events e
		SET status = 'processing',
			worker_id = $1,
			locked_at = NOW(),
			lease_expires_at = NOW() + INTERVAL '%d seconds',
			attempts = attempts + 1,
			updated_at = NOW()
		FROM due_events
		WHERE e.id = due_events.id
		RETURNING e.id, e.provider, e.event_id, e.event_type, e.order_id,
			e.provider_checkout_id, e.provider_payment_id, e.reported_amount, e.reported_currency,
			e.payload, e.headers, e.status, e.attempts, e.max_attempts,
			e.last_error, e.worker_id, e.locked_at, e.lease_expires_at, e.next_retry_at,
			e.created_at, e.updated_at
	`, batchSize, leaseSeconds)

	rows, err := r.db.QueryContext(ctx, query, workerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []domain.PaymentWebhookEvent
	for rows.Next() {
		ev, err := scanWebhookEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, *ev)
	}
	return events, rows.Err()
}

func (r *PgWebhookRepository) MarkCompleted(ctx context.Context, id uuid.UUID, lockedAt *time.Time) error {
	query := `
		UPDATE payment_webhook_events
		SET status = 'completed',
			locked_at = NULL,
			lease_expires_at = NULL,
			updated_at = NOW()
		WHERE id = $1 AND status = 'processing' AND locked_at = $2
	`
	_, err := r.db.ExecContext(ctx, query, id, lockedAt)
	return err
}

func (r *PgWebhookRepository) MarkFailed(ctx context.Context, id uuid.UUID, lockedAt *time.Time, lastErr string, retryAfter time.Duration, status string) error {
	if status == "" {
		status = domain.WebhookEventStatusFailed
	}
	retrySeconds := int(retryAfter.Seconds())
	if retrySeconds < 5 {
		retrySeconds = 5
	}
	query := fmt.Sprintf(`
		UPDATE payment_webhook_events
		SET status = $2,
			last_error = $3,
			next_retry_at = NOW() + INTERVAL '%d seconds',
			locked_at = NULL,
			lease_expires_at = NULL,
			updated_at = NOW()
		WHERE id = $1 AND status = 'processing' AND locked_at = $4
	`, retrySeconds)

	_, err := r.db.ExecContext(ctx, query, id, status, lastErr, lockedAt)
	return err
}

func (r *PgWebhookRepository) ListEvents(ctx context.Context, filter domain.WebhookEventFilter, limit, offset int) ([]domain.PaymentWebhookEvent, int, error) {
	if limit <= 0 {
		limit = 20
	}
	countQuery := `
		SELECT COUNT(*)
		FROM payment_webhook_events
		WHERE ($1 = '' OR provider = $1)
		  AND ($2 = '' OR status = $2)
		  AND ($3::uuid IS NULL OR order_id = $3)
	`
	var total int
	err := r.db.QueryRowContext(ctx, countQuery, filter.Provider, filter.Status, filter.OrderID).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	query := `
		SELECT id, provider, event_id, event_type, order_id,
			provider_checkout_id, provider_payment_id, reported_amount, reported_currency,
			payload, headers, status, attempts, max_attempts,
			last_error, worker_id, locked_at, lease_expires_at, next_retry_at,
			created_at, updated_at
		FROM payment_webhook_events
		WHERE ($1 = '' OR provider = $1)
		  AND ($2 = '' OR status = $2)
		  AND ($3::uuid IS NULL OR order_id = $3)
		ORDER BY created_at DESC
		LIMIT $4 OFFSET $5
	`
	rows, err := r.db.QueryContext(ctx, query, filter.Provider, filter.Status, filter.OrderID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var events []domain.PaymentWebhookEvent
	for rows.Next() {
		ev, err := scanWebhookEvent(rows)
		if err != nil {
			return nil, 0, err
		}
		events = append(events, *ev)
	}
	return events, total, rows.Err()
}

func (r *PgWebhookRepository) Requeue(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE payment_webhook_events
		SET status = 'queued',
			attempts = 0,
			next_retry_at = NOW(),
			locked_at = NULL,
			lease_expires_at = NULL,
			updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

func scanWebhookEvent(row rowScanner) (*domain.PaymentWebhookEvent, error) {
	var e domain.PaymentWebhookEvent
	var provCheckoutID, provPaymentID, reportedCurr, lastErr, workerID sql.NullString
	var reportedAmt sql.NullInt64
	var orderID *uuid.UUID
	var headersBytes []byte
	var lockedAt, leaseExp sql.NullTime

	err := row.Scan(
		&e.ID, &e.Provider, &e.EventID, &e.EventType, &orderID,
		&provCheckoutID, &provPaymentID, &reportedAmt, &reportedCurr,
		&e.Payload, &headersBytes, &e.Status, &e.Attempts, &e.MaxAttempts,
		&lastErr, &workerID, &lockedAt, &leaseExp, &e.NextRetryAt,
		&e.CreatedAt, &e.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	e.OrderID = orderID
	if provCheckoutID.Valid {
		e.ProviderCheckoutID = &provCheckoutID.String
	}
	if provPaymentID.Valid {
		e.ProviderPaymentID = &provPaymentID.String
	}
	if reportedAmt.Valid {
		val := int(reportedAmt.Int64)
		e.ReportedAmount = &val
	}
	if reportedCurr.Valid {
		e.ReportedCurrency = &reportedCurr.String
	}
	if lastErr.Valid {
		e.LastError = &lastErr.String
	}
	if workerID.Valid {
		e.WorkerID = &workerID.String
	}
	if lockedAt.Valid {
		e.LockedAt = &lockedAt.Time
	}
	if leaseExp.Valid {
		e.LeaseExpiresAt = &leaseExp.Time
	}
	if len(headersBytes) > 0 {
		_ = json.Unmarshal(headersBytes, &e.Headers)
	}

	return &e, nil
}
