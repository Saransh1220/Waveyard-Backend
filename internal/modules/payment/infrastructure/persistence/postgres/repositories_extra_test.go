package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/infrastructure/persistence/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var orderCols = []string{
	"id", "user_id", "spec_id", "license_type", "amount", "currency",
	"provider", "provider_checkout_id", "provider_payment_id", "checkout_url",
	"status", "notes", "created_at", "updated_at", "expires_at",
	"report_currency", "report_fx_rate", "report_fx_as_of", "report_fx_fetched_at", "report_fx_rate_id",
}

func TestPgOrderRepository_Create_GeneratesDefaultsAndErrors(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewOrderRepository(db)
	ctx := context.Background()

	order := &domain.Order{UserID: uuid.New(), SpecID: uuid.New(), LicenseType: "Basic", Amount: 100, Currency: "INR", Provider: "razorpay", Status: domain.OrderStatusPending}
	mock.ExpectExec("INSERT INTO orders").WillReturnResult(sqlmock.NewResult(1, 1))
	require.NoError(t, repo.Create(ctx, order))
	assert.NotEqual(t, uuid.Nil, order.ID)
	assert.False(t, order.CreatedAt.IsZero())
	assert.False(t, order.UpdatedAt.IsZero())

	// exec error
	mock.ExpectExec("INSERT INTO orders").WillReturnError(errors.New("insert fail"))
	assert.EqualError(t, repo.Create(ctx, &domain.Order{}), "insert fail")

	// notes cannot be marshalled
	bad := &domain.Order{Notes: map[string]any{"c": make(chan int)}}
	assert.Error(t, repo.Create(ctx, bad))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPgOrderRepository_GetByID_Branches(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewOrderRepository(db)
	ctx := context.Background()
	id := uuid.New()

	// success with notes
	mock.ExpectQuery(`FROM orders\s+WHERE id = \$1`).WithArgs(id).WillReturnRows(
		sqlmock.NewRows(orderCols).AddRow(id, uuid.New(), uuid.New(), "Basic", 1000, "INR", "razorpay", nil, nil, nil, "pending", []byte(`{"k":"v"}`), time.Now(), time.Now(), time.Now(), nil, nil, nil, nil, nil))
	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "v", got.Notes["k"])

	// success without notes
	mock.ExpectQuery(`FROM orders\s+WHERE id = \$1`).WithArgs(id).WillReturnRows(
		sqlmock.NewRows(orderCols).AddRow(id, uuid.New(), uuid.New(), "Basic", 1000, "INR", "razorpay", nil, nil, nil, "pending", []byte{}, time.Now(), time.Now(), time.Now(), nil, nil, nil, nil, nil))
	got, err = repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Nil(t, got.Notes)

	// not found
	mock.ExpectQuery(`FROM orders\s+WHERE id = \$1`).WithArgs(id).WillReturnError(sql.ErrNoRows)
	got, err = repo.GetByID(ctx, id)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, sql.ErrNoRows)

	// other db error
	mock.ExpectQuery(`FROM orders\s+WHERE id = \$1`).WithArgs(id).WillReturnError(sql.ErrConnDone)
	got, err = repo.GetByID(ctx, id)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, sql.ErrConnDone)

	// invalid notes JSON
	mock.ExpectQuery(`FROM orders\s+WHERE id = \$1`).WithArgs(id).WillReturnRows(
		sqlmock.NewRows(orderCols).AddRow(id, uuid.New(), uuid.New(), "Basic", 1000, "INR", "razorpay", nil, nil, nil, "pending", []byte(`{not json`), time.Now(), time.Now(), time.Now(), nil, nil, nil, nil, nil))
	got, err = repo.GetByID(ctx, id)
	assert.Nil(t, got)
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPgOrderRepository_ListByProducer_Branches(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewOrderRepository(db)
	ctx := context.Background()
	pid := uuid.New()

	// main query error
	mock.ExpectQuery(`SELECT COUNT\(\*\)`).WithArgs(pid).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery(`SELECT\s+o\.id`).WithArgs(pid, 10, 0).WillReturnError(errors.New("q fail"))
	_, _, err := repo.ListByProducer(ctx, pid, 10, 0)
	assert.EqualError(t, err, "q fail")

	// scan error (wrong column count)
	mock.ExpectQuery(`SELECT COUNT\(\*\)`).WithArgs(pid).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT\s+o\.id`).WithArgs(pid, 10, 0).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	_, _, err = repo.ListByProducer(ctx, pid, 10, 0)
	assert.Error(t, err)

	// empty notes and invalid notes JSON are tolerated
	cols := []string{"id", "user_id", "spec_id", "license_type", "amount", "currency", "provider", "provider_checkout_id", "provider_payment_id", "status", "notes", "created_at", "updated_at", "expires_at", "buyer_name", "buyer_email", "spec_title"}
	mock.ExpectQuery(`SELECT COUNT\(\*\)`).WithArgs(pid).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery(`SELECT\s+o\.id`).WithArgs(pid, 10, 0).WillReturnRows(sqlmock.NewRows(cols).
		AddRow(uuid.New(), uuid.New(), uuid.New(), "Basic", 1, "INR", "razorpay", nil, nil, "paid", []byte{}, time.Now(), time.Now(), time.Now(), "A", "a@x.com", "S1").
		AddRow(uuid.New(), uuid.New(), uuid.New(), "Basic", 1, "INR", "razorpay", nil, nil, "paid", []byte(`oops`), time.Now(), time.Now(), time.Now(), "B", "b@x.com", "S2"))
	orders, total, err := repo.ListByProducer(ctx, pid, 10, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	require.Len(t, orders, 2)
	assert.Equal(t, "B", orders[1].BuyerName)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPgLicenseRepository_Create_GeneratesDefaultsAndError(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewLicenseRepository(db)
	ctx := context.Background()

	lic := &domain.License{OrderID: uuid.New(), UserID: uuid.New(), SpecID: uuid.New(), LicenseOptionID: uuid.New(), LicenseKey: "K"}
	mock.ExpectExec("INSERT INTO licenses").WillReturnResult(sqlmock.NewResult(1, 1))
	require.NoError(t, repo.Create(ctx, lic))
	assert.NotEqual(t, uuid.Nil, lic.ID)
	assert.False(t, lic.CreatedAt.IsZero())
	assert.False(t, lic.IssuedAt.IsZero())

	preset := time.Now().Add(-time.Hour)
	lic2 := &domain.License{ID: uuid.New(), CreatedAt: preset}
	mock.ExpectExec("INSERT INTO licenses").WillReturnError(errors.New("insert fail"))
	assert.EqualError(t, repo.Create(ctx, lic2), "insert fail")
	assert.True(t, lic2.CreatedAt.Equal(preset), "existing CreatedAt must be preserved")
}

func TestPgLicenseRepository_ListByUser_FiltersAndErrors(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewLicenseRepository(db)
	ctx := context.Background()
	uid := uuid.New()

	cols := []string{"id", "order_id", "user_id", "spec_id", "license_option_id", "license_type", "purchase_price", "license_key", "is_active", "is_revoked", "downloads_count", "issued_at", "created_at", "updated_at", "spec_title", "spec_image", "total_count"}

	// search + type filter -> args: user, %q%, type, limit, offset
	mock.ExpectQuery(`s\.title ILIKE \$2 AND l\.license_type = \$3 ORDER BY l\.issued_at DESC LIMIT \$4 OFFSET \$5`).
		WithArgs(uid, "%beat%", "Premium", 5, 10).
		WillReturnRows(sqlmock.NewRows(cols).AddRow(uuid.New(), uuid.New(), uid, uuid.New(), uuid.New(), "Premium", 2000, "K", true, false, 0, time.Now(), time.Now(), time.Now(), "Beat", "img", 7))
	got, total, err := repo.ListByUser(ctx, uid, 5, 10, "beat", "Premium")
	require.NoError(t, err)
	assert.Equal(t, 7, total)
	require.Len(t, got, 1)
	assert.Equal(t, "Premium", string(got[0].LicenseType))

	// only type filter
	mock.ExpectQuery(`l\.license_type = \$2 ORDER BY l\.issued_at DESC LIMIT \$3 OFFSET \$4`).
		WithArgs(uid, "Basic", 5, 0).
		WillReturnRows(sqlmock.NewRows(cols))
	got, total, err = repo.ListByUser(ctx, uid, 5, 0, "", "Basic")
	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.NotNil(t, got)
	assert.Empty(t, got)

	// only search filter
	mock.ExpectQuery(`s\.title ILIKE \$2 ORDER BY l\.issued_at DESC LIMIT \$3 OFFSET \$4`).
		WithArgs(uid, "%x%", 5, 0).
		WillReturnRows(sqlmock.NewRows(cols))
	_, _, err = repo.ListByUser(ctx, uid, 5, 0, "x", "")
	require.NoError(t, err)

	// query error
	mock.ExpectQuery(`FROM licenses l`).WillReturnError(errors.New("q fail"))
	got, total, err = repo.ListByUser(ctx, uid, 5, 0, "", "")
	assert.Nil(t, got)
	assert.Zero(t, total)
	assert.EqualError(t, err, "q fail")
	assert.NoError(t, mock.ExpectationsWereMet())
}
