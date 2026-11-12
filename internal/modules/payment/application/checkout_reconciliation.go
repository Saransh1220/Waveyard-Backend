package application

import (
	"context"
	"database/sql"
	"errors"
	"log"

	"github.com/google/uuid"
	"time"

	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
)

func (s *paymentService) recoverCheckout(ctx context.Context, order *domain.Order, key *domain.OrderIdempotencyKey) (*domain.Order, error) {
	switch key.CheckoutState {
	case domain.CheckoutStateReserved:
		if time.Now().Before(order.ExpiresAt) {
			gateway, err := s.gateways.ByName(order.Provider)
			if err != nil {
				return nil, err
			}
			return s.dispatchCheckout(ctx, gateway, order, key)
		}
		return nil, domain.ErrOrderReservedInFlight
	case domain.CheckoutStateClaiming:
		if key.LeaseExpiresAt == nil || time.Now().Before(*key.LeaseExpiresAt) {
			return nil, domain.ErrOrderReservedInFlight
		}
	case domain.CheckoutStateReconciling:
	default:
		return nil, domain.ErrOrderReservedInFlight
	}

	gateway, err := s.gateways.ByName(order.Provider)
	if err != nil {
		return nil, err
	}
	return s.lookupCheckout(ctx, gateway, order, key, "")
}

func (s *paymentService) lookupCheckout(ctx context.Context, gateway domain.Gateway, order *domain.Order, key *domain.OrderIdempotencyKey, reference string) (*domain.Order, error) {
	var session *domain.CheckoutSession
	var capture *domain.PaymentConfirmation
	var err error
	if reference == "" {
		finder, ok := gateway.(domain.CheckoutFinder)
		if !ok {
			return nil, domain.ErrOrderReservedInFlight
		}
		session, capture, err = finder.FindCheckout(ctx, order)
	} else {
		finder, ok := gateway.(domain.CheckoutReferenceFinder)
		if !ok {
			return nil, domain.ErrProviderMismatch
		}
		session, capture, err = finder.FindCheckoutByReference(ctx, order, reference)
	}
	if err != nil {
		log.Printf("[PAYMENT_CHECKOUT_RECONCILE] order_id=%s provider=%s error=%v", order.ID, order.Provider, err)
		if reference != "" && (errors.Is(err, domain.ErrProviderMismatch) || errors.Is(err, domain.ErrAmountMismatch)) {
			return nil, err
		}
		return nil, domain.ErrOrderReservedInFlight
	}
	if capture != nil {
		if err := s.processCapture(ctx, order.Provider, capture); err != nil {
			return nil, err
		}
		return s.replayCheckout(ctx, key, key.RequestHash)
	}
	if session != nil && time.Now().Before(order.ExpiresAt) {
		return s.saveCheckout(ctx, order, key, key.AttemptGeneration, *session)
	}
	return nil, domain.ErrOrderReservedInFlight
}

// ReconcileOrderCheckout lets an authorized operator trigger provider reads for
// a previously dispatched intent. It never issues a provider create request.
func (s *paymentService) ReconcileOrderCheckout(ctx context.Context, orderID uuid.UUID, reference string) error {
	if s.idempotencyRepo == nil {
		return domain.ErrOrderNotFound
	}
	key, err := s.idempotencyRepo.GetByOrderID(ctx, orderID)
	if err != nil {
		return err
	}
	if key == nil {
		return domain.ErrOrderNotFound
	}
	order, err := s.orderRepo.GetByID(ctx, orderID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrOrderNotFound
	}
	if err != nil {
		return err
	}
	if key.CheckoutState == domain.CheckoutStateReserved {
		return domain.ErrOrderReservedInFlight
	}
	if order.Status != domain.OrderStatusPending && order.Status != domain.OrderStatusProcessing {
		return nil
	}
	if key.CheckoutState == domain.CheckoutStateReady {
		return nil
	}
	if reference == "" {
		_, err = s.recoverCheckout(ctx, order, key)
		return err
	}
	gateway, err := s.gateways.ByName(order.Provider)
	if err != nil {
		return err
	}
	_, err = s.lookupCheckout(ctx, gateway, order, key, reference)
	return err
}
