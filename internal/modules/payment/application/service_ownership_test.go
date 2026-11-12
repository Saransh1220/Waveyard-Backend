package application

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	catalogDomain "github.com/saransh1220/blueprint-audio/internal/modules/catalog/domain"
)

func TestPaymentService_GetOrder_Ownership(t *testing.T) {
	ctx := context.Background()

	t.Run("buyer can read own order", func(t *testing.T) {
		s, or, _, _, _, _, _, _ := newPaymentSvc()
		order := pendingOrder(nil)
		or.On("GetByID", ctx, order.ID).Return(order, nil).Once()
		got, err := s.GetOrder(ctx, order.ID, order.UserID)
		require.NoError(t, err)
		assert.Same(t, order, got)
	})

	t.Run("producer of the ordered spec can read it", func(t *testing.T) {
		s, or, _, _, sf, _, _, _ := newPaymentSvc()
		order := pendingOrder(nil)
		checkoutURL := "https://checkout.example.test/buyer-session"
		order.CheckoutURL = &checkoutURL
		producerID := uuid.New()
		or.On("GetByID", ctx, order.ID).Return(order, nil).Once()
		sf.On("FindByIDIncludingDeleted", ctx, order.SpecID).Return(&catalogDomain.Spec{ID: order.SpecID, ProducerID: producerID}, nil).Once()
		got, err := s.GetOrder(ctx, order.ID, producerID)
		require.NoError(t, err)
		assert.Same(t, order, got)
		assert.Nil(t, got.CheckoutURL)
	})

	t.Run("stranger gets not found", func(t *testing.T) {
		s, or, _, _, sf, _, _, _ := newPaymentSvc()
		order := pendingOrder(nil)
		or.On("GetByID", ctx, order.ID).Return(order, nil).Once()
		sf.On("FindByIDIncludingDeleted", ctx, order.SpecID).Return(&catalogDomain.Spec{ID: order.SpecID, ProducerID: uuid.New()}, nil).Once()
		got, err := s.GetOrder(ctx, order.ID, uuid.New())
		assert.Nil(t, got)
		assert.EqualError(t, err, "order not found")
	})

	t.Run("spec lookup failure denies access", func(t *testing.T) {
		s, or, _, _, sf, _, _, _ := newPaymentSvc()
		order := pendingOrder(nil)
		or.On("GetByID", ctx, order.ID).Return(order, nil).Once()
		sf.On("FindByIDIncludingDeleted", ctx, order.SpecID).Return(nil, errors.New("db down")).Once()
		_, err := s.GetOrder(ctx, order.ID, uuid.New())
		assert.EqualError(t, err, "order not found")
	})
}

func TestPaymentService_VerifyPayment_RejectsNonBuyer(t *testing.T) {
	ctx := context.Background()
	s, or, _, _, _, _, _, _ := newPaymentSvc()
	rzp := "order_1"
	order := pendingOrder(&rzp)
	or.On("GetByID", ctx, order.ID).Return(order, nil).Once()

	// No UpdateStatus expectation: a stranger must not be able to fail the order.
	_, err := s.VerifyPayment(ctx, order.ID, uuid.New(), "pay_1", "bad-signature")
	assert.EqualError(t, err, "order not found")
	or.AssertNotCalled(t, "UpdateStatus", ctx, order.ID, "failed")
}
