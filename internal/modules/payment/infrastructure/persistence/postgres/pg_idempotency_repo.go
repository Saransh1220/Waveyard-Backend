package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
)

type PgIdempotencyRepository struct {
	db DBTX
}

func NewIdempotencyRepository(db *sqlx.DB) domain.IdempotencyRepository {
	return NewIdempotencyRepositoryWithDB(db)
}

func NewIdempotencyRepositoryWithDB(db DBTX) domain.IdempotencyRepository {
	return &PgIdempotencyRepository{db: db}
}

func (r *PgIdempotencyRepository) ReserveKey(ctx context.Context, k *domain.OrderIdempotencyKey) (*domain.OrderIdempotencyKey, bool, error) {
	if k.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return nil, false, err
		}
		k.ID = id
	}
	if k.CreatedAt.IsZero() {
		k.CreatedAt = time.Now()
	}
	k.UpdatedAt = time.Now()
	if k.AttemptGeneration == 0 {
		k.AttemptGeneration = 1
	}
	if k.CheckoutState == "" {
		k.CheckoutState = domain.CheckoutStateReserved
	}

	query := `
		INSERT INTO order_idempotency_keys (
			id, user_id, idempotency_key, request_hash, order_id,
			checkout_state, provider, provider_checkout_id, checkout_url,
			attempt_generation, lease_expires_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
		)
		ON CONFLICT (user_id, idempotency_key) DO NOTHING
		RETURNING id, user_id, idempotency_key, request_hash, order_id,
			checkout_state, provider, provider_checkout_id, checkout_url,
			attempt_generation, lease_expires_at, created_at, updated_at
	`

	var existing domain.OrderIdempotencyKey
	var leaseExp sql.NullTime
	var checkoutID, checkoutURL sql.NullString
	var orderID *uuid.UUID

	err := r.db.QueryRowContext(ctx, query,
		k.ID, k.UserID, k.IdempotencyKey, k.RequestHash, k.OrderID,
		k.CheckoutState, k.Provider, k.ProviderCheckoutID, k.CheckoutURL,
		k.AttemptGeneration, k.LeaseExpiresAt, k.CreatedAt, k.UpdatedAt,
	).Scan(
		&existing.ID, &existing.UserID, &existing.IdempotencyKey, &existing.RequestHash, &orderID,
		&existing.CheckoutState, &existing.Provider, &checkoutID, &checkoutURL,
		&existing.AttemptGeneration, &leaseExp, &existing.CreatedAt, &existing.UpdatedAt,
	)

	if err == nil {
		existing.OrderID = orderID
		if checkoutID.Valid {
			existing.ProviderCheckoutID = &checkoutID.String
		}
		if checkoutURL.Valid {
			existing.CheckoutURL = &checkoutURL.String
		}
		if leaseExp.Valid {
			existing.LeaseExpiresAt = &leaseExp.Time
		}
		return &existing, true, nil
	}

	if errors.Is(err, sql.ErrNoRows) {
		// Conflict happened, fetch existing row
		found, getErr := r.GetByUserAndKey(ctx, k.UserID, k.IdempotencyKey)
		if getErr != nil {
			return nil, false, getErr
		}
		return found, false, nil
	}

	return nil, false, err
}

func (r *PgIdempotencyRepository) GetByUserAndKey(ctx context.Context, userID uuid.UUID, key string) (*domain.OrderIdempotencyKey, error) {
	query := `
		SELECT id, user_id, idempotency_key, request_hash, order_id,
			checkout_state, provider, provider_checkout_id, checkout_url,
			attempt_generation, lease_expires_at, created_at, updated_at
		FROM order_idempotency_keys
		WHERE user_id = $1 AND idempotency_key = $2
	`
	var k domain.OrderIdempotencyKey
	var leaseExp sql.NullTime
	var checkoutID, checkoutURL sql.NullString
	var orderID *uuid.UUID

	err := r.db.QueryRowContext(ctx, query, userID, key).Scan(
		&k.ID, &k.UserID, &k.IdempotencyKey, &k.RequestHash, &orderID,
		&k.CheckoutState, &k.Provider, &checkoutID, &checkoutURL,
		&k.AttemptGeneration, &leaseExp, &k.CreatedAt, &k.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	k.OrderID = orderID
	if checkoutID.Valid {
		k.ProviderCheckoutID = &checkoutID.String
	}
	if checkoutURL.Valid {
		k.CheckoutURL = &checkoutURL.String
	}
	if leaseExp.Valid {
		k.LeaseExpiresAt = &leaseExp.Time
	}
	return &k, nil
}

func (r *PgIdempotencyRepository) Claim(ctx context.Context, id uuid.UUID, currentGen int, leaseDuration time.Duration) (bool, int, error) {
	leaseSeconds := int(leaseDuration.Seconds())
	if leaseSeconds <= 0 {
		leaseSeconds = 30
	}
	query := fmt.Sprintf(`
		UPDATE order_idempotency_keys
		SET checkout_state = '%s',
			attempt_generation = attempt_generation + 1,
			lease_expires_at = NOW() + INTERVAL '%d seconds',
			updated_at = NOW()
		WHERE id = $1 AND attempt_generation = $2 AND checkout_state = 'reserved'
		RETURNING attempt_generation
	`, domain.CheckoutStateClaiming, leaseSeconds)

	var nextGen int
	err := r.db.QueryRowContext(ctx, query, id, currentGen).Scan(&nextGen)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, currentGen, nil
		}
		return false, currentGen, err
	}
	return true, nextGen, nil
}

func (r *PgIdempotencyRepository) AttachCheckoutResult(ctx context.Context, id uuid.UUID, gen int, providerCheckoutID, checkoutURL, state string) error {
	query := `
		UPDATE order_idempotency_keys
		SET provider_checkout_id = $2,
			checkout_url = $3,
			checkout_state = $4,
			updated_at = NOW()
		WHERE id = $1 AND attempt_generation = $5 AND checkout_state = 'claiming'
	`
	result, err := r.db.ExecContext(ctx, query, id, providerCheckoutID, checkoutURL, state, gen)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err == nil && rows == 0 {
		return domain.ErrOrderReservedInFlight
	}
	return err
}

func (r *PgIdempotencyRepository) MarkFailed(ctx context.Context, id uuid.UUID, gen int, state string) error {
	query := `
		UPDATE order_idempotency_keys
		SET checkout_state = $2,
			updated_at = NOW()
		WHERE id = $1 AND attempt_generation = $3
	`
	_, err := r.db.ExecContext(ctx, query, id, state, gen)
	return err
}
