package application

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type refundRepoMock struct {
	mock.Mock
}

func (m *refundRepoMock) ReserveRefund(ctx context.Context, refund *domain.OrderRefund) (*domain.OrderRefund, bool, error) {
	args := m.Called(ctx, refund)
	if args.Get(0) == nil {
		return nil, args.Bool(1), args.Error(2)
	}
	return args.Get(0).(*domain.OrderRefund), args.Bool(1), args.Error(2)
}

func (m *refundRepoMock) GetByID(ctx context.Context, id uuid.UUID) (*domain.OrderRefund, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.OrderRefund), args.Error(1)
}

func (m *refundRepoMock) GetByOrderID(ctx context.Context, orderID uuid.UUID) ([]domain.OrderRefund, error) {
	args := m.Called(ctx, orderID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.OrderRefund), args.Error(1)
}

func (m *refundRepoMock) GetByIdempotencyKey(ctx context.Context, key string) (*domain.OrderRefund, error) {
	args := m.Called(ctx, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.OrderRefund), args.Error(1)
}

func (m *refundRepoMock) GetByProviderRefundID(ctx context.Context, provider, providerRefundID string) (*domain.OrderRefund, error) {
	args := m.Called(ctx, provider, providerRefundID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.OrderRefund), args.Error(1)
}

func (m *refundRepoMock) UpdateStatus(ctx context.Context, id uuid.UUID, status string, providerRefundID *string, failureReason *string) error {
	args := m.Called(ctx, id, status, providerRefundID, failureReason)
	return args.Error(0)
}

type webhookRepoMock struct {
	mock.Mock
}

func (m *webhookRepoMock) InsertEvent(ctx context.Context, event *domain.PaymentWebhookEvent) (*domain.PaymentWebhookEvent, bool, error) {
	args := m.Called(ctx, event)
	if args.Get(0) == nil {
		return nil, args.Bool(1), args.Error(2)
	}
	return args.Get(0).(*domain.PaymentWebhookEvent), args.Bool(1), args.Error(2)
}

func (m *webhookRepoMock) GetByID(ctx context.Context, id uuid.UUID) (*domain.PaymentWebhookEvent, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.PaymentWebhookEvent), args.Error(1)
}

func (m *webhookRepoMock) ClaimNextBatch(ctx context.Context, workerID string, batchSize int, leaseDuration time.Duration) ([]domain.PaymentWebhookEvent, error) {
	args := m.Called(ctx, workerID, batchSize, leaseDuration)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.PaymentWebhookEvent), args.Error(1)
}

func (m *webhookRepoMock) MarkCompleted(ctx context.Context, id uuid.UUID, lockedAt *time.Time) error {
	args := m.Called(ctx, id, lockedAt)
	return args.Error(0)
}

func (m *webhookRepoMock) MarkFailed(ctx context.Context, id uuid.UUID, lockedAt *time.Time, lastErr string, retryAfter time.Duration, status string) error {
	args := m.Called(ctx, id, lockedAt, lastErr, retryAfter, status)
	return args.Error(0)
}

func (m *webhookRepoMock) ListEvents(ctx context.Context, filter domain.WebhookEventFilter, limit, offset int) ([]domain.PaymentWebhookEvent, int, error) {
	args := m.Called(ctx, filter, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]domain.PaymentWebhookEvent), args.Int(1), args.Error(2)
}

func (m *webhookRepoMock) Requeue(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

type refunderFake struct {
	fakeGateway
	refundRes *domain.RefundResult
	refundErr error
}

func (g *refunderFake) Refund(_ context.Context, _ domain.RefundRequest) (*domain.RefundResult, error) {
	if g.refundErr != nil {
		return nil, g.refundErr
	}
	return g.refundRes, nil
}

func TestPaymentService_Refunds(t *testing.T) {
	ctx := context.Background()
	oid := uuid.New()
	adminID := uuid.New()

	t.Run("order not found returns ErrOrderNotFound", func(t *testing.T) {
		s, or, _, _, _, _, _, _ := newPaymentSvc()
		or.On("GetByID", ctx, oid).Return(nil, domain.ErrOrderNotFound).Once()

		_, err := s.InitiateRefund(ctx, CreateRefundInput{
			OrderID: oid,
			AdminID: adminID,
			Reason:  "duplicate",
		})
		assert.ErrorIs(t, err, domain.ErrOrderNotFound)
	})

	t.Run("order not paid returns ErrRefundNotEligible", func(t *testing.T) {
		s, or, _, _, _, _, _, _ := newPaymentSvc()
		or.On("GetByID", ctx, oid).Return(&domain.Order{
			ID:     oid,
			Status: domain.OrderStatusPending,
		}, nil).Once()

		_, err := s.InitiateRefund(ctx, CreateRefundInput{
			OrderID: oid,
			AdminID: adminID,
			Reason:  "customer_request",
		})
		assert.ErrorIs(t, err, domain.ErrRefundNotEligible)
	})

	t.Run("successful refund transitions order, updates payment and revokes license", func(t *testing.T) {
		s, or, pr, lr, _, _, _, _ := newPaymentSvc()
		rfRepo := new(refundRepoMock)
		s.SetRefundRepository(rfRepo)

		rzRef := &refunderFake{
			fakeGateway: fakeGateway{name: domain.ProviderRazorpay},
			refundRes: &domain.RefundResult{
				ProviderRefundID: "rfnd_rzp_123",
				Status:           domain.RefundStatusSucceeded,
			},
		}
		reg, err := NewGatewayRegistry(map[string]string{"INR": domain.ProviderRazorpay}, rzRef)
		require.NoError(t, err)
		s.gateways = reg

		order := &domain.Order{
			ID:       oid,
			Status:   domain.OrderStatusPaid,
			Provider: domain.ProviderRazorpay,
			Amount:   2000,
			Currency: "INR",
		}
		or.On("GetByID", ctx, oid).Return(order, nil).Once()

		payID := uuid.New()
		payment := &domain.Payment{
			ID:                payID,
			OrderID:           oid,
			Provider:          domain.ProviderRazorpay,
			ProviderPaymentID: "pay_rzp_999",
			Status:            domain.PaymentStatusCaptured,
		}
		pr.On("GetByOrderID", ctx, oid).Return(payment, nil).Once()

		rfID := uuid.New()
		savedRefund := &domain.OrderRefund{
			ID:             rfID,
			OrderID:        oid,
			Amount:         2000,
			Currency:       "INR",
			IdempotencyKey: "ref-key-1",
			Status:         domain.RefundStatusRequested,
		}
		rfRepo.On("GetByIdempotencyKey", ctx, "ref-key-1").Return(nil, nil).Once()
		rfRepo.On("GetByOrderID", ctx, oid).Return([]domain.OrderRefund{}, nil).Once()
		rfRepo.On("ReserveRefund", ctx, mock.AnythingOfType("*domain.OrderRefund")).Return(savedRefund, true, nil).Once()
		rfRepo.On("UpdateStatus", ctx, rfID, domain.RefundStatusSucceeded, mock.Anything, (*string)(nil)).Return(nil).Once()

		or.On("MarkRefunded", ctx, oid).Return(nil).Once()
		pr.On("UpdateStatus", ctx, payID, domain.PaymentStatusRefunded).Return(nil).Once()

		licID := uuid.New()
		license := &domain.License{
			ID:       licID,
			OrderID:  oid,
			IsActive: true,
		}
		lr.On("GetByOrderID", ctx, oid).Return(license, nil).Once()
		lr.On("Revoke", ctx, licID, "Full refund issued by admin: customer_request").Return(nil).Once()

		res, err := s.InitiateRefund(ctx, CreateRefundInput{
			OrderID:        oid,
			AdminID:        adminID,
			IdempotencyKey: "ref-key-1",
			Reason:         "customer_request",
		})
		require.NoError(t, err)
		assert.Equal(t, rfID, res.ID)
		assert.Equal(t, domain.RefundStatusSucceeded, res.Status)
		assert.Equal(t, "rfnd_rzp_123", *res.ProviderRefundID)
	})

	t.Run("idempotent replay returns existing refund directly", func(t *testing.T) {
		s, _, _, _, _, _, _, _ := newPaymentSvc()
		rfRepo := new(refundRepoMock)
		s.SetRefundRepository(rfRepo)

		rfID := uuid.New()
		existing := &domain.OrderRefund{
			ID:             rfID,
			OrderID:        oid,
			Amount:         2000,
			Currency:       "INR",
			IdempotencyKey: "ref-key-existing",
			Status:         domain.RefundStatusSucceeded,
		}
		rfRepo.On("GetByIdempotencyKey", ctx, "ref-key-existing").Return(existing, nil).Once()

		res, err := s.InitiateRefund(ctx, CreateRefundInput{
			OrderID:        oid,
			AdminID:        adminID,
			IdempotencyKey: "ref-key-existing",
			Reason:         "customer_request",
		})
		require.NoError(t, err)
		assert.Equal(t, rfID, res.ID)
		assert.Equal(t, domain.RefundStatusSucceeded, res.Status)
	})

	t.Run("GetOrderRefunds delegates to repository", func(t *testing.T) {
		s, _, _, _, _, _, _, _ := newPaymentSvc()
		rfRepo := new(refundRepoMock)
		s.SetRefundRepository(rfRepo)

		expected := []domain.OrderRefund{
			{ID: uuid.New(), OrderID: oid, Status: domain.RefundStatusSucceeded},
		}
		rfRepo.On("GetByOrderID", ctx, oid).Return(expected, nil).Once()

		list, err := s.GetOrderRefunds(ctx, oid)
		require.NoError(t, err)
		assert.Len(t, list, 1)
		assert.Equal(t, expected[0].ID, list[0].ID)
	})
}

func TestPaymentService_WebhookAdminAndWorker(t *testing.T) {
	ctx := context.Background()
	eID := uuid.New()

	t.Run("ListWebhookEvents delegates to repository", func(t *testing.T) {
		s, _, _, _, _, _, _, _ := newPaymentSvc()
		wRepo := new(webhookRepoMock)
		s.SetWebhookRepository(wRepo)

		filter := domain.WebhookEventFilter{Provider: "dodo"}
		expected := []domain.PaymentWebhookEvent{{ID: eID, Provider: "dodo"}}
		wRepo.On("ListEvents", ctx, filter, 10, 0).Return(expected, 1, nil).Once()

		list, total, err := s.ListWebhookEvents(ctx, filter, 10, 0)
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		assert.Len(t, list, 1)
	})

	t.Run("RequeueWebhookEvent delegates to repository", func(t *testing.T) {
		s, _, _, _, _, _, _, _ := newPaymentSvc()
		wRepo := new(webhookRepoMock)
		s.SetWebhookRepository(wRepo)

		wRepo.On("Requeue", ctx, eID).Return(nil).Once()
		err := s.RequeueWebhookEvent(ctx, eID)
		require.NoError(t, err)
	})

	t.Run("ProcessNextWebhookBatch processes claimed events", func(t *testing.T) {
		s, or, pr, _, _, _, _, _ := newPaymentSvc()
		wRepo := new(webhookRepoMock)
		s.SetWebhookRepository(wRepo)

		oid := uuid.New()
		lockedAt := time.Now()
		paymentID := "p1"
		event := domain.PaymentWebhookEvent{
			OrderID:           &oid,
			ProviderPaymentID: &paymentID,
			LockedAt:          &lockedAt,
			ID:                eID,
			Provider:          domain.ProviderDodo,
			Payload:           []byte(`{"data":{"order_id":"` + oid.String() + `","payment_id":"p1"}}`),
			Headers:           map[string]string{"webhook-signature": "v1,abc"},
			Status:            domain.WebhookEventStatusQueued,
		}

		wRepo.On("ClaimNextBatch", ctx, "w1", 10, 30*time.Second).Return([]domain.PaymentWebhookEvent{event}, nil).Once()

		or.On("GetByID", ctx, oid).Return(&domain.Order{
			ID:       oid,
			Status:   domain.OrderStatusPaid,
			Provider: domain.ProviderDodo,
		}, nil).Once()
		pr.On("GetByOrderID", ctx, oid).Return(&domain.Payment{
			OrderID:           oid,
			ProviderPaymentID: "p1",
		}, nil).Once()
		wRepo.On("MarkCompleted", ctx, eID, &lockedAt).Return(nil).Once()

		processed, err := s.ProcessNextWebhookBatch(ctx, "w1", 10, 30*time.Second)
		require.NoError(t, err)
		assert.Equal(t, 1, processed)
	})

	t.Run("StartWebhookWorker terminates cleanly on cancelled context", func(t *testing.T) {
		s, _, _, _, _, _, _, _ := newPaymentSvc()
		wRepo := new(webhookRepoMock)
		s.SetWebhookRepository(wRepo)
		wRepo.On("ClaimNextBatch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]domain.PaymentWebhookEvent{}, nil).Maybe()

		cancelCtx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			StartWebhookWorker(cancelCtx, s, WebhookWorkerConfig{
				PollInterval: 20 * time.Millisecond,
				BatchSize:    5,
				WorkerID:     "test-worker",
			})
			close(done)
		}()

		time.Sleep(50 * time.Millisecond)
		cancel()

		select {
		case <-done:
			// exited cleanly
		case <-time.After(2 * time.Second):
			t.Fatal("worker did not exit on cancelled context")
		}
	})
}
