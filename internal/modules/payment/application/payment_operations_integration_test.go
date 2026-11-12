package application

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	catalog "github.com/saransh1220/blueprint-audio/internal/modules/catalog/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
	pg "github.com/saransh1220/blueprint-audio/internal/modules/payment/infrastructure/persistence/postgres"
	"github.com/saransh1220/blueprint-audio/internal/shared/money"
	"github.com/stretchr/testify/require"
)

type countingCheckoutGateway struct {
	writes atomic.Int32
}

func (g *countingCheckoutGateway) Name() string { return domain.ProviderRazorpay }

func (g *countingCheckoutGateway) CreateCheckout(_ context.Context, req domain.CheckoutRequest) (domain.CheckoutSession, error) {
	g.writes.Add(1)
	return domain.CheckoutSession{ProviderRef: req.Order.ID.String()}, nil
}

func TestPaymentOperationConcurrency(t *testing.T) {
	dsn := os.Getenv("TEST_PAYMENT_DB_URL")
	if dsn == "" {
		t.Skip("TEST_PAYMENT_DB_URL must point to a disposable, migrated PostgreSQL database")
	}
	db, err := sqlx.Connect("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	userID, specID, licenseID := uuid.New(), uuid.New(), uuid.New()
	_, err = db.ExecContext(ctx, `INSERT INTO users(id,email,password_hash,name,role,display_name)
		VALUES($1,$2,'hash','Test','producer','Test')`, userID, userID.String()+"@example.test")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := db.Exec(`DELETE FROM users WHERE id = $1`, userID)
		require.NoError(t, err)
	})
	_, err = db.ExecContext(ctx, `INSERT INTO specs(id,producer_id,title,category,type,bpm,key,base_price,image_url,preview_url)
		VALUES($1,$2,'Track','beat','WAV',120,'C',10,'image','preview')`, specID, userID)
	require.NoError(t, err)

	gateway := &countingCheckoutGateway{}
	registry, err := NewGatewayRegistry(map[string]string{"INR": gateway.Name()}, gateway)
	require.NoError(t, err)
	service := &paymentService{
		orderRepo:       pg.NewOrderRepository(db),
		idempotencyRepo: pg.NewIdempotencyRepository(db),
		txRunner:        pg.NewTxRunner(db),
		gateways:        registry,
	}
	spec := &catalog.Spec{Title: "Track"}
	license := &catalog.LicenseOption{LicenseType: catalog.LicenseBasic}
	offer := money.SelectedPriceOffer{AmountMinor: 1000, Currency: "INR"}
	input := CreateOrderInput{UserID: userID, SpecID: specID, LicenseOptionID: licenseID, IdempotencyKey: uuid.NewString()}
	fingerprint := ComputeCanonicalFingerprint(input)

	// Both calls are past the optimistic lookup when they compete for the key.
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = service.createOrderFromSpec(ctx, spec, license, offer, input, fingerprint)
		}()
	}
	close(start)
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			require.ErrorIs(t, err, domain.ErrOrderReservedInFlight)
		}
	}
	require.EqualValues(t, 1, gateway.writes.Load())
	var orderCount int
	require.NoError(t, db.GetContext(ctx, &orderCount, `SELECT count(*) FROM orders WHERE user_id = $1`, userID))
	require.Equal(t, 1, orderCount)
	order, err := service.CreateOrderWithOffer(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, order.RazorpayOrderID)

	// Competing refund keys must serialize independently of the checkout key.
	paymentID := uuid.New()
	_, err = db.ExecContext(ctx, `INSERT INTO payments(id,order_id,provider_payment_id,provider_signature,amount,provider)
		VALUES($1,$2,$3,'signature',1000,'razorpay')`, paymentID, order.ID, paymentID.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := db.Exec(`DELETE FROM order_refunds WHERE payment_id = $1`, paymentID)
		require.NoError(t, err)
	})
	refunds := pg.NewRefundRepository(db)
	start = make(chan struct{})
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, errs[i] = refunds.ReserveRefund(ctx, &domain.OrderRefund{
				OrderID: order.ID, PaymentID: paymentID, UserID: userID, AdminID: userID,
				IdempotencyKey: uuid.NewString(), Amount: 1000, Currency: "INR",
				Provider: gateway.Name(), ProviderPaymentID: paymentID.String(), Reason: "duplicate",
			})
		}()
	}
	close(start)
	wg.Wait()
	if errs[0] == nil {
		require.ErrorIs(t, errs[1], domain.ErrRefundAlreadyExists)
	} else {
		require.ErrorIs(t, errs[0], domain.ErrRefundAlreadyExists)
		require.NoError(t, errs[1])
	}

	// External partial refunds only revoke access once the captured total is returned.
	_, err = db.ExecContext(ctx, `INSERT INTO license_options(id,spec_id,license_type,name,price,features,file_types)
		VALUES($1,$2,'Basic','Basic',10,'{}','{}')`, licenseID, specID)
	require.NoError(t, err)
	licenses := pg.NewLicenseRepository(db)
	issued, err := service.issueLicenseWithRepo(ctx, order, licenses)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := db.Exec(`DELETE FROM licenses WHERE id = $1`, issued.ID)
		require.NoError(t, err)
	})
	_, err = db.ExecContext(ctx, `UPDATE payments SET status='captured', currency='INR' WHERE id=$1`, paymentID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE orders SET status='paid' WHERE id=$1`, order.ID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := db.Exec(`DELETE FROM payment_provider_refunds WHERE payment_id = $1`, paymentID)
		require.NoError(t, err)
		_, err = db.Exec(`DELETE FROM payment_webhook_events WHERE provider_payment_id = $1`, paymentID.String())
		require.NoError(t, err)
	})
	service.refundRepo = refunds
	service.webhookRepo = pg.NewWebhookRepository(db)
	first := domain.RefundConfirmation{
		ProviderRefundID: uuid.NewString(), Amount: 400, Currency: "INR", Status: domain.RefundStatusSucceeded,
	}
	require.NoError(t, service.reconcileRefund(ctx, domain.ProviderRazorpay, paymentID.String(), first))
	require.NoError(t, service.reconcileRefund(ctx, domain.ProviderRazorpay, paymentID.String(), first))
	licenseBefore, err := licenses.GetByID(ctx, issued.ID)
	require.NoError(t, err)
	require.False(t, licenseBefore.IsRevoked)
	hasRefund, err := refunds.HasProviderRefund(ctx, paymentID)
	require.NoError(t, err)
	require.True(t, hasRefund)

	second := domain.RefundConfirmation{
		ProviderRefundID: uuid.NewString(), Amount: 600, Currency: "INR", Status: domain.RefundStatusSucceeded,
	}
	require.NoError(t, service.enqueueRefund(ctx, domain.ProviderRazorpay, &domain.PaymentConfirmation{
		ProviderPaymentID: paymentID.String(), Refund: &second,
	}, []byte("{}")))
	processed, err := service.ProcessNextWebhookBatch(ctx, "test-refund", 1, time.Minute)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	refunded, err := service.orderRepo.GetByID(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, domain.OrderStatusRefunded, refunded.Status)
	licenseAfter, err := licenses.GetByID(ctx, issued.ID)
	require.NoError(t, err)
	require.True(t, licenseAfter.IsRevoked)
}

func TestWebhookLeaseRecovery(t *testing.T) {
	dsn := os.Getenv("TEST_PAYMENT_DB_URL")
	if dsn == "" {
		t.Skip("TEST_PAYMENT_DB_URL must point to a disposable, migrated PostgreSQL database")
	}
	db, err := sqlx.Connect("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	inbox := pg.NewWebhookRepository(db)
	unknownOrder := uuid.New()
	event, inserted, err := inbox.InsertEvent(ctx, &domain.PaymentWebhookEvent{
		Provider: domain.ProviderDodo, EventID: uuid.NewString(), EventType: "payment.captured",
		OrderID: &unknownOrder, Payload: []byte("{}"), MaxAttempts: 2,
	})
	require.NoError(t, err)
	require.True(t, inserted)
	defer func() {
		_, err := db.Exec(`DELETE FROM payment_webhook_events WHERE id = $1`, event.ID)
		require.NoError(t, err)
	}()

	first, err := inbox.ClaimNextBatch(ctx, "first", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.Equal(t, event.ID, first[0].ID)
	_, err = db.ExecContext(ctx, `UPDATE payment_webhook_events SET lease_expires_at = NOW() WHERE id = $1`, event.ID)
	require.NoError(t, err)

	second, err := inbox.ClaimNextBatch(ctx, "second", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.Equal(t, event.ID, second[0].ID)
	require.Equal(t, 2, second[0].Attempts)

	require.NoError(t, inbox.MarkCompleted(ctx, event.ID, first[0].LockedAt))
	current, err := inbox.GetByID(ctx, event.ID)
	require.NoError(t, err)
	require.Equal(t, domain.WebhookEventStatusProcessing, current.Status)
	require.NoError(t, inbox.MarkCompleted(ctx, event.ID, second[0].LockedAt))
	current, err = inbox.GetByID(ctx, event.ID)
	require.NoError(t, err)
	require.Equal(t, domain.WebhookEventStatusCompleted, current.Status)
}
