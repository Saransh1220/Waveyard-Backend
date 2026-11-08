package postgres

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
)

type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	GetContext(ctx context.Context, dest any, query string, args ...any) error
	SelectContext(ctx context.Context, dest any, query string, args ...any) error
	NamedExecContext(ctx context.Context, query string, arg any) (sql.Result, error)
}

type PgTxRunner struct {
	db *sqlx.DB
}

func NewTxRunner(db *sqlx.DB) domain.TxRunner {
	return &PgTxRunner{db: db}
}

type paymentTx struct {
	orderRepo       domain.OrderRepository
	paymentRepo     domain.PaymentRepository
	licenseRepo     domain.LicenseRepository
	idempotencyRepo domain.IdempotencyRepository
	refundRepo      domain.RefundRepository
}

func (pt *paymentTx) OrderRepo() domain.OrderRepository             { return pt.orderRepo }
func (pt *paymentTx) PaymentRepo() domain.PaymentRepository         { return pt.paymentRepo }
func (pt *paymentTx) LicenseRepo() domain.LicenseRepository         { return pt.licenseRepo }
func (pt *paymentTx) IdempotencyRepo() domain.IdempotencyRepository { return pt.idempotencyRepo }
func (pt *paymentTx) RefundRepo() domain.RefundRepository           { return pt.refundRepo }

func (r *PgTxRunner) WithTx(ctx context.Context, fn func(tx domain.PaymentTx) error) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	pt := &paymentTx{
		orderRepo:       NewOrderRepositoryWithDB(tx),
		paymentRepo:     NewPaymentRepositoryWithDB(tx),
		licenseRepo:     NewLicenseRepositoryWithDB(tx),
		idempotencyRepo: NewIdempotencyRepositoryWithDB(tx),
		refundRepo:      NewRefundRepositoryWithDB(tx),
	}

	if err := fn(pt); err != nil {
		return err
	}

	return tx.Commit()
}
