package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
)

type PgPaymentRepository struct {
	db DBTX
}

func NewPaymentRepository(db *sqlx.DB) domain.PaymentRepository {
	return NewPaymentRepositoryWithDB(db)
}

func NewPaymentRepositoryWithDB(db DBTX) domain.PaymentRepository {
	return &PgPaymentRepository{db: db}
}

func (r *PgPaymentRepository) Create(ctx context.Context, payment *domain.Payment) error {
	if payment.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		payment.ID = id
	}
	if payment.CreatedAt.IsZero() {
		payment.CreatedAt = time.Now()
	}
	payment.UpdatedAt = time.Now()
	query := `
		INSERT INTO payments (
			id, order_id, provider, provider_payment_id, provider_signature,
			amount, currency, status, method, bank, wallet, vpa,
			card_network, card_last4, email, contact,
			error_code, error_description, captured_at,
			created_at, updated_at
		) VALUES (
			:id, :order_id, :provider, :provider_payment_id, :provider_signature,
			:amount, :currency, :status, :method, :bank, :wallet, :vpa,
			:card_network, :card_last4, :email, :contact,
			:error_code, :error_description, :captured_at,
			:created_at, :updated_at
		)`
	_, err := r.db.NamedExecContext(ctx, query, payment)
	return err
}

func (r *PgPaymentRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
	payment := &domain.Payment{}
	query := `SELECT * FROM payments WHERE id = $1`
	err := r.db.GetContext(ctx, payment, query, id)
	return payment, err
}

func (r *PgPaymentRepository) GetByOrderID(ctx context.Context, orderID uuid.UUID) (*domain.Payment, error) {
	payment := &domain.Payment{}
	query := `SELECT * FROM payments WHERE order_id = $1`
	err := r.db.GetContext(ctx, payment, query, orderID)
	return payment, err
}

func (r *PgPaymentRepository) GetByProviderPaymentID(ctx context.Context, provider, providerPaymentID string) (*domain.Payment, error) {
	payment := &domain.Payment{}
	query := `SELECT * FROM payments WHERE provider = $1 AND provider_payment_id = $2`
	err := r.db.GetContext(ctx, payment, query, provider, providerPaymentID)
	return payment, err
}

func (r *PgPaymentRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.PaymentStatus) error {
	query := `UPDATE payments SET status = $1, updated_at = NOW() WHERE id = $2`
	_, err := r.db.ExecContext(ctx, query, status, id)
	return err
}
