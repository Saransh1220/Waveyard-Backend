package postgres_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/infrastructure/persistence/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMockDB(t *testing.T) (*sqlx.DB, sqlmock.Sqlmock, func()) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	return sqlx.NewDb(sqlDB, "sqlmock"), mock, func() { _ = sqlDB.Close() }
}

func TestPgOrderRepository_CreateAndList(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewOrderRepository(db)
	ctx := context.Background()

	id := uuid.New()
	userID := uuid.New()
	specID := uuid.New()
	razor := "order_1"
	order := &domain.Order{ID: id, UserID: userID, SpecID: specID, LicenseType: "Basic", Amount: 1000, Currency: "INR", Provider: "razorpay", ProviderCheckoutID: &razor, Status: domain.OrderStatusPending, Notes: map[string]any{"k": "v"}, ExpiresAt: time.Now().Add(time.Hour)}

	mock.ExpectExec("INSERT INTO orders").WillReturnResult(sqlmock.NewResult(1, 1))
	require.NoError(t, repo.Create(ctx, order))

	notes, _ := json.Marshal(order.Notes)
	orderCols := []string{"id", "user_id", "spec_id", "license_type", "amount", "currency", "provider", "provider_checkout_id", "provider_payment_id", "checkout_url", "status", "notes", "created_at", "updated_at", "expires_at", "report_currency", "report_fx_rate", "report_fx_as_of", "report_fx_fetched_at", "report_fx_rate_id"}
	rows := sqlmock.NewRows(orderCols).AddRow(id, userID, specID, "Basic", 1000, "INR", "razorpay", nil, nil, nil, "pending", notes, time.Now(), time.Now(), time.Now(), nil, nil, nil, nil, nil)
	mock.ExpectQuery("SELECT id, user_id, spec_id, license_type").WithArgs(id).WillReturnRows(rows)
	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, id, got.ID)

	mock.ExpectExec(`UPDATE orders SET status = \$1, updated_at = NOW\(\) WHERE id = \$2`).WithArgs(domain.OrderStatusPaid, id).WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.UpdateStatus(ctx, id, domain.OrderStatusPaid))

	mock.ExpectQuery(`FROM orders WHERE user_id = \$1 ORDER BY created_at DESC LIMIT \$2 OFFSET \$3`).WithArgs(userID, 20, 0).WillReturnError(sql.ErrConnDone)
	_, err = repo.ListByUser(ctx, userID, 20, 0)
	require.Error(t, err)

	mock.ExpectQuery(`FROM orders WHERE provider = \$1 AND provider_checkout_id = \$2`).WithArgs("razorpay", razor).WillReturnError(sql.ErrNoRows)
	_, err = repo.GetByProviderCheckoutID(ctx, "razorpay", razor)
	require.Error(t, err)
}

func TestPgOrderRepository_ListByProducerAndErrors(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewOrderRepository(db)
	ctx := context.Background()
	producerID := uuid.New()

	mock.ExpectQuery(`SELECT COUNT\(\*\)`).WithArgs(producerID).WillReturnError(sql.ErrConnDone)
	_, _, err := repo.ListByProducer(ctx, producerID, 50, 0)
	require.Error(t, err)

	mock.ExpectQuery(`SELECT COUNT\(\*\)`).WithArgs(producerID).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	notes, _ := json.Marshal(map[string]any{"x": 1})
	rows := sqlmock.NewRows([]string{"id", "user_id", "spec_id", "license_type", "amount", "currency", "provider", "provider_checkout_id", "provider_payment_id", "status", "notes", "created_at", "updated_at", "expires_at", "buyer_name", "buyer_email", "spec_title"}).
		AddRow(uuid.New(), uuid.New(), uuid.New(), "Basic", 1000, "INR", "razorpay", nil, nil, "paid", notes, time.Now(), time.Now(), time.Now(), "Buyer", "buyer@example.com", "Spec")
	mock.ExpectQuery(`SELECT\s+o\.id`).WithArgs(producerID, 50, 0).WillReturnRows(rows)
	orders, total, err := repo.ListByProducer(ctx, producerID, 50, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	require.Len(t, orders, 1)
}

func TestPgPaymentRepository_Basic(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewPaymentRepository(db)
	ctx := context.Background()
	id := uuid.New()
	orderID := uuid.New()

	p := &domain.Payment{ID: id, OrderID: orderID, Provider: "razorpay", ProviderPaymentID: "pay_1", ProviderSignature: "sig", Amount: 1000, Currency: "INR", Status: domain.PaymentStatusCaptured}
	mock.ExpectExec("INSERT INTO payments").WillReturnResult(sqlmock.NewResult(1, 1))
	require.NoError(t, repo.Create(ctx, p))

	rows := sqlmock.NewRows([]string{"id", "order_id", "provider", "provider_payment_id", "provider_signature", "amount", "currency", "status", "created_at", "updated_at"}).
		AddRow(id, orderID, "razorpay", "pay_1", "sig", 1000, "INR", "captured", time.Now(), time.Now())
	mock.ExpectQuery(`SELECT \* FROM payments WHERE id = \$1`).WithArgs(id).WillReturnRows(rows)
	_, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	rowsByOrder := sqlmock.NewRows([]string{"id", "order_id", "provider", "provider_payment_id", "provider_signature", "amount", "currency", "status", "created_at", "updated_at"}).
		AddRow(id, orderID, "razorpay", "pay_1", "sig", 1000, "INR", "captured", time.Now(), time.Now())
	mock.ExpectQuery(`SELECT \* FROM payments WHERE order_id = \$1`).WithArgs(orderID).WillReturnRows(rowsByOrder)
	_, err = repo.GetByOrderID(ctx, orderID)
	require.NoError(t, err)
	rowsByRzp := sqlmock.NewRows([]string{"id", "order_id", "provider", "provider_payment_id", "provider_signature", "amount", "currency", "status", "created_at", "updated_at"}).
		AddRow(id, orderID, "razorpay", "pay_1", "sig", 1000, "INR", "captured", time.Now(), time.Now())
	mock.ExpectQuery(`SELECT \* FROM payments WHERE provider = \$1 AND provider_payment_id = \$2`).WithArgs("razorpay", "pay_1").WillReturnRows(rowsByRzp)
	_, err = repo.GetByProviderPaymentID(ctx, "razorpay", "pay_1")
	require.NoError(t, err)
}

func TestPgLicenseRepository_Basic(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewLicenseRepository(db)
	ctx := context.Background()
	id := uuid.New()
	orderID := uuid.New()
	userID := uuid.New()
	specID := uuid.New()
	optID := uuid.New()

	license := &domain.License{ID: id, OrderID: orderID, UserID: userID, SpecID: specID, LicenseOptionID: optID, LicenseType: "Basic", PurchasePrice: 1000, LicenseKey: "LIC-1", IsActive: true}
	mock.ExpectExec("INSERT INTO licenses").WillReturnResult(sqlmock.NewResult(1, 1))
	require.NoError(t, repo.Create(ctx, license))

	rows := sqlmock.NewRows([]string{"id", "order_id", "user_id", "spec_id", "license_option_id", "license_type", "purchase_price", "license_key", "is_active", "is_revoked", "downloads_count", "issued_at", "created_at", "updated_at"}).
		AddRow(id, orderID, userID, specID, optID, "Basic", 1000, "LIC-1", true, false, 0, time.Now(), time.Now(), time.Now())
	mock.ExpectQuery(`SELECT \* FROM licenses WHERE id = \$1`).WithArgs(id).WillReturnRows(rows)
	_, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	rowsByOrder := sqlmock.NewRows([]string{"id", "order_id", "user_id", "spec_id", "license_option_id", "license_type", "purchase_price", "license_key", "is_active", "is_revoked", "downloads_count", "issued_at", "created_at", "updated_at"}).
		AddRow(id, orderID, userID, specID, optID, "Basic", 1000, "LIC-1", true, false, 0, time.Now(), time.Now(), time.Now())
	mock.ExpectQuery(`SELECT \* FROM licenses WHERE order_id = \$1`).WithArgs(orderID).WillReturnRows(rowsByOrder)
	_, err = repo.GetByOrderID(ctx, orderID)
	require.NoError(t, err)

	listRows := sqlmock.NewRows([]string{"id", "order_id", "user_id", "spec_id", "license_option_id", "license_type", "purchase_price", "license_key", "is_active", "is_revoked", "downloads_count", "issued_at", "created_at", "updated_at", "spec_title", "spec_image", "total_count"}).
		AddRow(id, orderID, userID, specID, optID, "Basic", 1000, "LIC-1", true, false, 0, time.Now(), time.Now(), time.Now(), "Track", nil, 1)
	mock.ExpectQuery(`SELECT l\.\*, s\.title as spec_title`).WillReturnRows(listRows)
	licenses, total, err := repo.ListByUser(ctx, userID, 5, 0, "", "")
	require.NoError(t, err)
	require.Len(t, licenses, 1)
	assert.Equal(t, 1, total)

	mock.ExpectExec("UPDATE licenses").WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.IncrementDownloads(ctx, id))
	mock.ExpectExec("UPDATE licenses").WithArgs("reason", id).WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.Revoke(ctx, id, "reason"))
}

func TestPgOrderRepository_MarkPaidIfPayable(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewOrderRepository(db)
	ctx := context.Background()
	id := uuid.New()
	providerPayID := "pay_test_123"

	expectedQuery := `UPDATE orders\s+SET status = \$1, provider_payment_id = \$2, updated_at = NOW\(\)\s+WHERE id = \$3 AND status IN \('pending', 'processing', 'failed'\)\s+RETURNING id`

	t.Run("order payable updates to paid and returns true", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{"id"}).AddRow(id)
		mock.ExpectQuery(expectedQuery).
			WithArgs(domain.OrderStatusPaid, providerPayID, id).
			WillReturnRows(rows)

		changed, err := repo.MarkPaidIfPayable(ctx, id, providerPayID)
		require.NoError(t, err)
		assert.True(t, changed)
	})

	t.Run("order not payable returns false without error", func(t *testing.T) {
		mock.ExpectQuery(expectedQuery).
			WithArgs(domain.OrderStatusPaid, providerPayID, id).
			WillReturnError(sql.ErrNoRows)

		changed, err := repo.MarkPaidIfPayable(ctx, id, providerPayID)
		require.NoError(t, err)
		assert.False(t, changed)
	})

	t.Run("db error returns false and error", func(t *testing.T) {
		mock.ExpectQuery(expectedQuery).
			WithArgs(domain.OrderStatusPaid, providerPayID, id).
			WillReturnError(sql.ErrConnDone)

		changed, err := repo.MarkPaidIfPayable(ctx, id, providerPayID)
		require.Error(t, err)
		assert.False(t, changed)
	})
}

func TestPgTxRunner_WithTx(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	runner := postgres.NewTxRunner(db)
	ctx := context.Background()

	t.Run("success commits transaction", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectCommit()

		err := runner.WithTx(ctx, func(tx domain.PaymentTx) error {
			assert.NotNil(t, tx.OrderRepo())
			assert.NotNil(t, tx.PaymentRepo())
			assert.NotNil(t, tx.LicenseRepo())
			return nil
		})
		require.NoError(t, err)
	})

	t.Run("fn error rolls back transaction", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectRollback()

		boom := errors.New("boom")
		err := runner.WithTx(ctx, func(tx domain.PaymentTx) error {
			return boom
		})
		assert.ErrorIs(t, err, boom)
	})

	t.Run("begin error returns error", func(t *testing.T) {
		mock.ExpectBegin().WillReturnError(sql.ErrConnDone)

		err := runner.WithTx(ctx, func(tx domain.PaymentTx) error {
			return nil
		})
		assert.ErrorIs(t, err, sql.ErrConnDone)
	})
}
