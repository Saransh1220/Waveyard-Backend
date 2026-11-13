package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	catalogDomain "github.com/saransh1220/blueprint-audio/internal/modules/catalog/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
	sharedmoney "github.com/saransh1220/blueprint-audio/internal/shared/money"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type idempotencyRepoMock struct {
	mock.Mock
}

func (m *idempotencyRepoMock) GetByOrderID(ctx context.Context, orderID uuid.UUID) (*domain.OrderIdempotencyKey, error) {
	args := m.Called(ctx, orderID)
	key, _ := args.Get(0).(*domain.OrderIdempotencyKey)
	return key, args.Error(1)
}

func (m *idempotencyRepoMock) ReserveKey(ctx context.Context, k *domain.OrderIdempotencyKey) (*domain.OrderIdempotencyKey, bool, error) {
	args := m.Called(ctx, k)
	if args.Get(0) == nil {
		return nil, args.Bool(1), args.Error(2)
	}
	return args.Get(0).(*domain.OrderIdempotencyKey), args.Bool(1), args.Error(2)
}

func (m *idempotencyRepoMock) GetByUserAndKey(ctx context.Context, userID uuid.UUID, key string) (*domain.OrderIdempotencyKey, error) {
	args := m.Called(ctx, userID, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.OrderIdempotencyKey), args.Error(1)
}

func (m *idempotencyRepoMock) Claim(ctx context.Context, id uuid.UUID, currentGen int, leaseDuration time.Duration) (bool, int, error) {
	args := m.Called(ctx, id, currentGen, leaseDuration)
	return args.Bool(0), args.Int(1), args.Error(2)
}

func (m *idempotencyRepoMock) AttachCheckoutResult(ctx context.Context, id uuid.UUID, gen int, providerCheckoutID, checkoutURL, state string) error {
	args := m.Called(ctx, id, gen, providerCheckoutID, checkoutURL, state)
	return args.Error(0)
}

func (m *idempotencyRepoMock) MarkFailed(ctx context.Context, id uuid.UUID, gen int, state string) error {
	args := m.Called(ctx, id, gen, state)
	return args.Error(0)
}

func TestPriceMismatchError_ErrorMethod(t *testing.T) {
	err := &PriceMismatchError{
		Message: "price changed",
	}
	assert.Equal(t, "price changed", err.Error())
}

func TestNormalizeIdempotencyKey_Validation(t *testing.T) {
	// Normal key
	key, ok := NormalizeIdempotencyKey("valid-key-123")
	assert.True(t, ok)
	assert.Equal(t, "valid-key-123", key)

	// Empty key generates UUID
	kEmpty, okEmpty := NormalizeIdempotencyKey("   ")
	assert.False(t, okEmpty)
	assert.NotEmpty(t, kEmpty)

	// Key longer than 128 chars is truncated to 128 chars
	longKey := strings.Repeat("a", 200)
	kLong, okLong := NormalizeIdempotencyKey(longKey)
	assert.True(t, okLong)
	assert.Equal(t, 128, len(kLong))
}

func TestPaymentService_CreateOrderWithOffer_IdempotencyBranches(t *testing.T) {
	ctx := context.Background()
	uid := uuid.New()
	specID := uuid.New()
	licID := uuid.New()
	priceID := uuid.New()
	rev := int64(1)
	amt := 1000
	curr := "INR"
	idKey := "client-idemp-key-1"

	input := CreateOrderInput{
		UserID:              uid,
		SpecID:              specID,
		LicenseOptionID:     licID,
		Currency:            curr,
		ExpectedPriceID:     &priceID,
		ExpectedRevision:    &rev,
		ExpectedAmountMinor: &amt,
		ExpectedCurrency:    &curr,
		IdempotencyKey:      idKey,
	}

	t.Run("Conflict: different request hash returns ErrIdempotencyConflict", func(t *testing.T) {
		s, _, _, _, _, _, _, _ := newPaymentSvc()
		idemRepo := new(idempotencyRepoMock)
		s.SetIdempotencyRepository(idemRepo)

		idemRepo.On("GetByUserAndKey", ctx, uid, idKey).Return(&domain.OrderIdempotencyKey{
			UserID:         uid,
			IdempotencyKey: idKey,
			RequestHash:    "different-sha256-hash",
		}, nil).Once()

		_, err := s.CreateOrderWithOffer(ctx, input)
		require.ErrorIs(t, err, domain.ErrIdempotencyConflict)
	})

	t.Run("Replay: existing paid order returns order without checkout url", func(t *testing.T) {
		s, or, _, _, _, _, _, _ := newPaymentSvc()
		idemRepo := new(idempotencyRepoMock)
		s.SetIdempotencyRepository(idemRepo)

		reqHash := ComputeCanonicalFingerprint(input)
		existingOID := uuid.New()
		url := "https://checkout.example.com"

		idemRepo.On("GetByUserAndKey", ctx, uid, idKey).Return(&domain.OrderIdempotencyKey{
			UserID:         uid,
			IdempotencyKey: idKey,
			RequestHash:    reqHash,
			OrderID:        &existingOID,
			CheckoutURL:    &url,
			CheckoutState:  domain.CheckoutStateReady,
		}, nil).Once()

		or.On("GetByID", ctx, existingOID).Return(&domain.Order{
			ID:          existingOID,
			Status:      domain.OrderStatusPaid,
			CheckoutURL: &url,
		}, nil).Once()

		res, err := s.CreateOrderWithOffer(ctx, input)
		require.NoError(t, err)
		assert.Equal(t, existingOID, res.ID)
		assert.Nil(t, res.CheckoutURL)
	})

	t.Run("Replay: existing pending order returns order with checkout url", func(t *testing.T) {
		s, or, _, _, _, _, _, _ := newPaymentSvc()
		idemRepo := new(idempotencyRepoMock)
		s.SetIdempotencyRepository(idemRepo)

		reqHash := ComputeCanonicalFingerprint(input)
		existingOID := uuid.New()
		url := "https://checkout.example.com/pending"

		idemRepo.On("GetByUserAndKey", ctx, uid, idKey).Return(&domain.OrderIdempotencyKey{
			UserID:         uid,
			IdempotencyKey: idKey,
			RequestHash:    reqHash,
			OrderID:        &existingOID,
			CheckoutURL:    &url,
			CheckoutState:  domain.CheckoutStateReady,
		}, nil).Once()

		or.On("GetByID", ctx, existingOID).Return(&domain.Order{
			ID:          existingOID,
			Status:      domain.OrderStatusPending,
			CheckoutURL: &url,
		}, nil).Once()

		res, err := s.CreateOrderWithOffer(ctx, input)
		require.NoError(t, err)
		assert.Equal(t, existingOID, res.ID)
		assert.Equal(t, &url, res.CheckoutURL)
	})

	t.Run("Replay: in flight returns ErrOrderReservedInFlight", func(t *testing.T) {
		s, _, _, _, _, _, _, _ := newPaymentSvc()
		idemRepo := new(idempotencyRepoMock)
		s.SetIdempotencyRepository(idemRepo)

		reqHash := ComputeCanonicalFingerprint(input)

		idemRepo.On("GetByUserAndKey", ctx, uid, idKey).Return(&domain.OrderIdempotencyKey{
			UserID:         uid,
			IdempotencyKey: idKey,
			RequestHash:    reqHash,
			CheckoutState:  domain.CheckoutStateClaiming,
		}, nil).Once()

		_, err := s.CreateOrderWithOffer(ctx, input)
		require.ErrorIs(t, err, domain.ErrOrderReservedInFlight)
	})
}

func TestPaymentService_CreateOrderWithOffer_ValidationErrors(t *testing.T) {
	ctx := context.Background()
	uid := uuid.New()
	specID := uuid.New()
	licID := uuid.New()
	priceID := uuid.New()
	rev := int64(1)
	amt := 1000
	curr := "INR"

	input := CreateOrderInput{
		UserID:              uid,
		SpecID:              specID,
		LicenseOptionID:     licID,
		Currency:            curr,
		ExpectedPriceID:     &priceID,
		ExpectedRevision:    &rev,
		ExpectedAmountMinor: &amt,
		ExpectedCurrency:    &curr,
	}

	t.Run("spec not found", func(t *testing.T) {
		s, _, _, _, sf, _, _, _ := newPaymentSvc()
		sf.On("FindWithLicenses", ctx, specID).Return(nil, errors.New("not found")).Once()

		_, err := s.CreateOrderWithOffer(ctx, input)
		require.Error(t, err)
	})

	t.Run("spec processing not completed", func(t *testing.T) {
		s, _, _, _, sf, _, _, _ := newPaymentSvc()
		sf.On("FindWithLicenses", ctx, specID).Return(&catalogDomain.Spec{
			ID:               specID,
			ProcessingStatus: catalogDomain.ProcessingStatusPending,
		}, nil).Once()

		_, err := s.CreateOrderWithOffer(ctx, input)
		assert.EqualError(t, err, "Beat/Sample is not ready for purchase")
	})

	t.Run("license option not found", func(t *testing.T) {
		s, _, _, _, sf, _, _, _ := newPaymentSvc()
		sf.On("FindWithLicenses", ctx, specID).Return(&catalogDomain.Spec{
			ID:               specID,
			ProcessingStatus: catalogDomain.ProcessingStatusCompleted,
			Licenses:         []catalogDomain.LicenseOption{},
		}, nil).Once()

		_, err := s.CreateOrderWithOffer(ctx, input)
		assert.EqualError(t, err, "license option not found")
	})

	t.Run("invalid currency", func(t *testing.T) {
		s, _, _, _, sf, _, _, _ := newPaymentSvc()
		sf.On("FindWithLicenses", ctx, specID).Return(&catalogDomain.Spec{
			ID:               specID,
			ProcessingStatus: catalogDomain.ProcessingStatusCompleted,
			Licenses: []catalogDomain.LicenseOption{
				{ID: licID},
			},
		}, nil).Once()

		badCurrInput := input
		badCurrInput.Currency = "ZZZ"
		_, err := s.CreateOrderWithOffer(ctx, badCurrInput)
		require.ErrorIs(t, err, sharedmoney.ErrInvalidCurrency)
	})

	t.Run("missing price evidence fields", func(t *testing.T) {
		s, _, _, _, sf, _, _, _ := newPaymentSvc()
		sf.On("FindWithLicenses", ctx, specID).Return(&catalogDomain.Spec{
			ID:               specID,
			ProcessingStatus: catalogDomain.ProcessingStatusCompleted,
			Licenses: []catalogDomain.LicenseOption{
				{
					ID:            licID,
					PriceCurrency: "INR",
					Prices: []sharedmoney.StoredPrice{
						{ID: priceID, Currency: "INR", AmountMinor: 1000, Revision: 1, IsActive: true},
					},
				},
			},
		}, nil).Once()

		missingEvidence := input
		missingEvidence.ExpectedPriceID = nil
		_, err := s.CreateOrderWithOffer(ctx, missingEvidence)
		var pErr *PriceMismatchError
		require.True(t, errors.As(err, &pErr))
	})
}

func TestPaymentService_RefundAndWebhookBranches(t *testing.T) {
	ctx := context.Background()
	oid := uuid.New()
	adminID := uuid.New()

	t.Run("InitiateRefund repository missing returns error", func(t *testing.T) {
		s, or, pr, _, _, _, _, _ := newPaymentSvc()
		or.On("GetByID", ctx, oid).Return(&domain.Order{
			ID:     oid,
			Status: domain.OrderStatusPaid,
		}, nil).Once()
		pr.On("GetByOrderID", ctx, oid).Return(&domain.Payment{
			OrderID: oid,
			Status:  domain.PaymentStatusCaptured,
		}, nil).Once()
		_, err := s.InitiateRefund(ctx, CreateRefundInput{
			OrderID: oid,
			AdminID: adminID,
			Reason:  "test",
		})
		require.Error(t, err)
	})

	t.Run("GetOrderRefunds repository missing returns empty", func(t *testing.T) {
		s, _, _, _, _, _, _, _ := newPaymentSvc()
		res, err := s.GetOrderRefunds(ctx, oid)
		require.NoError(t, err)
		assert.Empty(t, res)
	})

	t.Run("ListWebhookEvents repository missing returns empty", func(t *testing.T) {
		s, _, _, _, _, _, _, _ := newPaymentSvc()
		evs, count, err := s.ListWebhookEvents(ctx, domain.WebhookEventFilter{}, 10, 0)
		require.NoError(t, err)
		assert.Empty(t, evs)
		assert.Zero(t, count)
	})

	t.Run("RequeueWebhookEvent repository missing returns nil", func(t *testing.T) {
		s, _, _, _, _, _, _, _ := newPaymentSvc()
		err := s.RequeueWebhookEvent(ctx, uuid.New())
		require.NoError(t, err)
	})

	t.Run("ProcessNextWebhookBatch repository missing returns zero", func(t *testing.T) {
		s, _, _, _, _, _, _, _ := newPaymentSvc()
		n, err := s.ProcessNextWebhookBatch(ctx, "w1", 10, time.Minute)
		require.NoError(t, err)
		assert.Equal(t, 0, n)
	})

	t.Run("HandleWebhook without repository processes directly", func(t *testing.T) {
		s, or, pr, _, _, _, _, _ := newPaymentSvc()
		fake := fakeOf(s, domain.ProviderDodo)
		fake.webhook = &domain.PaymentConfirmation{
			OrderID:           oid,
			ProviderPaymentID: "pay_direct_1",
		}
		or.On("GetByID", ctx, oid).Return(&domain.Order{
			ID:       oid,
			Status:   domain.OrderStatusPaid,
			Provider: domain.ProviderDodo,
		}, nil).Once()
		pr.On("GetByOrderID", ctx, oid).Return(&domain.Payment{
			OrderID:           oid,
			ProviderPaymentID: "pay_direct_1",
		}, nil).Once()

		err := s.HandleWebhook(ctx, domain.ProviderDodo, []byte(`{}`), map[string]string{"signature": "ok"})
		require.NoError(t, err)
	})
}

func TestStartWebhookWorker_Defaults(t *testing.T) {
	s, _, _, _, _, _, _, _ := newPaymentSvc()
	wRepo := new(webhookRepoMock)
	s.SetWebhookRepository(wRepo)
	wRepo.On("ClaimNextBatch", mock.Anything, "payment-webhook-worker", 10, 60*time.Second).Return([]domain.PaymentWebhookEvent{}, errors.New("err")).Maybe()

	cancelCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		StartWebhookWorker(cancelCtx, s, WebhookWorkerConfig{
			PollInterval: 10 * time.Millisecond,
		})
		close(done)
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not exit")
	}
}
