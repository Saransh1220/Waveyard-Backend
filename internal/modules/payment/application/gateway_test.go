package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	authDomain "github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	catalogDomain "github.com/saransh1220/blueprint-audio/internal/modules/catalog/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// fakeGateway implements Gateway, ClientConfirmer and WebhookConfirmer so one
// fake can stand in for either real gateway.
type fakeGateway struct {
	name string

	session    domain.CheckoutSession
	createErr  error
	lastCreate domain.CheckoutRequest

	confirmation *domain.PaymentConfirmation
	confirmErr   error

	webhook    *domain.PaymentConfirmation
	webhookErr error
}

func (f *fakeGateway) Name() string { return f.name }

func (f *fakeGateway) CreateCheckout(_ context.Context, req domain.CheckoutRequest) (domain.CheckoutSession, error) {
	f.lastCreate = req
	return f.session, f.createErr
}

func (f *fakeGateway) ConfirmClientPayment(_ context.Context, _ *domain.Order, _, _ string) (*domain.PaymentConfirmation, error) {
	return f.confirmation, f.confirmErr
}

func (f *fakeGateway) ParseWebhook([]byte, map[string]string) (*domain.PaymentConfirmation, error) {
	return f.webhook, f.webhookErr
}

// checkoutOnlyGateway supports neither client confirmation nor webhooks.
type checkoutOnlyGateway struct{}

func (checkoutOnlyGateway) Name() string { return "plain" }
func (checkoutOnlyGateway) CreateCheckout(context.Context, domain.CheckoutRequest) (domain.CheckoutSession, error) {
	return domain.CheckoutSession{}, nil
}

func newFakeRegistry() *GatewayRegistry {
	r, err := NewGatewayRegistry(
		map[string]string{"INR": domain.ProviderRazorpay, "USD": domain.ProviderDodo},
		&fakeGateway{name: domain.ProviderRazorpay, session: domain.CheckoutSession{ProviderRef: "order_fake_1"}},
		&fakeGateway{name: domain.ProviderDodo, session: domain.CheckoutSession{ProviderRef: "cks_fake_1", CheckoutURL: "https://pay.example/cks_fake_1"}},
		checkoutOnlyGateway{},
	)
	if err != nil {
		panic(err)
	}
	return r
}

func fakeOf(s *paymentService, name string) *fakeGateway {
	g, err := s.gateways.ByName(name)
	if err != nil {
		panic(err)
	}
	return g.(*fakeGateway)
}

func TestGatewayRegistry(t *testing.T) {
	rz := &fakeGateway{name: "razorpay"}
	dd := &fakeGateway{name: "dodo"}

	r, err := NewGatewayRegistry(map[string]string{"INR": "razorpay", "usd": "dodo"}, rz, dd)
	require.NoError(t, err)

	g, err := r.ForCurrency(" inr ")
	require.NoError(t, err)
	assert.Same(t, rz, g)
	g, err = r.ForCurrency("USD")
	require.NoError(t, err)
	assert.Same(t, dd, g)
	_, err = r.ForCurrency("EUR")
	assert.EqualError(t, err, `no payment gateway configured for currency "EUR"`)

	g, err = r.ByName("dodo")
	require.NoError(t, err)
	assert.Same(t, dd, g)
	_, err = r.ByName("paypal")
	assert.EqualError(t, err, `unknown payment provider "paypal"`)

	_, err = NewGatewayRegistry(nil, rz, &fakeGateway{name: "razorpay"})
	assert.EqualError(t, err, `payment gateway "razorpay" registered twice`)
	_, err = NewGatewayRegistry(map[string]string{"INR": "stripe"}, rz)
	assert.EqualError(t, err, `currency INR routes to unknown payment gateway "stripe"`)
}

func TestPaymentService_CreateOrder_UsesGatewayForCurrency(t *testing.T) {
	ctx := context.Background()
	loID := uuid.New()

	t.Run("INR goes to razorpay and exposes razorpay_order_id", func(t *testing.T) {
		s, or, _, _, sf, _, _, _ := newPaymentSvc()
		specID := uuid.New()
		sf.On("FindWithLicenses", ctx, specID).Return(specWithLicense(loID, catalogDomain.ProcessingStatusCompleted, "INR"), nil).Once()
		or.On("Create", ctx, mock.AnythingOfType("*domain.Order")).Return(nil).Once()
		or.On("AttachCheckoutDetails", ctx, mock.AnythingOfType("uuid.UUID"), "order_fake_1", "").Return(nil).Once()

		order, err := s.CreateOrder(ctx, uuid.New(), specID, loID, "INR")
		require.NoError(t, err)
		assert.Equal(t, domain.ProviderRazorpay, order.Provider)
		require.NotNil(t, order.ProviderCheckoutID)
		assert.Equal(t, "order_fake_1", *order.ProviderCheckoutID)
		require.NotNil(t, order.RazorpayOrderID, "frontend still reads razorpay_order_id")
		assert.Equal(t, "order_fake_1", *order.RazorpayOrderID)
		assert.Nil(t, order.CheckoutURL)

		req := fakeOf(s, domain.ProviderRazorpay).lastCreate
		assert.Equal(t, "Track", req.SpecTitle)
		assert.Equal(t, "Basic", req.LicenseName)
		assert.Equal(t, order.ID, req.Order.ID)
	})

	t.Run("USD goes to dodo with a checkout url", func(t *testing.T) {
		s, or, _, _, sf, _, _, _ := newPaymentSvc()
		specID := uuid.New()
		sf.On("FindWithLicenses", ctx, specID).Return(specWithLicense(loID, catalogDomain.ProcessingStatusCompleted, "USD"), nil).Once()
		or.On("Create", ctx, mock.AnythingOfType("*domain.Order")).Return(nil).Once()
		or.On("AttachCheckoutDetails", ctx, mock.AnythingOfType("uuid.UUID"), "cks_fake_1", "https://pay.example/cks_fake_1").Return(nil).Once()

		order, err := s.CreateOrder(ctx, uuid.New(), specID, loID, " usd ")
		require.NoError(t, err)
		assert.Equal(t, domain.ProviderDodo, order.Provider)
		assert.Equal(t, "USD", order.Currency)
		require.NotNil(t, order.CheckoutURL)
		assert.Equal(t, "https://pay.example/cks_fake_1", *order.CheckoutURL)
		assert.Equal(t, "cks_fake_1", *order.ProviderCheckoutID)
		assert.Nil(t, order.RazorpayOrderID)
	})

	t.Run("gateway failure reserves order first and returns error", func(t *testing.T) {
		s, or, _, _, sf, _, _, _ := newPaymentSvc()
		specID := uuid.New()
		sf.On("FindWithLicenses", ctx, specID).Return(specWithLicense(loID, catalogDomain.ProcessingStatusCompleted, "INR"), nil).Once()
		or.On("Create", ctx, mock.AnythingOfType("*domain.Order")).Return(nil).Once()
		fakeOf(s, domain.ProviderRazorpay).createErr = errors.New("razorpay down")

		_, err := s.CreateOrder(ctx, uuid.New(), specID, loID, "INR")
		assert.EqualError(t, err, "razorpay down")
		or.AssertCalled(t, "Create", ctx, mock.AnythingOfType("*domain.Order"))
	})

	t.Run("repo failure is returned", func(t *testing.T) {
		s, or, _, _, sf, _, _, _ := newPaymentSvc()
		specID := uuid.New()
		sf.On("FindWithLicenses", ctx, specID).Return(specWithLicense(loID, "", ""), nil).Once()
		or.On("Create", ctx, mock.AnythingOfType("*domain.Order")).Return(errors.New("db down")).Once()
		_, err := s.CreateOrder(ctx, uuid.New(), specID, loID, "inr")
		assert.EqualError(t, err, "db down")
	})

	t.Run("missing gateway route is an error", func(t *testing.T) {
		s, _, _, _, sf, _, _, _ := newPaymentSvc()
		reg, err := NewGatewayRegistry(map[string]string{"INR": domain.ProviderRazorpay}, &fakeGateway{name: domain.ProviderRazorpay})
		require.NoError(t, err)
		s.gateways = reg
		specID := uuid.New()
		sf.On("FindWithLicenses", ctx, specID).Return(specWithLicense(loID, catalogDomain.ProcessingStatusCompleted, "USD"), nil).Once()
		_, err = s.CreateOrder(ctx, uuid.New(), specID, loID, "USD")
		assert.ErrorContains(t, err, "no payment gateway configured")
	})
}

func TestPaymentService_VerifyPayment_Preconditions(t *testing.T) {
	ctx := context.Background()

	t.Run("order missing", func(t *testing.T) {
		s, or, _, _, _, _, _, _ := newPaymentSvc()
		id := uuid.New()
		or.On("GetByID", ctx, id).Return(nil, errors.New("missing")).Once()
		_, err := s.VerifyPayment(ctx, id, uuid.Nil, "pay", "sig")
		assert.EqualError(t, err, "order not found")
	})

	t.Run("already processed", func(t *testing.T) {
		s, or, _, _, _, _, _, _ := newPaymentSvc()
		o := pendingOrder(ptr("order_1"))
		o.Status = domain.OrderStatusCancelled
		or.On("GetByID", ctx, o.ID).Return(o, nil).Once()
		_, err := s.VerifyPayment(ctx, o.ID, o.UserID, "pay", "sig")
		assert.EqualError(t, err, "order already processed")
	})

	t.Run("already paid returns existing license", func(t *testing.T) {
		s, or, _, lr, _, _, _, _ := newPaymentSvc()
		o := pendingOrder(ptr("order_1"))
		o.Status = domain.OrderStatusPaid
		expectedLicense := &domain.License{ID: uuid.New(), OrderID: o.ID}
		or.On("GetByID", ctx, o.ID).Return(o, nil).Once()
		lr.On("GetByOrderID", ctx, o.ID).Return(expectedLicense, nil).Once()
		lic, err := s.VerifyPayment(ctx, o.ID, o.UserID, "pay", "sig")
		require.NoError(t, err)
		assert.Equal(t, expectedLicense, lic)
	})

	t.Run("expired marks failed", func(t *testing.T) {
		s, or, _, _, _, _, _, _ := newPaymentSvc()
		o := pendingOrder(ptr("order_1"))
		o.ExpiresAt = time.Now().Add(-time.Hour)
		or.On("GetByID", ctx, o.ID).Return(o, nil).Once()
		or.On("UpdateStatus", ctx, o.ID, domain.OrderStatusFailed).Return(nil).Once()
		_, err := s.VerifyPayment(ctx, o.ID, o.UserID, "pay", "sig")
		assert.EqualError(t, err, "order expired")
	})

	t.Run("expired and status update fails", func(t *testing.T) {
		s, or, _, _, _, _, _, _ := newPaymentSvc()
		o := pendingOrder(ptr("order_1"))
		o.ExpiresAt = time.Now().Add(-time.Hour)
		or.On("GetByID", ctx, o.ID).Return(o, nil).Once()
		or.On("UpdateStatus", ctx, o.ID, domain.OrderStatusFailed).Return(errors.New("upd")).Once()
		_, err := s.VerifyPayment(ctx, o.ID, o.UserID, "pay", "sig")
		assert.EqualError(t, err, "order expired and status update failed: upd")
	})

	t.Run("unknown provider on the order", func(t *testing.T) {
		s, or, _, _, _, _, _, _ := newPaymentSvc()
		o := pendingOrder(ptr("x"))
		o.Provider = "paypal"
		or.On("GetByID", ctx, o.ID).Return(o, nil).Once()
		_, err := s.VerifyPayment(ctx, o.ID, o.UserID, "pay", "sig")
		assert.EqualError(t, err, `unknown payment provider "paypal"`)
	})

	t.Run("gateway that cannot confirm from the client", func(t *testing.T) {
		s, or, _, _, _, _, _, _ := newPaymentSvc()
		o := pendingOrder(ptr("x"))
		o.Provider = "plain"
		or.On("GetByID", ctx, o.ID).Return(o, nil).Once()
		_, err := s.VerifyPayment(ctx, o.ID, o.UserID, "pay", "sig")
		assert.ErrorIs(t, err, domain.ErrProviderCompletesByWebhook)
		assert.Contains(t, err.Error(), "plain payments are confirmed by webhook, not by the client")
	})
}

func TestPaymentService_VerifyPayment_GatewayRejections(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name       string
		gatewayErr error
		wantMarked bool
		wantMsg    string
	}{
		{"invalid signature fails the order", domain.ErrInvalidSignature, true, "invalid signature"},
		{"payment not captured fails the order", domain.ErrPaymentNotCaptured, true, "payment not captured"},
		{"other gateway errors leave the order pending", errors.New("failed to fetch payment: boom"), false, "failed to fetch payment: boom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, or, _, _, _, _, _, _ := newPaymentSvc()
			o := pendingOrder(ptr("order_1"))
			fakeOf(s, domain.ProviderRazorpay).confirmErr = tc.gatewayErr
			or.On("GetByID", ctx, o.ID).Return(o, nil).Once()
			if tc.wantMarked {
				or.On("UpdateStatus", ctx, o.ID, domain.OrderStatusFailed).Return(nil).Once()
			}
			_, err := s.VerifyPayment(ctx, o.ID, o.UserID, "pay", "sig")
			assert.EqualError(t, err, tc.wantMsg)
			if !tc.wantMarked {
				or.AssertNotCalled(t, "UpdateStatus")
			}
		})
	}

	t.Run("status update failure is reported with the cause", func(t *testing.T) {
		s, or, _, _, _, _, _, _ := newPaymentSvc()
		o := pendingOrder(ptr("order_1"))
		fakeOf(s, domain.ProviderRazorpay).confirmErr = domain.ErrInvalidSignature
		or.On("GetByID", ctx, o.ID).Return(o, nil).Once()
		or.On("UpdateStatus", ctx, o.ID, domain.OrderStatusFailed).Return(errors.New("upd2")).Once()
		_, err := s.VerifyPayment(ctx, o.ID, o.UserID, "pay", "wrong")
		assert.EqualError(t, err, "invalid signature and status update failed: upd2")
		assert.ErrorIs(t, err, domain.ErrInvalidSignature)
	})
}

func TestPaymentService_VerifyPayment_SuccessStoresProviderNeutralPaymentAndSendsReceipt(t *testing.T) {
	ctx := context.Background()
	s, or, pr, lr, _, _, uf, es := newPaymentSvc()
	o := pendingOrder(ptr("order_9"))
	method, email := "upi", "pay@example.com"
	fakeOf(s, domain.ProviderRazorpay).confirmation = &domain.PaymentConfirmation{
		OrderID: o.ID, ProviderPaymentID: "pay_9", Signature: "sig_9", Method: &method, Email: &email,
	}

	or.On("GetByID", ctx, o.ID).Return(o, nil).Once()
	var stored *domain.Payment
	pr.On("Create", ctx, mock.AnythingOfType("*domain.Payment")).Run(func(a mock.Arguments) { stored = a.Get(1).(*domain.Payment) }).Return(nil).Once()
	or.On("MarkPaidIfPayable", ctx, o.ID, "pay_9").Return(true, nil).Once()
	lr.On("Create", ctx, mock.AnythingOfType("*domain.License")).Return(nil).Once()
	uf.On("FindByID", mock.Anything, o.UserID).Return(&authDomain.User{ID: o.UserID, Email: "acct@example.com", Name: "N"}, nil).Once()
	sent := make(chan struct{}, 1)
	es.On("Send", mock.Anything, mock.AnythingOfType("email.Message")).Run(func(mock.Arguments) { sent <- struct{}{} }).Return(nil).Once()

	license, err := s.VerifyPayment(ctx, o.ID, o.UserID, "pay_9", "sig_9")
	require.NoError(t, err)
	require.NotNil(t, license)

	require.NotNil(t, stored)
	assert.Equal(t, domain.ProviderRazorpay, stored.Provider)
	assert.Equal(t, "pay_9", stored.ProviderPaymentID)
	assert.Equal(t, "sig_9", stored.ProviderSignature)
	assert.Equal(t, domain.PaymentStatusCaptured, stored.Status)
	assert.Equal(t, &method, stored.Method)
	assert.Equal(t, &email, stored.Email)
	assert.NotNil(t, stored.CapturedAt)

	select {
	case <-sent:
	case <-time.After(2 * time.Second):
		t.Fatal("receipt email was not sent")
	}
}

func TestPaymentService_FinalizePayment_FailureBranches(t *testing.T) {
	ctx := context.Background()
	setup := func() (*paymentService, *orderRepoMock, *paymentRepoMock, *licenseRepoMock, *domain.Order, *domain.PaymentConfirmation) {
		s, or, pr, lr, _, _, _, _ := newPaymentSvc()
		s.emailSender = nil
		o := pendingOrder(ptr("order_1"))
		return s, or, pr, lr, o, &domain.PaymentConfirmation{OrderID: o.ID, ProviderPaymentID: "pay_1"}
	}

	t.Run("mark paid fails", func(t *testing.T) {
		s, or, _, _, o, c := setup()
		or.On("MarkPaidIfPayable", ctx, o.ID, c.ProviderPaymentID).Return(false, errors.New("paid err")).Once()
		_, err := s.finalizePayment(ctx, o, c)
		assert.EqualError(t, err, "paid err")
	})
	t.Run("payment insert fails", func(t *testing.T) {
		s, or, pr, _, o, c := setup()
		or.On("MarkPaidIfPayable", ctx, o.ID, c.ProviderPaymentID).Return(true, nil).Once()
		pr.On("Create", ctx, mock.AnythingOfType("*domain.Payment")).Return(errors.New("pay db")).Once()
		_, err := s.finalizePayment(ctx, o, c)
		assert.EqualError(t, err, "pay db")
	})
	t.Run("license fails after payment", func(t *testing.T) {
		s, or, pr, lr, o, c := setup()
		or.On("MarkPaidIfPayable", ctx, o.ID, c.ProviderPaymentID).Return(true, nil).Once()
		pr.On("Create", ctx, mock.AnythingOfType("*domain.Payment")).Return(nil).Once()
		lr.On("Create", ctx, mock.AnythingOfType("*domain.License")).Return(errors.New("lic err")).Once()
		_, err := s.finalizePayment(ctx, o, c)
		assert.EqualError(t, err, "payment ok but license failed: lic err")
	})
}

func TestPaymentService_HandleWebhook(t *testing.T) {
	ctx := context.Background()
	oid := uuid.New()
	loID := uuid.New()
	confirm := func() *domain.PaymentConfirmation {
		return &domain.PaymentConfirmation{OrderID: oid, ProviderPaymentID: "pay_dodo_1", Signature: "v1,abc"}
	}
	order := func(status domain.OrderStatus, provider string, notes map[string]any) *domain.Order {
		return &domain.Order{ID: oid, UserID: uuid.New(), SpecID: uuid.New(), Status: status, Provider: provider, Amount: 500, Currency: "USD", LicenseType: "Unlimited", Notes: notes}
	}

	t.Run("unknown provider", func(t *testing.T) {
		s, _, _, _, _, _, _, _ := newPaymentSvc()
		assert.EqualError(t, s.HandleWebhook(ctx, "paypal", nil, nil), `unknown payment provider "paypal"`)
	})
	t.Run("provider without webhooks", func(t *testing.T) {
		s, _, _, _, _, _, _, _ := newPaymentSvc()
		assert.EqualError(t, s.HandleWebhook(ctx, "plain", nil, nil), "plain does not use webhooks")
	})
	t.Run("parse error is returned", func(t *testing.T) {
		s, _, _, _, _, _, _, _ := newPaymentSvc()
		fakeOf(s, domain.ProviderDodo).webhookErr = errors.New("invalid dodo webhook signature")
		assert.EqualError(t, s.HandleWebhook(ctx, domain.ProviderDodo, nil, nil), "invalid dodo webhook signature")
	})
	t.Run("ignored event does nothing", func(t *testing.T) {
		s, or, _, _, _, _, _, _ := newPaymentSvc()
		assert.NoError(t, s.HandleWebhook(ctx, domain.ProviderDodo, nil, nil))
		or.AssertNotCalled(t, "GetByID")
	})
	t.Run("order not found", func(t *testing.T) {
		s, or, _, _, _, _, _, _ := newPaymentSvc()
		fakeOf(s, domain.ProviderDodo).webhook = confirm()
		or.On("GetByID", ctx, oid).Return(nil, errors.New("nf")).Once()
		assert.EqualError(t, s.HandleWebhook(ctx, domain.ProviderDodo, nil, nil), "order not found")
	})
	t.Run("order belongs to another provider", func(t *testing.T) {
		s, or, pr, _, _, _, _, _ := newPaymentSvc()
		fakeOf(s, domain.ProviderDodo).webhook = confirm()
		or.On("GetByID", ctx, oid).Return(order(domain.OrderStatusPending, domain.ProviderRazorpay, nil), nil).Once()
		assert.ErrorIs(t, s.HandleWebhook(ctx, domain.ProviderDodo, nil, nil), domain.ErrProviderMismatch)
		pr.AssertNotCalled(t, "Create")
	})
	t.Run("already paid is idempotent", func(t *testing.T) {
		s, or, pr, _, _, _, _, _ := newPaymentSvc()
		fakeOf(s, domain.ProviderDodo).webhook = confirm()
		or.On("GetByID", ctx, oid).Return(order(domain.OrderStatusPaid, domain.ProviderDodo, nil), nil).Once()
		pr.On("GetByOrderID", ctx, oid).Return(&domain.Payment{OrderID: oid, ProviderPaymentID: "pay_dodo_1"}, nil).Once()
		assert.NoError(t, s.HandleWebhook(ctx, domain.ProviderDodo, nil, nil))
		pr.AssertNotCalled(t, "Create")
	})
	t.Run("already paid with distinct payment ID flags capture anomaly", func(t *testing.T) {
		s, or, pr, _, _, _, _, _ := newPaymentSvc()
		fakeOf(s, domain.ProviderDodo).webhook = confirm()
		or.On("GetByID", ctx, oid).Return(order(domain.OrderStatusPaid, domain.ProviderDodo, nil), nil).Once()
		pr.On("GetByOrderID", ctx, oid).Return(&domain.Payment{OrderID: oid, ProviderPaymentID: "pay_dodo_other"}, nil).Once()
		assert.ErrorIs(t, s.HandleWebhook(ctx, domain.ProviderDodo, nil, nil), domain.ErrDistinctCaptureAnomaly)
		pr.AssertNotCalled(t, "Create")
	})
	t.Run("not payable", func(t *testing.T) {
		s, or, _, _, _, _, _, _ := newPaymentSvc()
		fakeOf(s, domain.ProviderDodo).webhook = confirm()
		or.On("GetByID", ctx, oid).Return(order(domain.OrderStatusCancelled, domain.ProviderDodo, nil), nil).Once()
		assert.EqualError(t, s.HandleWebhook(ctx, domain.ProviderDodo, nil, nil), "order is not payable")
	})
	t.Run("finalize error is returned", func(t *testing.T) {
		s, or, pr, _, _, _, _, _ := newPaymentSvc()
		fakeOf(s, domain.ProviderDodo).webhook = confirm()
		or.On("GetByID", ctx, oid).Return(order(domain.OrderStatusPending, domain.ProviderDodo, nil), nil).Once()
		or.On("MarkPaidIfPayable", ctx, oid, "pay_dodo_1").Return(true, nil).Once()
		pr.On("Create", ctx, mock.AnythingOfType("*domain.Payment")).Return(errors.New("pay err")).Once()
		assert.EqualError(t, s.HandleWebhook(ctx, domain.ProviderDodo, nil, nil), "pay err")
	})

	t.Run("success records a dodo payment, issues the license and now sends the receipt", func(t *testing.T) {
		s, or, pr, lr, _, _, uf, es := newPaymentSvc()
		fakeOf(s, domain.ProviderDodo).webhook = confirm()
		o := order(domain.OrderStatusPending, domain.ProviderDodo, map[string]any{"license_option_id": loID.String(), "spec_title": "Track"})
		or.On("GetByID", ctx, oid).Return(o, nil).Once()
		pr.On("Create", ctx, mock.MatchedBy(func(p *domain.Payment) bool {
			return p.OrderID == oid && p.Provider == domain.ProviderDodo &&
				p.ProviderPaymentID == "pay_dodo_1" && p.ProviderSignature == "v1,abc" &&
				p.Status == domain.PaymentStatusCaptured && p.CapturedAt != nil
		})).Return(nil).Once()
		or.On("MarkPaidIfPayable", ctx, oid, "pay_dodo_1").Return(true, nil).Once()
		lr.On("Create", ctx, mock.MatchedBy(func(l *domain.License) bool {
			return l.OrderID == oid && l.LicenseOptionID == loID && l.Currency == "USD"
		})).Return(nil).Once()
		uf.On("FindByID", mock.Anything, o.UserID).Return(&authDomain.User{ID: o.UserID, Email: "buyer@example.com", Name: "Buyer"}, nil).Once()
		sent := make(chan struct{}, 1)
		es.On("Send", mock.Anything, mock.AnythingOfType("email.Message")).Run(func(mock.Arguments) { sent <- struct{}{} }).Return(nil).Once()

		require.NoError(t, s.HandleWebhook(ctx, domain.ProviderDodo, nil, nil))
		select {
		case <-sent:
		case <-time.After(2 * time.Second):
			t.Fatal("dodo purchases must send the same receipt email as razorpay ones")
		}
	})
}
