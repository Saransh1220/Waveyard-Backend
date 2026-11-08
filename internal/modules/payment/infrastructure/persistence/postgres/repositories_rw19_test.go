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

func TestPgIdempotencyRepository(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()

	repo := postgres.NewIdempotencyRepository(db)
	ctx := context.Background()

	id := uuid.New()
	userID := uuid.New()
	orderID := uuid.New()
	pCheckoutID := "chk_123"
	checkoutURL := "https://checkout.example.com"
	now := time.Now()
	leaseExp := now.Add(30 * time.Second)

	cols := []string{
		"id", "user_id", "idempotency_key", "request_hash", "order_id",
		"checkout_state", "provider", "provider_checkout_id", "checkout_url",
		"attempt_generation", "lease_expires_at", "created_at", "updated_at",
	}

	t.Run("ReserveKey new record", func(t *testing.T) {
		k := &domain.OrderIdempotencyKey{
			ID:                 id,
			UserID:             userID,
			IdempotencyKey:     "key-1",
			RequestHash:        "hash-1",
			OrderID:            &orderID,
			CheckoutState:      domain.CheckoutStateReserved,
			Provider:           "razorpay",
			ProviderCheckoutID: &pCheckoutID,
			CheckoutURL:        &checkoutURL,
			AttemptGeneration:  1,
			LeaseExpiresAt:     &leaseExp,
			CreatedAt:          now,
		}

		mock.ExpectQuery(`INSERT INTO order_idempotency_keys`).
			WithArgs(k.ID, k.UserID, k.IdempotencyKey, k.RequestHash, k.OrderID,
				k.CheckoutState, k.Provider, k.ProviderCheckoutID, k.CheckoutURL,
				k.AttemptGeneration, k.LeaseExpiresAt, k.CreatedAt, sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows(cols).AddRow(
				id, userID, "key-1", "hash-1", &orderID,
				domain.CheckoutStateReserved, "razorpay", pCheckoutID, checkoutURL,
				1, leaseExp, now, now,
			))

		res, inserted, err := repo.ReserveKey(ctx, k)
		require.NoError(t, err)
		assert.True(t, inserted)
		assert.Equal(t, id, res.ID)
		assert.Equal(t, &pCheckoutID, res.ProviderCheckoutID)
		assert.Equal(t, &checkoutURL, res.CheckoutURL)
	})

	t.Run("ReserveKey conflict fallback to existing", func(t *testing.T) {
		k := &domain.OrderIdempotencyKey{
			UserID:         userID,
			IdempotencyKey: "key-conflict",
			RequestHash:    "hash-2",
		}

		mock.ExpectQuery(`INSERT INTO order_idempotency_keys`).
			WillReturnError(sql.ErrNoRows)

		mock.ExpectQuery(`SELECT id, user_id, idempotency_key, request_hash, order_id`).
			WithArgs(userID, "key-conflict").
			WillReturnRows(sqlmock.NewRows(cols).AddRow(
				id, userID, "key-conflict", "hash-2", nil,
				domain.CheckoutStateReserved, "razorpay", nil, nil,
				1, nil, now, now,
			))

		res, inserted, err := repo.ReserveKey(ctx, k)
		require.NoError(t, err)
		assert.False(t, inserted)
		assert.Equal(t, id, res.ID)
		assert.Nil(t, res.ProviderCheckoutID)
	})

	t.Run("ReserveKey db error", func(t *testing.T) {
		k := &domain.OrderIdempotencyKey{
			UserID:         userID,
			IdempotencyKey: "key-err",
			RequestHash:    "hash-3",
		}
		mock.ExpectQuery(`INSERT INTO order_idempotency_keys`).
			WillReturnError(errors.New("db error"))

		_, _, err := repo.ReserveKey(ctx, k)
		require.Error(t, err)
	})

	t.Run("GetByUserAndKey not found", func(t *testing.T) {
		mock.ExpectQuery(`SELECT id, user_id, idempotency_key, request_hash, order_id`).
			WithArgs(userID, "nonexistent").
			WillReturnError(sql.ErrNoRows)

		res, err := repo.GetByUserAndKey(ctx, userID, "nonexistent")
		require.NoError(t, err)
		assert.Nil(t, res)
	})

	t.Run("Claim success and stale", func(t *testing.T) {
		// Success
		mock.ExpectQuery(`UPDATE order_idempotency_keys`).
			WithArgs(id, 1).
			WillReturnRows(sqlmock.NewRows([]string{"attempt_generation"}).AddRow(2))

		claimed, nextGen, err := repo.Claim(ctx, id, 1, 30*time.Second)
		require.NoError(t, err)
		assert.True(t, claimed)
		assert.Equal(t, 2, nextGen)

		// Stale / no rows
		mock.ExpectQuery(`UPDATE order_idempotency_keys`).
			WithArgs(id, 1).
			WillReturnError(sql.ErrNoRows)

		claimed, nextGen, err = repo.Claim(ctx, id, 1, 0)
		require.NoError(t, err)
		assert.False(t, claimed)
		assert.Equal(t, 1, nextGen)

		// Error
		mock.ExpectQuery(`UPDATE order_idempotency_keys`).
			WithArgs(id, 1).
			WillReturnError(errors.New("claim err"))

		claimed, _, err = repo.Claim(ctx, id, 1, 10*time.Second)
		require.Error(t, err)
		assert.False(t, claimed)
	})

	t.Run("AttachCheckoutResult and MarkFailed", func(t *testing.T) {
		mock.ExpectExec(`UPDATE order_idempotency_keys`).
			WithArgs(id, "pchk", "curl", "ready", 2).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := repo.AttachCheckoutResult(ctx, id, 2, "pchk", "curl", "ready")
		require.NoError(t, err)

		mock.ExpectExec(`UPDATE order_idempotency_keys`).
			WithArgs(id, "failed", 2).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err = repo.MarkFailed(ctx, id, 2, "failed")
		require.NoError(t, err)
	})
}

func TestPgWebhookRepository(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()

	repo := postgres.NewWebhookRepository(db)
	ctx := context.Background()

	id := uuid.New()
	orderID := uuid.New()
	amt := 1500
	curr := "USD"
	now := time.Now()
	pChk := "chk_dodo_1"
	pPay := "pay_dodo_1"
	lastErr := "network timeout"
	worker := "w-1"

	cols := []string{
		"id", "provider", "event_id", "event_type", "order_id",
		"provider_checkout_id", "provider_payment_id", "reported_amount", "reported_currency",
		"payload", "headers", "status", "attempts", "max_attempts",
		"last_error", "worker_id", "locked_at", "lease_expires_at", "next_retry_at",
		"created_at", "updated_at",
	}

	t.Run("InsertEvent success with full fields", func(t *testing.T) {
		e := &domain.PaymentWebhookEvent{
			ID:                 id,
			Provider:           "dodo",
			EventID:            "evt_1",
			EventType:          "payment.succeeded",
			OrderID:            &orderID,
			ProviderCheckoutID: &pChk,
			ProviderPaymentID:  &pPay,
			ReportedAmount:     &amt,
			ReportedCurrency:   &curr,
			Payload:            []byte(`{"status":"succeeded"}`),
			Headers:            map[string]string{"h1": "v1"},
			Status:             domain.WebhookEventStatusQueued,
			Attempts:           0,
			MaxAttempts:        5,
			LastError:          &lastErr,
			WorkerID:           &worker,
			LockedAt:           &now,
			LeaseExpiresAt:     &now,
			NextRetryAt:        now,
			CreatedAt:          now,
		}

		mock.ExpectQuery(`INSERT INTO payment_webhook_events`).
			WillReturnRows(sqlmock.NewRows(cols).AddRow(
				id, "dodo", "evt_1", "payment.succeeded", &orderID,
				pChk, pPay, int64(amt), curr,
				e.Payload, []byte(`{"h1":"v1"}`), "queued", 0, 5,
				lastErr, worker, now, now, now, now, now,
			))

		res, inserted, err := repo.InsertEvent(ctx, e)
		require.NoError(t, err)
		assert.True(t, inserted)
		assert.Equal(t, id, res.ID)
		assert.Equal(t, &pPay, res.ProviderPaymentID)
		assert.Equal(t, &amt, res.ReportedAmount)
	})

	t.Run("InsertEvent conflict fallback", func(t *testing.T) {
		e := &domain.PaymentWebhookEvent{
			Provider:  "dodo",
			EventID:   "evt_conflict",
			EventType: "payment.succeeded",
			Payload:   []byte(`{}`),
		}

		mock.ExpectQuery(`INSERT INTO payment_webhook_events`).
			WillReturnError(sql.ErrNoRows)

		mock.ExpectQuery(`SELECT id, provider, event_id, event_type, order_id`).
			WithArgs("dodo", "evt_conflict").
			WillReturnRows(sqlmock.NewRows(cols).AddRow(
				id, "dodo", "evt_conflict", "payment.succeeded", nil,
				nil, nil, nil, nil,
				e.Payload, []byte(`{}`), "completed", 1, 5,
				nil, nil, nil, nil, now, now, now,
			))

		res, inserted, err := repo.InsertEvent(ctx, e)
		require.NoError(t, err)
		assert.False(t, inserted)
		assert.Equal(t, id, res.ID)
	})

	t.Run("GetByID found and not found", func(t *testing.T) {
		mock.ExpectQuery(`SELECT id, provider, event_id, event_type, order_id`).
			WithArgs(id).
			WillReturnRows(sqlmock.NewRows(cols).AddRow(
				id, "dodo", "evt_1", "payment.succeeded", nil,
				nil, nil, nil, nil,
				[]byte(`{}`), []byte(`{}`), "queued", 0, 5,
				nil, nil, nil, nil, now, now, now,
			))

		res, err := repo.GetByID(ctx, id)
		require.NoError(t, err)
		assert.NotNil(t, res)

		mock.ExpectQuery(`SELECT id, provider, event_id, event_type, order_id`).
			WithArgs(id).
			WillReturnError(sql.ErrNoRows)

		res, err = repo.GetByID(ctx, id)
		require.NoError(t, err)
		assert.Nil(t, res)
	})

	t.Run("ClaimNextBatch", func(t *testing.T) {
		mock.ExpectQuery(`WITH due_events AS`).
			WithArgs("worker-1").
			WillReturnRows(sqlmock.NewRows(cols).AddRow(
				id, "dodo", "evt_1", "payment.succeeded", nil,
				nil, nil, nil, nil,
				[]byte(`{}`), []byte(`{}`), "processing", 1, 5,
				nil, "worker-1", now, now, now, now, now,
			))

		events, err := repo.ClaimNextBatch(ctx, "worker-1", 0, 0)
		require.NoError(t, err)
		require.Len(t, events, 1)
		assert.Equal(t, "processing", events[0].Status)
	})

	t.Run("MarkCompleted, MarkFailed, Requeue", func(t *testing.T) {
		mock.ExpectExec(`UPDATE payment_webhook_events SET status = 'completed'`).
			WithArgs(id).
			WillReturnResult(sqlmock.NewResult(1, 1))
		require.NoError(t, repo.MarkCompleted(ctx, id))

		mock.ExpectExec(`UPDATE payment_webhook_events SET status = \$2, last_error = \$3`).
			WithArgs(id, "failed", "err msg").
			WillReturnResult(sqlmock.NewResult(1, 1))
		require.NoError(t, repo.MarkFailed(ctx, id, "err msg", 0, ""))

		mock.ExpectExec(`UPDATE payment_webhook_events SET status = 'queued'`).
			WithArgs(id).
			WillReturnResult(sqlmock.NewResult(1, 1))
		require.NoError(t, repo.Requeue(ctx, id))
	})

	t.Run("ListEvents with filters", func(t *testing.T) {
		filter := domain.WebhookEventFilter{
			Provider: "dodo",
			Status:   "queued",
			OrderID:  &orderID,
		}

		mock.ExpectQuery(`SELECT COUNT\(\*\)`).
			WithArgs("dodo", "queued", &orderID).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		mock.ExpectQuery(`SELECT id, provider, event_id, event_type, order_id`).
			WithArgs("dodo", "queued", &orderID, 20, 0).
			WillReturnRows(sqlmock.NewRows(cols).AddRow(
				id, "dodo", "evt_1", "payment.succeeded", &orderID,
				nil, nil, nil, nil,
				[]byte(`{}`), []byte(`{}`), "queued", 0, 5,
				nil, nil, nil, nil, now, now, now,
			))

		events, total, err := repo.ListEvents(ctx, filter, 0, 0)
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		require.Len(t, events, 1)
	})
}

func TestPgRefundRepository(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()

	repo := postgres.NewRefundRepository(db)
	ctx := context.Background()

	id := uuid.New()
	orderID := uuid.New()
	payID := uuid.New()
	userID := uuid.New()
	adminID := uuid.New()
	pRefID := "rfnd_123"
	notes := "customer requested"
	failReason := "insufficient balance"
	now := time.Now()

	cols := []string{
		"id", "order_id", "payment_id", "user_id", "admin_id",
		"idempotency_key", "amount", "currency", "provider",
		"provider_refund_id", "provider_payment_id", "reason", "notes",
		"status", "failure_reason", "created_at", "updated_at",
	}

	t.Run("ReserveRefund new and conflict", func(t *testing.T) {
		r := &domain.OrderRefund{
			ID:                id,
			OrderID:           orderID,
			PaymentID:         payID,
			UserID:            userID,
			AdminID:           adminID,
			IdempotencyKey:    "ref-key-1",
			Amount:            2000,
			Currency:          "INR",
			Provider:          "razorpay",
			ProviderRefundID:  &pRefID,
			ProviderPaymentID: "pay_1",
			Reason:            "fraudulent",
			Notes:             &notes,
			Status:            domain.RefundStatusRequested,
			FailureReason:     &failReason,
			CreatedAt:         now,
		}

		mock.ExpectQuery(`INSERT INTO order_refunds`).
			WillReturnRows(sqlmock.NewRows(cols).AddRow(
				id, orderID, payID, userID, adminID,
				"ref-key-1", 2000, "INR", "razorpay",
				pRefID, "pay_1", "fraudulent", notes,
				"requested", failReason, now, now,
			))

		res, inserted, err := repo.ReserveRefund(ctx, r)
		require.NoError(t, err)
		assert.True(t, inserted)
		assert.Equal(t, id, res.ID)
		assert.Equal(t, &pRefID, res.ProviderRefundID)
		assert.Equal(t, &notes, res.Notes)
		assert.Equal(t, &failReason, res.FailureReason)

		// Conflict
		mock.ExpectQuery(`INSERT INTO order_refunds`).
			WillReturnError(sql.ErrNoRows)

		mock.ExpectQuery(`SELECT id, order_id, payment_id, user_id, admin_id`).
			WithArgs("ref-key-1").
			WillReturnRows(sqlmock.NewRows(cols).AddRow(
				id, orderID, payID, userID, adminID,
				"ref-key-1", 2000, "INR", "razorpay",
				nil, "pay_1", "fraudulent", nil,
				"succeeded", nil, now, now,
			))

		res, inserted, err = repo.ReserveRefund(ctx, r)
		require.NoError(t, err)
		assert.False(t, inserted)
		assert.Equal(t, "succeeded", res.Status)
	})

	t.Run("GetByID, GetByOrderID, GetByIdempotencyKey, GetByProviderRefundID", func(t *testing.T) {
		// GetByID found
		mock.ExpectQuery(`SELECT id, order_id, payment_id.*FROM order_refunds WHERE id = \$1`).
			WithArgs(id).
			WillReturnRows(sqlmock.NewRows(cols).AddRow(
				id, orderID, payID, userID, adminID,
				"ref-key-1", 2000, "INR", "razorpay",
				pRefID, "pay_1", "fraudulent", notes,
				"requested", nil, now, now,
			))
		res, err := repo.GetByID(ctx, id)
		require.NoError(t, err)
		assert.NotNil(t, res)

		// GetByID not found
		mock.ExpectQuery(`SELECT id, order_id, payment_id.*FROM order_refunds WHERE id = \$1`).
			WithArgs(id).
			WillReturnError(sql.ErrNoRows)
		res, err = repo.GetByID(ctx, id)
		require.NoError(t, err)
		assert.Nil(t, res)

		// GetByOrderID
		mock.ExpectQuery(`SELECT id, order_id, payment_id.*FROM order_refunds WHERE order_id = \$1`).
			WithArgs(orderID).
			WillReturnRows(sqlmock.NewRows(cols).AddRow(
				id, orderID, payID, userID, adminID,
				"ref-key-1", 2000, "INR", "razorpay",
				pRefID, "pay_1", "fraudulent", notes,
				"requested", nil, now, now,
			))
		refunds, err := repo.GetByOrderID(ctx, orderID)
		require.NoError(t, err)
		require.Len(t, refunds, 1)

		// GetByIdempotencyKey
		mock.ExpectQuery(`SELECT id, order_id, payment_id.*FROM order_refunds WHERE idempotency_key = \$1`).
			WithArgs("ref-k").
			WillReturnRows(sqlmock.NewRows(cols).AddRow(
				id, orderID, payID, userID, adminID,
				"ref-k", 2000, "INR", "razorpay",
				nil, "pay_1", "fraudulent", nil,
				"requested", nil, now, now,
			))
		res, err = repo.GetByIdempotencyKey(ctx, "ref-k")
		require.NoError(t, err)
		assert.NotNil(t, res)

		// GetByProviderRefundID
		mock.ExpectQuery(`SELECT id, order_id, payment_id.*FROM order_refunds WHERE provider = \$1 AND provider_refund_id = \$2`).
			WithArgs("razorpay", pRefID).
			WillReturnRows(sqlmock.NewRows(cols).AddRow(
				id, orderID, payID, userID, adminID,
				"ref-k", 2000, "INR", "razorpay",
				pRefID, "pay_1", "fraudulent", nil,
				"succeeded", nil, now, now,
			))
		res, err = repo.GetByProviderRefundID(ctx, "razorpay", pRefID)
		require.NoError(t, err)
		assert.NotNil(t, res)
	})

	t.Run("UpdateStatus", func(t *testing.T) {
		mock.ExpectExec(`UPDATE order_refunds SET status = \$2`).
			WithArgs(id, "succeeded", &pRefID, &failReason).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := repo.UpdateStatus(ctx, id, "succeeded", &pRefID, &failReason)
		require.NoError(t, err)
	})
}

func TestPgOrderAndPaymentRepository_RW19Methods(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()

	orderRepo := postgres.NewOrderRepository(db)
	payRepo := postgres.NewPaymentRepository(db)
	ctx := context.Background()
	oid := uuid.New()
	pid := uuid.New()

	t.Run("AttachCheckoutDetails", func(t *testing.T) {
		mock.ExpectExec(`UPDATE orders SET provider_checkout_id = \$2, checkout_url = \$3`).
			WithArgs(oid, "p_chk", "https://checkout.url").
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := orderRepo.AttachCheckoutDetails(ctx, oid, "p_chk", "https://checkout.url")
		require.NoError(t, err)
	})

	t.Run("Payment UpdateStatus", func(t *testing.T) {
		mock.ExpectExec(`UPDATE payments SET status = \$1, updated_at = NOW\(\) WHERE id = \$2`).
			WithArgs(domain.PaymentStatusRefunded, pid).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := payRepo.UpdateStatus(ctx, pid, domain.PaymentStatusRefunded)
		require.NoError(t, err)
	})
}


func TestPgTxRunner_Accessors(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	runner := postgres.NewTxRunner(db)
	mock.ExpectBegin()
	mock.ExpectCommit()
	err := runner.WithTx(context.Background(), func(tx domain.PaymentTx) error {
		assert.NotNil(t, tx.OrderRepo())
		assert.NotNil(t, tx.PaymentRepo())
		assert.NotNil(t, tx.LicenseRepo())
		assert.NotNil(t, tx.IdempotencyRepo())
		assert.NotNil(t, tx.RefundRepo())
		return nil
	})
	require.NoError(t, err)
}
