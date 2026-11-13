package application

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
)

func (s *paymentService) enqueueRefund(ctx context.Context, provider string, confirmation *domain.PaymentConfirmation, payload []byte) error {
	refund := confirmation.Refund
	if s.webhookRepo == nil {
		return s.reconcileRefund(ctx, provider, confirmation.ProviderPaymentID, *refund)
	}
	eventID := confirmation.EventID
	if eventID == "" {
		eventID = fmt.Sprintf("refund:%s:%s", refund.ProviderRefundID, refund.Status)
	}
	_, _, err := s.webhookRepo.InsertEvent(ctx, &domain.PaymentWebhookEvent{
		Provider: provider, EventID: eventID, EventType: "payment.refund",
		ProviderPaymentID: &confirmation.ProviderPaymentID,
		ReportedAmount:    &refund.Amount, ReportedCurrency: &refund.Currency, Payload: payload,
		Headers: map[string]string{
			"refund_id": refund.ProviderRefundID, "full": strconv.FormatBool(refund.Full),
			"status": refund.Status, "intent_key": refund.IntentKey,
		},
	})
	return err
}

func (s *paymentService) reconcileRefund(ctx context.Context, provider, providerPaymentID string, refund domain.RefundConfirmation) error {
	if s.refundRepo == nil {
		return fmt.Errorf("refund repository unavailable")
	}
	return s.getTxRunner().WithTx(ctx, func(tx domain.PaymentTx) error {
		payment, err := tx.PaymentRepo().GetByProviderPaymentID(ctx, provider, providerPaymentID)
		if err != nil {
			return err
		}
		repo := tx.RefundRepo()
		if err := repo.LockPayment(ctx, payment.ID); err != nil {
			return err
		}
		payment, err = tx.PaymentRepo().GetByID(ctx, payment.ID)
		if err != nil {
			return err
		}
		if payment.Status != domain.PaymentStatusCaptured && payment.Status != domain.PaymentStatusRefunded {
			return domain.ErrOrderNotPayable
		}
		if refund.Full && provider == domain.ProviderDodo {
			// Dodo's signed full-refund marker applies to the complete
			// captured payment. Its amount may be in settlement currency.
			if refund.Amount == 0 || !strings.EqualFold(refund.Currency, payment.Currency) {
				refund.Amount = payment.Amount
				refund.Currency = payment.Currency
			}
		}
		if refund.Amount <= 0 || refund.Amount > payment.Amount ||
			(refund.Full && refund.Amount != payment.Amount) ||
			(refund.Currency != "" && !strings.EqualFold(refund.Currency, payment.Currency)) {
			return domain.ErrAmountMismatch
		}
		total, err := repo.RecordProviderRefund(ctx, payment.ID, provider, refund)
		if err != nil {
			return err
		}
		if total > payment.Amount {
			return domain.ErrAmountMismatch
		}

		intent, err := repo.GetByProviderRefundID(ctx, provider, refund.ProviderRefundID)
		if err != nil {
			return err
		}
		if intent == nil && refund.IntentKey != "" {
			intent, err = repo.GetByIdempotencyKey(ctx, refund.IntentKey)
			if err != nil {
				return err
			}
		}
		if intent != nil && intent.PaymentID == payment.ID && intent.Provider == provider &&
			intent.Amount == refund.Amount &&
			(intent.ProviderRefundID == nil || *intent.ProviderRefundID == refund.ProviderRefundID) {
			if err := repo.UpdateStatus(ctx, intent.ID, refund.Status, &refund.ProviderRefundID, nil); err != nil {
				return err
			}
		}
		if total < payment.Amount || payment.Status == domain.PaymentStatusRefunded {
			return nil
		}

		license, err := tx.LicenseRepo().GetByOrderID(ctx, payment.OrderID)
		if err != nil {
			return err
		}
		if err := tx.OrderRepo().MarkRefunded(ctx, payment.OrderID); err != nil {
			return err
		}
		if err := tx.PaymentRepo().UpdateStatus(ctx, payment.ID, domain.PaymentStatusRefunded); err != nil {
			return err
		}
		return tx.LicenseRepo().Revoke(ctx, license.ID, "Full refund confirmed by payment provider")
	})
}
