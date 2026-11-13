package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
)

type PgOrderRepository struct {
	db DBTX
}

func NewOrderRepository(db *sqlx.DB) domain.OrderRepository {
	return NewOrderRepositoryWithDB(db)
}

func NewOrderRepositoryWithDB(db DBTX) domain.OrderRepository {
	return &PgOrderRepository{db: db}
}

func (r *PgOrderRepository) Create(ctx context.Context, order *domain.Order) error {
	if order.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		order.ID = id
	}
	if order.CreatedAt.IsZero() {
		order.CreatedAt = time.Now()
	}
	order.UpdatedAt = time.Now()

	// Marshal notes to JSON for JSONB column
	notesJSON, err := json.Marshal(order.Notes)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO orders (
			id, user_id, spec_id, license_type, amount, currency,
			provider, provider_checkout_id, provider_payment_id, checkout_url,
			status, notes, created_at, updated_at, expires_at,
			report_currency, report_fx_rate, report_fx_as_of, report_fx_fetched_at, report_fx_rate_id
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
			$15, $16, $17, $18, $19, $20
		)`

	_, err = r.db.ExecContext(ctx, query,
		order.ID,
		order.UserID,
		order.SpecID,
		order.LicenseType,
		order.Amount,
		order.Currency,
		order.Provider,
		order.ProviderCheckoutID,
		order.ProviderPaymentID,
		order.CheckoutURL,
		order.Status,
		notesJSON,
		order.CreatedAt,
		order.UpdatedAt,
		order.ExpiresAt,
		order.ReportCurrency,
		order.ReportFXRate,
		order.ReportFXAsOf,
		order.ReportFXFetchedAt,
		order.ReportFXRateID,
	)
	return err
}

func (r *PgOrderRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Order, error) {
	query := `SELECT ` + orderColumns + ` FROM orders WHERE id = $1`
	return scanOrder(r.db.QueryRowContext(ctx, query, id))
}

const orderColumns = `id, user_id, spec_id, license_type, amount, currency,
	provider, provider_checkout_id, provider_payment_id, checkout_url,
	status, notes, created_at, updated_at, expires_at,
	report_currency, report_fx_rate, report_fx_as_of, report_fx_fetched_at, report_fx_rate_id`

type rowScanner interface{ Scan(dest ...any) error }

func scanOrder(row rowScanner) (*domain.Order, error) {
	order := &domain.Order{}
	var notesJSON []byte
	var checkoutURL sql.NullString
	if err := row.Scan(
		&order.ID, &order.UserID, &order.SpecID, &order.LicenseType, &order.Amount, &order.Currency,
		&order.Provider, &order.ProviderCheckoutID, &order.ProviderPaymentID, &checkoutURL,
		&order.Status, &notesJSON, &order.CreatedAt, &order.UpdatedAt, &order.ExpiresAt,
		&order.ReportCurrency, &order.ReportFXRate, &order.ReportFXAsOf, &order.ReportFXFetchedAt, &order.ReportFXRateID,
	); err != nil {
		return nil, err
	}
	if checkoutURL.Valid {
		order.CheckoutURL = &checkoutURL.String
	}
	if len(notesJSON) > 0 {
		if err := json.Unmarshal(notesJSON, &order.Notes); err != nil {
			return nil, err
		}
	}
	return order, nil
}

func (r *PgOrderRepository) GetByProviderCheckoutID(ctx context.Context, provider, checkoutID string) (*domain.Order, error) {
	query := `SELECT ` + orderColumns + ` FROM orders WHERE provider = $1 AND provider_checkout_id = $2`
	return scanOrder(r.db.QueryRowContext(ctx, query, provider, checkoutID))
}

func (r *PgOrderRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.OrderStatus) error {
	query := `UPDATE orders SET status = $1, updated_at = NOW()
		WHERE id = $2 AND status IN ('pending', 'processing', 'failed')`
	_, err := r.db.ExecContext(ctx, query, status, id)
	return err
}

func (r *PgOrderRepository) AttachCheckoutDetails(ctx context.Context, id uuid.UUID, providerCheckoutID, checkoutURL string) error {
	query := `UPDATE orders SET provider_checkout_id = $2, checkout_url = $3, updated_at = NOW() WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id, providerCheckoutID, checkoutURL)
	return err
}

func (r *PgOrderRepository) MarkPaidIfPayable(ctx context.Context, id uuid.UUID, providerPaymentID string) (bool, error) {
	query := `
		UPDATE orders
		SET status = $1, provider_payment_id = $2, updated_at = NOW()
		WHERE id = $3 AND status IN ('pending', 'processing', 'failed')
		RETURNING id`
	var updatedID uuid.UUID
	err := r.db.QueryRowContext(ctx, query, domain.OrderStatusPaid, providerPaymentID, id).Scan(&updatedID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *PgOrderRepository) MarkRefunded(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE orders SET status = $1, updated_at = NOW() WHERE id = $2`
	_, err := r.db.ExecContext(ctx, query, domain.OrderStatusRefunded, id)
	return err
}

func (r *PgOrderRepository) ListByUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]domain.Order, error) {
	query := `SELECT ` + orderColumns + ` FROM orders WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`
	rows, err := r.db.QueryContext(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	orders := []domain.Order{}
	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		orders = append(orders, *order)
	}
	return orders, rows.Err()
}

func (r *PgOrderRepository) ListByProducer(ctx context.Context, producerID uuid.UUID, limit, offset int) ([]domain.OrderWithBuyer, int, error) {
	var total int
	countQuery := `
		SELECT COUNT(*)
		FROM orders o
		JOIN specs s ON o.spec_id = s.id
		WHERE s.producer_id = $1
	`
	err := r.db.GetContext(ctx, &total, countQuery, producerID)
	if err != nil {
		return nil, 0, err
	}

	query := `
		SELECT 
			o.id,
			o.user_id,
			o.spec_id,
			o.license_type,
			o.amount,
			o.currency,
			o.provider,
			o.provider_checkout_id,
			o.provider_payment_id,
			o.status,
			o.notes,
			o.created_at,
			o.updated_at,
			o.expires_at,
			u.name as buyer_name,
			u.email as buyer_email,
			s.title as spec_title
		FROM orders o
		JOIN specs s ON o.spec_id = s.id
		JOIN users u ON o.user_id = u.id
		WHERE s.producer_id = $1
		ORDER BY o.created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.QueryContext(ctx, query, producerID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var orders []domain.OrderWithBuyer
	for rows.Next() {
		var order domain.OrderWithBuyer
		var notesJSON []byte

		err := rows.Scan(
			&order.ID, &order.UserID, &order.SpecID, &order.LicenseType, &order.Amount, &order.Currency,
			&order.Provider, &order.ProviderCheckoutID, &order.ProviderPaymentID,
			&order.Status, &notesJSON, &order.CreatedAt, &order.UpdatedAt, &order.ExpiresAt,
			&order.BuyerName, &order.BuyerEmail, &order.SpecTitle,
		)
		if err != nil {
			return nil, 0, err
		}

		if len(notesJSON) > 0 {
			_ = json.Unmarshal(notesJSON, &order.Notes)
		}

		orders = append(orders, order)
	}

	return orders, total, rows.Err()
}
