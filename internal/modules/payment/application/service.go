package application

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	authDomain "github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	catalogDomain "github.com/saransh1220/blueprint-audio/internal/modules/catalog/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
	sharedemail "github.com/saransh1220/blueprint-audio/internal/shared/infrastructure/email"
	sharedmoney "github.com/saransh1220/blueprint-audio/internal/shared/money"
)

type FileService interface {
	GetKeyFromUrl(url string) (string, error)
	GetPresignedURL(ctx context.Context, key string, expiresIn time.Duration) (string, error)
}

type PaymentService interface {
	CreateOrder(ctx context.Context, userID, specID, licenseOptionID uuid.UUID, currency string) (*domain.Order, error)
	CreateOrderWithOffer(ctx context.Context, input CreateOrderInput) (*domain.Order, error)
	GetOrder(ctx context.Context, orderID, requesterID uuid.UUID) (*domain.Order, error)
	GetOrderStatus(ctx context.Context, orderID, requesterID uuid.UUID) (*OrderStatusResponse, error)
	VerifyPayment(ctx context.Context, orderID, userID uuid.UUID, providerPaymentID, signature string) (*domain.License, error)
	HandleWebhook(ctx context.Context, provider string, payload []byte, headers map[string]string) error
	GetUserOrders(ctx context.Context, userID uuid.UUID, page int) ([]domain.Order, error)
	GetUserLicenses(ctx context.Context, userID uuid.UUID, page int, search, licenseType string) ([]domain.License, int, error)
	GetLicenseDownloads(ctx context.Context, licenseID, userID uuid.UUID) (*LicenseDownloadsResponse, error)
	GetProducerOrders(ctx context.Context, producerID uuid.UUID, page, limit int) (*ProducerOrderResponse, error)

	// RW-1-9 Idempotency, Retries and Refunds
	InitiateRefund(ctx context.Context, input CreateRefundInput) (*OrderRefundResponse, error)
	GetOrderRefunds(ctx context.Context, orderID uuid.UUID) ([]domain.OrderRefund, error)
	ListWebhookEvents(ctx context.Context, filter domain.WebhookEventFilter, limit, offset int) ([]domain.PaymentWebhookEvent, int, error)
	RequeueWebhookEvent(ctx context.Context, eventID uuid.UUID) error
	ProcessNextWebhookBatch(ctx context.Context, workerID string, batchSize int, leaseDuration time.Duration) (int, error)
}

type nopTxRunner struct {
	orderRepo       domain.OrderRepository
	paymentRepo     domain.PaymentRepository
	licenseRepo     domain.LicenseRepository
	idempotencyRepo domain.IdempotencyRepository
	refundRepo      domain.RefundRepository
}

type nopPaymentTx struct {
	orderRepo       domain.OrderRepository
	paymentRepo     domain.PaymentRepository
	licenseRepo     domain.LicenseRepository
	idempotencyRepo domain.IdempotencyRepository
	refundRepo      domain.RefundRepository
}

func (pt *nopPaymentTx) OrderRepo() domain.OrderRepository             { return pt.orderRepo }
func (pt *nopPaymentTx) PaymentRepo() domain.PaymentRepository         { return pt.paymentRepo }
func (pt *nopPaymentTx) LicenseRepo() domain.LicenseRepository         { return pt.licenseRepo }
func (pt *nopPaymentTx) IdempotencyRepo() domain.IdempotencyRepository { return pt.idempotencyRepo }
func (pt *nopPaymentTx) RefundRepo() domain.RefundRepository           { return pt.refundRepo }

func (n *nopTxRunner) WithTx(ctx context.Context, fn func(tx domain.PaymentTx) error) error {
	return fn(&nopPaymentTx{
		orderRepo:       n.orderRepo,
		paymentRepo:     n.paymentRepo,
		licenseRepo:     n.licenseRepo,
		idempotencyRepo: n.idempotencyRepo,
		refundRepo:      n.refundRepo,
	})
}

type paymentService struct {
	orderRepo       domain.OrderRepository
	paymentRepo     domain.PaymentRepository
	licenseRepo     domain.LicenseRepository
	idempotencyRepo domain.IdempotencyRepository
	webhookRepo     domain.WebhookEventRepository
	refundRepo      domain.RefundRepository
	specFinder      catalogDomain.SpecFinder
	userFinder      authDomain.UserFinder
	fileService     FileService
	emailSender     sharedemail.Sender
	appBaseURL      string
	gateways        GatewayResolver
	snapshotter     domain.ReportingRateSnapshotter
	txRunner        domain.TxRunner
}

func NewPaymentService(
	orderRepo domain.OrderRepository,
	paymentRepo domain.PaymentRepository,
	licenseRepo domain.LicenseRepository,
	specFinder catalogDomain.SpecFinder,
	userFinder authDomain.UserFinder,
	fileService FileService,
	emailSender sharedemail.Sender,
	appBaseURL string,
	gateways GatewayResolver,
	txRunner ...domain.TxRunner,
) PaymentService {
	var runner domain.TxRunner
	if len(txRunner) > 0 {
		runner = txRunner[0]
	}
	return &paymentService{
		orderRepo:   orderRepo,
		paymentRepo: paymentRepo,
		licenseRepo: licenseRepo,
		specFinder:  specFinder,
		userFinder:  userFinder,
		fileService: fileService,
		emailSender: emailSender,
		appBaseURL:  appBaseURL,
		gateways:    gateways,
		txRunner:    runner,
	}
}

func (s *paymentService) SetIdempotencyRepository(r domain.IdempotencyRepository) {
	s.idempotencyRepo = r
}

func (s *paymentService) SetWebhookRepository(r domain.WebhookEventRepository) {
	s.webhookRepo = r
}

func (s *paymentService) SetRefundRepository(r domain.RefundRepository) {
	s.refundRepo = r
}

func (s *paymentService) getTxRunner() domain.TxRunner {
	if s.txRunner != nil {
		return s.txRunner
	}
	return &nopTxRunner{
		orderRepo:       s.orderRepo,
		paymentRepo:     s.paymentRepo,
		licenseRepo:     s.licenseRepo,
		idempotencyRepo: s.idempotencyRepo,
		refundRepo:      s.refundRepo,
	}
}

func (s *paymentService) SetReportingRateSnapshotter(snapshotter domain.ReportingRateSnapshotter) {
	s.snapshotter = snapshotter
}

func (s *paymentService) CreateOrder(ctx context.Context, userID, specID, licenseOptionID uuid.UUID, currency string) (*domain.Order, error) {
	spec, err := s.specFinder.FindWithLicenses(ctx, specID)
	if err != nil {
		return nil, errors.New("Beat/Sample not found")
	}
	if spec.ProcessingStatus != "" && spec.ProcessingStatus != catalogDomain.ProcessingStatusCompleted {
		return nil, errors.New("Beat/Sample is not ready for purchase")
	}
	var lo *catalogDomain.LicenseOption
	for _, l := range spec.Licenses {
		if l.ID == licenseOptionID {
			lo = &l
			break
		}
	}
	if lo == nil {
		return nil, errors.New("license option not found")
	}
	offer := sharedmoney.SelectPriceOffer(lo.Prices, lo.PriceCurrency, currency)
	if !offer.IsPurchasable || offer.PriceID == nil {
		return nil, sharedmoney.ErrPriceUnavailable
	}
	input := CreateOrderInput{
		UserID:              userID,
		SpecID:              specID,
		LicenseOptionID:     licenseOptionID,
		Currency:            currency,
		ExpectedPriceID:     offer.PriceID,
		ExpectedRevision:    &offer.Revision,
		ExpectedAmountMinor: &offer.AmountMinor,
		ExpectedCurrency:    &offer.Currency,
	}
	key, _ := NormalizeIdempotencyKey("")
	input.IdempotencyKey = key
	fingerprint := ComputeCanonicalFingerprint(input)
	return s.createOrderFromSpec(ctx, spec, lo, offer, input, fingerprint)
}

func (s *paymentService) CreateOrderWithOffer(ctx context.Context, input CreateOrderInput) (*domain.Order, error) {
	key, clientProvided := NormalizeIdempotencyKey(input.IdempotencyKey)
	input.IdempotencyKey = key
	fingerprint := ComputeCanonicalFingerprint(input)

	if s.idempotencyRepo != nil && clientProvided {
		existingKey, err := s.idempotencyRepo.GetByUserAndKey(ctx, input.UserID, key)
		if err == nil && existingKey != nil {
			if existingKey.RequestHash != fingerprint {
				return nil, domain.ErrIdempotencyConflict
			}
			if existingKey.OrderID != nil {
				existingOrder, oErr := s.orderRepo.GetByID(ctx, *existingKey.OrderID)
				if oErr == nil && existingOrder != nil {
					switch existingOrder.Status {
					case domain.OrderStatusPaid, domain.OrderStatusCancelled, domain.OrderStatusRefunded, domain.OrderStatusFailed:
						existingOrder.FillClientFields()
						existingOrder.CheckoutURL = nil
						return existingOrder, nil
					case domain.OrderStatusPending, domain.OrderStatusProcessing:
						if existingKey.CheckoutURL != nil && *existingKey.CheckoutURL != "" {
							existingOrder.CheckoutURL = existingKey.CheckoutURL
						}
						existingOrder.FillClientFields()
						return existingOrder, nil
					}
				}
			}
			if existingKey.CheckoutState == domain.CheckoutStateClaiming || existingKey.CheckoutState == domain.CheckoutStateReserved {
				return nil, domain.ErrOrderReservedInFlight
			}
		}
	}

	spec, err := s.specFinder.FindWithLicenses(ctx, input.SpecID)
	if err != nil {
		return nil, errors.New("Beat/Sample not found")
	}
	if spec.ProcessingStatus != "" && spec.ProcessingStatus != catalogDomain.ProcessingStatusCompleted {
		return nil, errors.New("Beat/Sample is not ready for purchase")
	}

	var licenseOption *catalogDomain.LicenseOption
	for _, lo := range spec.Licenses {
		if lo.ID == input.LicenseOptionID {
			licenseOption = &lo
			break
		}
	}

	if licenseOption == nil {
		return nil, errors.New("license option not found")
	}

	requestedCurrency := strings.ToUpper(strings.TrimSpace(input.Currency))
	if requestedCurrency == "" {
		requestedCurrency = sharedmoney.CurrencyINR
	}

	if _, ok := sharedmoney.LookupCurrency(requestedCurrency); !ok {
		return nil, sharedmoney.ErrInvalidCurrency
	}

	offer := sharedmoney.SelectPriceOffer(licenseOption.Prices, licenseOption.PriceCurrency, requestedCurrency)
	if !offer.IsPurchasable || offer.Selection == sharedmoney.SelectionUnavailable {
		return nil, sharedmoney.ErrPriceUnavailable
	}

	if input.ExpectedPriceID == nil || input.ExpectedRevision == nil || input.ExpectedAmountMinor == nil || input.ExpectedCurrency == nil {
		return nil, &PriceMismatchError{
			Message:      "missing required price evidence: expected_price_id, expected_revision, expected_amount_minor, and expected_currency are required",
			CurrentOffer: offer,
		}
	}

	expectedCurr := strings.ToUpper(strings.TrimSpace(*input.ExpectedCurrency))
	if expectedCurr != offer.Currency {
		return nil, &PriceMismatchError{
			Message:      fmt.Sprintf("currency mismatch or unconfirmed fallback: expected %s, current offer is %s", expectedCurr, offer.Currency),
			CurrentOffer: offer,
		}
	}

	if offer.PriceID == nil || *input.ExpectedPriceID != *offer.PriceID {
		return nil, &PriceMismatchError{
			Message:      fmt.Sprintf("price changed: expected %s, current %v", input.ExpectedPriceID, offer.PriceID),
			CurrentOffer: offer,
		}
	}
	if *input.ExpectedRevision != offer.Revision {
		return nil, &PriceMismatchError{
			Message:      fmt.Sprintf("price revision changed: expected %d, current %d", *input.ExpectedRevision, offer.Revision),
			CurrentOffer: offer,
		}
	}
	if *input.ExpectedAmountMinor != offer.AmountMinor {
		return nil, &PriceMismatchError{
			Message:      fmt.Sprintf("price amount changed: expected %d, current %d", *input.ExpectedAmountMinor, offer.AmountMinor),
			CurrentOffer: offer,
		}
	}

	return s.createOrderFromSpec(ctx, spec, licenseOption, offer, input, fingerprint)
}

func (s *paymentService) createOrderFromSpec(
	ctx context.Context,
	spec *catalogDomain.Spec,
	licenseOption *catalogDomain.LicenseOption,
	offer sharedmoney.SelectedPriceOffer,
	input CreateOrderInput,
	fingerprint string,
) (*domain.Order, error) {
	receiptID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("failed to generate uuid: %w", err)
	}

	priceIDStr := ""
	if offer.PriceID != nil {
		priceIDStr = offer.PriceID.String()
	}

	order := &domain.Order{
		ID:          receiptID,
		UserID:      input.UserID,
		SpecID:      input.SpecID,
		LicenseType: string(licenseOption.LicenseType),
		Amount:      offer.AmountMinor,
		Currency:    offer.Currency,
		Status:      domain.OrderStatusPending,
		Notes: map[string]any{
			"license_option_id": input.LicenseOptionID.String(),
			"spec_title":        spec.Title,
			"license_name":      licenseOption.Name,
			"display_currency":  offer.Currency,
			"pricing_snapshot": map[string]any{
				"price_id":     priceIDStr,
				"revision":     offer.Revision,
				"currency":     offer.Currency,
				"amount_minor": offer.AmountMinor,
				"amount_major": offer.AmountMajor,
				"selection":    string(offer.Selection),
				"validated_at": time.Now().UTC().Format(time.RFC3339),
			},
		},
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}

	if s.snapshotter != nil {
		snapshot, snapErr := s.snapshotter.Snapshot(ctx, order.Currency)
		if snapErr != nil {
			log.Printf("[PaymentService] Warning: FX snapshot failed for currency %s: %v", order.Currency, snapErr)
		} else if snapshot != nil {
			order.ReportCurrency = &snapshot.ReportCurrency
			order.ReportFXRate = snapshot.ReportFXRate
			order.ReportFXRateID = snapshot.ReportFXRateID
			order.ReportFXAsOf = snapshot.ReportFXAsOf
			order.ReportFXFetchedAt = snapshot.ReportFXFetchedAt
		}
	}

	gateway, err := s.gateways.ForCurrency(order.Currency)
	if err != nil {
		return nil, err
	}
	order.Provider = gateway.Name()

	var idempotencyRecord *domain.OrderIdempotencyKey
	txErr := s.getTxRunner().WithTx(ctx, func(tx domain.PaymentTx) error {
		if err := tx.OrderRepo().Create(ctx, order); err != nil {
			return err
		}
		if tx.IdempotencyRepo() != nil {
			keyRecord := &domain.OrderIdempotencyKey{
				UserID:            input.UserID,
				IdempotencyKey:    input.IdempotencyKey,
				RequestHash:       fingerprint,
				OrderID:           &order.ID,
				CheckoutState:     domain.CheckoutStateReserved,
				Provider:          order.Provider,
				AttemptGeneration: 1,
			}
			res, created, rErr := tx.IdempotencyRepo().ReserveKey(ctx, keyRecord)
			if rErr != nil {
				return rErr
			}
			if !created && res.RequestHash != fingerprint {
				return domain.ErrIdempotencyConflict
			}
			idempotencyRecord = res
		}
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}

	var currentGen = 1
	if s.idempotencyRepo != nil && idempotencyRecord != nil {
		claimed, newGen, cErr := s.idempotencyRepo.Claim(ctx, idempotencyRecord.ID, idempotencyRecord.AttemptGeneration, 45*time.Second)
		if cErr == nil && claimed {
			currentGen = newGen
		}
	}

	session, err := gateway.CreateCheckout(ctx, domain.CheckoutRequest{
		Order:       order,
		SpecTitle:   spec.Title,
		LicenseName: licenseOption.Name,
	})
	if err != nil {
		if s.idempotencyRepo != nil && idempotencyRecord != nil {
			_ = s.idempotencyRepo.MarkFailed(ctx, idempotencyRecord.ID, currentGen, domain.CheckoutStateReconciling)
		}
		return nil, err
	}

	order.ProviderCheckoutID = &session.ProviderRef
	if session.CheckoutURL != "" {
		order.CheckoutURL = &session.CheckoutURL
	}

	_ = s.orderRepo.AttachCheckoutDetails(ctx, order.ID, session.ProviderRef, session.CheckoutURL)
	if s.idempotencyRepo != nil && idempotencyRecord != nil {
		_ = s.idempotencyRepo.AttachCheckoutResult(ctx, idempotencyRecord.ID, currentGen, session.ProviderRef, session.CheckoutURL, domain.CheckoutStateReady)
	}

	order.FillClientFields()
	return order, nil
}

// GetOrderStatus returns a provider-neutral minimal status for polling.
func (s *paymentService) GetOrderStatus(ctx context.Context, orderID, requesterID uuid.UUID) (*OrderStatusResponse, error) {
	order, err := s.orderRepo.GetByID(ctx, orderID)
	if err != nil {
		return nil, domain.ErrOrderNotFound
	}
	if order.UserID != requesterID {
		return nil, domain.ErrOrderNotFound
	}

	resp := &OrderStatusResponse{
		OrderID:   order.ID,
		Status:    order.Status,
		Provider:  order.Provider,
		ExpiresAt: order.ExpiresAt,
	}

	if order.Status == domain.OrderStatusPaid {
		lic, licErr := s.licenseRepo.GetByOrderID(ctx, order.ID)
		if licErr == nil && lic != nil {
			resp.License = &OrderLicenseSummary{
				ID:         lic.ID,
				LicenseKey: lic.LicenseKey,
			}
		}
	} else if order.Status == domain.OrderStatusPending || order.Status == domain.OrderStatusProcessing {
		resp.RetryAfterSeconds = 2
	}

	return resp, nil
}

// VerifyPayment confirms a payment that the browser reported back.
func (s *paymentService) VerifyPayment(ctx context.Context, orderID, userID uuid.UUID, providerPaymentID, signature string) (*domain.License, error) {
	order, err := s.orderRepo.GetByID(ctx, orderID)
	if err != nil {
		return nil, errors.New("order not found")
	}
	if order.UserID != userID {
		return nil, errors.New("order not found")
	}

	gateway, err := s.gateways.ByName(order.Provider)
	if err != nil {
		return nil, err
	}
	confirmer, ok := gateway.(domain.ClientConfirmer)
	if !ok {
		return nil, fmt.Errorf("%s payments are confirmed by webhook, not by the client: %w", order.Provider, domain.ErrProviderCompletesByWebhook)
	}

	if order.Status == domain.OrderStatusPaid {
		return s.licenseRepo.GetByOrderID(ctx, order.ID)
	}

	if order.Status != domain.OrderStatusPending && order.Status != domain.OrderStatusProcessing && order.Status != domain.OrderStatusFailed {
		return nil, errors.New("order already processed")
	}
	if time.Now().After(order.ExpiresAt) {
		if updateErr := s.orderRepo.UpdateStatus(ctx, orderID, domain.OrderStatusFailed); updateErr != nil {
			return nil, fmt.Errorf("order expired and status update failed: %w", updateErr)
		}
		return nil, errors.New("order expired")
	}

	confirmation, err := confirmer.ConfirmClientPayment(ctx, order, providerPaymentID, signature)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidSignature), errors.Is(err, domain.ErrPaymentNotCaptured):
			if updateErr := s.orderRepo.UpdateStatus(ctx, orderID, domain.OrderStatusFailed); updateErr != nil {
				return nil, fmt.Errorf("%w and status update failed: %v", err, updateErr)
			}
		}
		return nil, err
	}

	if confirmation.ReportedAmountMinor > 0 && confirmation.ReportedAmountMinor != int64(order.Amount) {
		log.Printf("[ALERT][PAYMENT_AMOUNT_MISMATCH] order_id=%s provider=%s expected=%d reported=%d", order.ID, order.Provider, order.Amount, confirmation.ReportedAmountMinor)
		return nil, domain.ErrAmountMismatch
	}
	if confirmation.ReportedCurrency != "" && !strings.EqualFold(confirmation.ReportedCurrency, order.Currency) {
		log.Printf("[ALERT][PAYMENT_AMOUNT_MISMATCH] order_id=%s provider=%s expected=%s reported=%s", order.ID, order.Provider, order.Currency, confirmation.ReportedCurrency)
		return nil, domain.ErrAmountMismatch
	}

	return s.finalizePayment(ctx, order, confirmation)
}

// HandleWebhook records the event durably in PostgreSQL and executes completion.
func (s *paymentService) HandleWebhook(ctx context.Context, provider string, payload []byte, headers map[string]string) error {
	gateway, err := s.gateways.ByName(provider)
	if err != nil {
		return err
	}
	confirmer, ok := gateway.(domain.WebhookConfirmer)
	if !ok {
		return fmt.Errorf("%s does not use webhooks", gateway.Name())
	}

	confirmation, err := confirmer.ParseWebhook(payload, headers)
	if err != nil {
		return err
	}
	if confirmation == nil {
		return nil
	}

	eventID := confirmation.EventID
	if strings.TrimSpace(eventID) == "" {
		eventID = fmt.Sprintf("%s_%s", provider, confirmation.ProviderPaymentID)
	}

	if s.webhookRepo != nil {
		amt := int(confirmation.ReportedAmountMinor)
		ev := &domain.PaymentWebhookEvent{
			Provider:            provider,
			EventID:             eventID,
			EventType:           "payment.captured",
			OrderID:             &confirmation.OrderID,
			ProviderCheckoutID:  &confirmation.ProviderCheckoutID,
			ProviderPaymentID:   &confirmation.ProviderPaymentID,
			ReportedAmount:      &amt,
			ReportedCurrency:    &confirmation.ReportedCurrency,
			Payload:             payload,
			Headers:             headers,
			Status:              domain.WebhookEventStatusQueued,
		}
		existing, inserted, iErr := s.webhookRepo.InsertEvent(ctx, ev)
		if iErr != nil {
			return iErr
		}
		if !inserted && existing.Status == domain.WebhookEventStatusCompleted {
			return nil
		}

		processErr := s.processCapture(ctx, provider, confirmation)
		if processErr == nil {
			_ = s.webhookRepo.MarkCompleted(ctx, existing.ID)
			return nil
		}

		switch {
		case errors.Is(processErr, domain.ErrOrderNotFound),
			errors.Is(processErr, domain.ErrProviderMismatch),
			errors.Is(processErr, domain.ErrAmountMismatch),
			errors.Is(processErr, domain.ErrDistinctCaptureAnomaly),
			errors.Is(processErr, domain.ErrOrderNotPayable):
			_ = s.webhookRepo.MarkFailed(ctx, existing.ID, processErr.Error(), 0, domain.WebhookEventStatusNeedsReview)
			return processErr
		default:
			_ = s.webhookRepo.MarkFailed(ctx, existing.ID, processErr.Error(), 15*time.Second, domain.WebhookEventStatusFailed)
			return processErr
		}
	}

	return s.processCapture(ctx, provider, confirmation)
}

func (s *paymentService) processCapture(ctx context.Context, provider string, confirmation *domain.PaymentConfirmation) error {
	var order *domain.Order
	var err error
	if confirmation.OrderID != uuid.Nil {
		order, err = s.orderRepo.GetByID(ctx, confirmation.OrderID)
	} else if confirmation.ProviderCheckoutID != "" {
		order, err = s.orderRepo.GetByProviderCheckoutID(ctx, provider, confirmation.ProviderCheckoutID)
	} else {
		return domain.ErrOrderNotFound
	}
	if err != nil || order == nil {
		return domain.ErrOrderNotFound
	}

	if order.Provider != provider {
		return domain.ErrProviderMismatch
	}

	if confirmation.ReportedAmountMinor > 0 && confirmation.ReportedAmountMinor != int64(order.Amount) {
		log.Printf("[ALERT][PAYMENT_AMOUNT_MISMATCH] order_id=%s provider=%s expected=%d reported=%d", order.ID, order.Provider, order.Amount, confirmation.ReportedAmountMinor)
		return domain.ErrAmountMismatch
	}
	if confirmation.ReportedCurrency != "" && !strings.EqualFold(confirmation.ReportedCurrency, order.Currency) {
		log.Printf("[ALERT][PAYMENT_AMOUNT_MISMATCH] order_id=%s provider=%s expected=%s reported=%s", order.ID, order.Provider, order.Currency, confirmation.ReportedCurrency)
		return domain.ErrAmountMismatch
	}

	if order.Status == domain.OrderStatusPaid {
		existingPayment, pErr := s.paymentRepo.GetByOrderID(ctx, order.ID)
		if pErr == nil && existingPayment != nil {
			if existingPayment.ProviderPaymentID == confirmation.ProviderPaymentID {
				return nil
			}
			log.Printf("[ALERT][PAYMENT_ANOMALY_DISTINCT_CAPTURE] order_id=%s original_payment=%s second_payment=%s", order.ID, existingPayment.ProviderPaymentID, confirmation.ProviderPaymentID)
			return domain.ErrDistinctCaptureAnomaly
		}
		return nil
	}

	if order.Status == domain.OrderStatusCancelled || order.Status == domain.OrderStatusRefunded {
		log.Printf("[ALERT][PAYMENT_INVALID_STATUS] order_id=%s status=%s provider=%s", order.ID, order.Status, order.Provider)
		return domain.ErrOrderNotPayable
	}

	_, err = s.finalizePayment(ctx, order, confirmation)
	return err
}

func (s *paymentService) ProcessNextWebhookBatch(ctx context.Context, workerID string, batchSize int, leaseDuration time.Duration) (int, error) {
	if s.webhookRepo == nil {
		return 0, nil
	}
	events, err := s.webhookRepo.ClaimNextBatch(ctx, workerID, batchSize, leaseDuration)
	if err != nil {
		return 0, err
	}

	processedCount := 0
	for _, event := range events {
		gateway, gErr := s.gateways.ByName(event.Provider)
		if gErr != nil {
			_ = s.webhookRepo.MarkFailed(ctx, event.ID, gErr.Error(), 30*time.Second, domain.WebhookEventStatusFailed)
			continue
		}
		confirmer, ok := gateway.(domain.WebhookConfirmer)
		if !ok {
			_ = s.webhookRepo.MarkFailed(ctx, event.ID, "gateway does not support webhooks", 0, domain.WebhookEventStatusNeedsReview)
			continue
		}
		confirmation, pErr := confirmer.ParseWebhook(event.Payload, event.Headers)
		if pErr != nil {
			_ = s.webhookRepo.MarkFailed(ctx, event.ID, pErr.Error(), 0, domain.WebhookEventStatusNeedsReview)
			continue
		}
		if confirmation == nil {
			_ = s.webhookRepo.MarkCompleted(ctx, event.ID)
			processedCount++
			continue
		}

		cErr := s.processCapture(ctx, event.Provider, confirmation)
		if cErr == nil {
			_ = s.webhookRepo.MarkCompleted(ctx, event.ID)
			processedCount++
		} else {
			switch {
			case errors.Is(cErr, domain.ErrOrderNotFound),
				errors.Is(cErr, domain.ErrProviderMismatch),
				errors.Is(cErr, domain.ErrAmountMismatch),
				errors.Is(cErr, domain.ErrDistinctCaptureAnomaly),
				errors.Is(cErr, domain.ErrOrderNotPayable):
				_ = s.webhookRepo.MarkFailed(ctx, event.ID, cErr.Error(), 0, domain.WebhookEventStatusNeedsReview)
			default:
				backoff := time.Duration(15*(1<<event.Attempts)) * time.Second
				if backoff > 15*time.Minute {
					backoff = 15 * time.Minute
				}
				_ = s.webhookRepo.MarkFailed(ctx, event.ID, cErr.Error(), backoff, domain.WebhookEventStatusFailed)
			}
		}
	}

	return processedCount, nil
}

func (s *paymentService) finalizePayment(ctx context.Context, order *domain.Order, c *domain.PaymentConfirmation) (*domain.License, error) {
	var (
		license     *domain.License
		payment     *domain.Payment
		didFinalize bool
	)

	runner := s.getTxRunner()
	err := runner.WithTx(ctx, func(tx domain.PaymentTx) error {
		changed, err := tx.OrderRepo().MarkPaidIfPayable(ctx, order.ID, c.ProviderPaymentID)
		if err != nil {
			return err
		}
		if !changed {
			existingLic, licErr := tx.LicenseRepo().GetByOrderID(ctx, order.ID)
			if licErr == nil && existingLic != nil {
				license = existingLic
				didFinalize = false
				return nil
			}
			return domain.ErrOrderNotPayable
		}

		now := time.Now()
		payment = &domain.Payment{
			OrderID:           order.ID,
			Provider:          order.Provider,
			ProviderPaymentID: c.ProviderPaymentID,
			ProviderSignature: c.Signature,
			Amount:            order.Amount,
			Currency:          order.Currency,
			Status:            domain.PaymentStatusCaptured,
			Method:            c.Method,
			Email:             c.Email,
			CapturedAt:        &now,
		}
		if err := tx.PaymentRepo().Create(ctx, payment); err != nil {
			return err
		}

		lic, err := s.issueLicenseWithRepo(ctx, order, tx.LicenseRepo())
		if err != nil {
			return fmt.Errorf("payment ok but license failed: %w", err)
		}
		license = lic
		didFinalize = true
		return nil
	})
	if err != nil {
		return nil, err
	}

	if didFinalize && payment != nil && license != nil {
		go func() {
			emailCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := s.sendReceiptEmail(emailCtx, order, payment, license); err != nil {
				log.Printf("PaymentService.finalizePayment receipt email failed. order_id=%s err=%v", order.ID, err)
			}
		}()
	}

	return license, nil
}

func (s *paymentService) InitiateRefund(ctx context.Context, input CreateRefundInput) (*OrderRefundResponse, error) {
	if input.OrderID == uuid.Nil {
		return nil, errors.New("invalid order id")
	}
	if strings.TrimSpace(input.Reason) == "" {
		return nil, errors.New("refund reason is required")
	}

	key, _ := NormalizeIdempotencyKey(input.IdempotencyKey)
	input.IdempotencyKey = key

	if s.refundRepo != nil {
		existing, err := s.refundRepo.GetByIdempotencyKey(ctx, key)
		if err == nil && existing != nil {
			return &OrderRefundResponse{
				ID:               existing.ID,
				OrderID:          existing.OrderID,
				PaymentID:        existing.PaymentID,
				Amount:           existing.Amount,
				Currency:         existing.Currency,
				Status:           existing.Status,
				Reason:           existing.Reason,
				ProviderRefundID: existing.ProviderRefundID,
				CreatedAt:        existing.CreatedAt,
			}, nil
		}
	}

	order, err := s.orderRepo.GetByID(ctx, input.OrderID)
	if err != nil || order == nil {
		return nil, domain.ErrOrderNotFound
	}
	if order.Status != domain.OrderStatusPaid {
		return nil, domain.ErrRefundNotEligible
	}

	payment, err := s.paymentRepo.GetByOrderID(ctx, order.ID)
	if err != nil || payment == nil || payment.Status != domain.PaymentStatusCaptured {
		return nil, domain.ErrRefundNotEligible
	}

	if s.refundRepo != nil {
		existingRefunds, rErr := s.refundRepo.GetByOrderID(ctx, order.ID)
		if rErr == nil {
			for _, r := range existingRefunds {
				if r.Status == domain.RefundStatusSucceeded || r.Status == domain.RefundStatusPending || r.Status == domain.RefundStatusRequested {
					return nil, domain.ErrRefundAlreadyExists
				}
			}
		}
	}

	refund := &domain.OrderRefund{
		OrderID:           order.ID,
		PaymentID:         payment.ID,
		UserID:            order.UserID,
		AdminID:           input.AdminID,
		IdempotencyKey:    key,
		Amount:            payment.Amount,
		Currency:          payment.Currency,
		Provider:          payment.Provider,
		ProviderPaymentID: payment.ProviderPaymentID,
		Reason:            input.Reason,
		Notes:             &input.Notes,
		Status:            domain.RefundStatusRequested,
	}

	if s.refundRepo != nil {
		reserved, created, rErr := s.refundRepo.ReserveRefund(ctx, refund)
		if rErr != nil {
			return nil, rErr
		}
		if !created {
			return &OrderRefundResponse{
				ID:               reserved.ID,
				OrderID:          reserved.OrderID,
				PaymentID:        reserved.PaymentID,
				Amount:           reserved.Amount,
				Currency:         reserved.Currency,
				Status:           reserved.Status,
				Reason:           reserved.Reason,
				ProviderRefundID: reserved.ProviderRefundID,
				CreatedAt:        reserved.CreatedAt,
			}, nil
		}
		refund = reserved
	}

	gateway, err := s.gateways.ByName(payment.Provider)
	if err != nil {
		return nil, err
	}
	refunder, ok := gateway.(domain.Refunder)
	if !ok {
		return nil, fmt.Errorf("provider %s does not support refunds", payment.Provider)
	}

	res, err := refunder.Refund(ctx, domain.RefundRequest{
		IdempotencyKey:    key,
		OrderID:           order.ID,
		ProviderPaymentID: payment.ProviderPaymentID,
		AmountMinor:       payment.Amount,
		Currency:          payment.Currency,
		Reason:            input.Reason,
	})
	if err != nil {
		errStr := err.Error()
		if s.refundRepo != nil {
			_ = s.refundRepo.UpdateStatus(ctx, refund.ID, domain.RefundStatusFailed, nil, &errStr)
		}
		return nil, fmt.Errorf("%w: %v", domain.ErrRefundProviderFailed, err)
	}

	refundStatus := res.Status
	txErr := s.getTxRunner().WithTx(ctx, func(tx domain.PaymentTx) error {
		if tx.RefundRepo() != nil {
			if err := tx.RefundRepo().UpdateStatus(ctx, refund.ID, refundStatus, &res.ProviderRefundID, nil); err != nil {
				return err
			}
		}
		if refundStatus == domain.RefundStatusSucceeded {
			if err := tx.OrderRepo().MarkRefunded(ctx, order.ID); err != nil {
				return err
			}
			if err := tx.PaymentRepo().UpdateStatus(ctx, payment.ID, domain.PaymentStatusRefunded); err != nil {
				return err
			}
			lic, lErr := tx.LicenseRepo().GetByOrderID(ctx, order.ID)
			if lErr == nil && lic != nil {
				if err := tx.LicenseRepo().Revoke(ctx, lic.ID, "Full refund issued by admin: "+input.Reason); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}

	return &OrderRefundResponse{
		ID:               refund.ID,
		OrderID:          order.ID,
		PaymentID:        payment.ID,
		Amount:           payment.Amount,
		Currency:         payment.Currency,
		Status:           refundStatus,
		Reason:           input.Reason,
		ProviderRefundID: &res.ProviderRefundID,
		CreatedAt:        time.Now(),
	}, nil
}

func (s *paymentService) GetOrderRefunds(ctx context.Context, orderID uuid.UUID) ([]domain.OrderRefund, error) {
	if s.refundRepo == nil {
		return nil, nil
	}
	return s.refundRepo.GetByOrderID(ctx, orderID)
}

func (s *paymentService) ListWebhookEvents(ctx context.Context, filter domain.WebhookEventFilter, limit, offset int) ([]domain.PaymentWebhookEvent, int, error) {
	if s.webhookRepo == nil {
		return nil, 0, nil
	}
	return s.webhookRepo.ListEvents(ctx, filter, limit, offset)
}

func (s *paymentService) RequeueWebhookEvent(ctx context.Context, eventID uuid.UUID) error {
	if s.webhookRepo == nil {
		return nil
	}
	return s.webhookRepo.Requeue(ctx, eventID)
}

// GetOrder returns an order only to its buyer or to the producer who owns the
// ordered spec. Anyone else gets the same "not found" error as a missing order.
func (s *paymentService) GetOrder(ctx context.Context, orderID, requesterID uuid.UUID) (*domain.Order, error) {
	order, err := s.orderRepo.GetByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if order.UserID == requesterID {
		order.FillClientFields()
		return order, nil
	}
	if spec, specErr := s.specFinder.FindByIDIncludingDeleted(ctx, order.SpecID); specErr == nil && spec != nil && spec.ProducerID == requesterID {
		order.FillClientFields()
		return order, nil
	}
	return nil, errors.New("order not found")
}

func (s *paymentService) GetUserOrders(ctx context.Context, userID uuid.UUID, page int) ([]domain.Order, error) {
	limit := 20
	offset := (page - 1) * limit
	if offset < 0 {
		offset = 0
	}
	orders, err := s.orderRepo.ListByUser(ctx, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	for i := range orders {
		orders[i].FillClientFields()
	}
	return orders, nil
}

func (s *paymentService) GetUserLicenses(ctx context.Context, userID uuid.UUID, page int, search, licenseType string) ([]domain.License, int, error) {
	limit := 5
	offset := (page - 1) * limit
	if offset < 0 {
		offset = 0
	}
	licenses, total, err := s.licenseRepo.ListByUser(ctx, userID, limit, offset, search, licenseType)
	if err != nil {
		return nil, 0, err
	}

	for i := range licenses {
		if licenses[i].SpecImage != nil && *licenses[i].SpecImage != "" {
			key, err := s.fileService.GetKeyFromUrl(*licenses[i].SpecImage)
			if err == nil {
				signedURL, err := s.fileService.GetPresignedURL(ctx, key, 1*time.Hour)
				if err == nil {
					licenses[i].SpecImage = &signedURL
				}
			}
		}
	}

	return licenses, total, nil
}

func (s *paymentService) issueLicense(ctx context.Context, order *domain.Order) (*domain.License, error) {
	return s.issueLicenseWithRepo(ctx, order, s.licenseRepo)
}

func (s *paymentService) issueLicenseWithRepo(ctx context.Context, order *domain.Order, licenseRepo domain.LicenseRepository) (*domain.License, error) {
	licenseOptionIDStr, ok := order.Notes["license_option_id"].(string)
	if !ok {
		return nil, errors.New("license_option_id missing")
	}
	licenseOptionID, err := uuid.Parse(licenseOptionIDStr)
	if err != nil {
		return nil, errors.New("invalid license_option_id")
	}

	licenseKeyID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("failed to generate uuid: %w", err)
	}

	license := &domain.License{
		OrderID:         order.ID,
		UserID:          order.UserID,
		SpecID:          order.SpecID,
		LicenseOptionID: licenseOptionID,
		LicenseType:     order.LicenseType,
		PurchasePrice:   order.Amount,
		Currency:        order.Currency,
		LicenseKey:      fmt.Sprintf("LIC-%s", licenseKeyID.String()),
		IsActive:        true,
		IsRevoked:       false,
		DownloadsCount:  0,
		IssuedAt:        time.Now(),
	}

	return license, licenseRepo.Create(ctx, license)
}

func (s *paymentService) GetLicenseDownloads(ctx context.Context, licenseID, userID uuid.UUID) (*LicenseDownloadsResponse, error) {
	license, err := s.licenseRepo.GetByID(ctx, licenseID)
	if err != nil {
		return nil, errors.New("license not found")
	}

	if license.UserID != userID {
		return nil, errors.New("unauthorized: you do not own this license")
	}
	if !license.IsActive {
		return nil, errors.New("license is not active")
	}
	if license.IsRevoked {
		return nil, errors.New("license has been revoked")
	}

	spec, err := s.specFinder.FindByIDIncludingDeleted(ctx, license.SpecID)
	if err != nil {
		return nil, errors.New("spec not found")
	}

	response := &LicenseDownloadsResponse{
		LicenseID:   license.ID.String(),
		LicenseType: license.LicenseType,
		SpecTitle:   spec.Title,
		ExpiresIn:   3600,
	}

	getSignedURL := func(fileURL string) *string {
		if fileURL == "" {
			return nil
		}
		key, err := s.fileService.GetKeyFromUrl(fileURL)
		if err != nil {
			return &fileURL
		}
		signedURL, err := s.fileService.GetPresignedURL(ctx, key, 1*time.Hour)
		if err != nil {
			return &fileURL
		}
		return &signedURL
	}

	switch license.LicenseType {
	case "Basic":
		if spec.PreviewUrl != "" {
			response.MP3URL = getSignedURL(spec.PreviewUrl)
		}
	case "Premium":
		if spec.PreviewUrl != "" {
			response.MP3URL = getSignedURL(spec.PreviewUrl)
		}
		if spec.WavUrl != nil && *spec.WavUrl != "" {
			response.WAVURL = getSignedURL(*spec.WavUrl)
		}
	case "Trackout", "Unlimited":
		if spec.PreviewUrl != "" {
			response.MP3URL = getSignedURL(spec.PreviewUrl)
		}
		if spec.WavUrl != nil && *spec.WavUrl != "" {
			response.WAVURL = getSignedURL(*spec.WavUrl)
		}
		if spec.StemsUrl != nil && *spec.StemsUrl != "" {
			response.StemsURL = getSignedURL(*spec.StemsUrl)
		}
	}

	_ = s.licenseRepo.IncrementDownloads(ctx, licenseID)
	return response, nil
}

func (s *paymentService) GetProducerOrders(ctx context.Context, producerID uuid.UUID, page, limit int) (*ProducerOrderResponse, error) {
	offset := (page - 1) * limit
	if offset < 0 {
		offset = 0
	}

	orders, total, err := s.orderRepo.ListByProducer(ctx, producerID, limit, offset)
	if err != nil {
		return nil, err
	}

	orderDtos := make([]ProducerOrderDto, len(orders))
	for i, o := range orders {
		o.FillClientFields()
		orderDtos[i] = ProducerOrderDto{
			ID:              o.ID,
			Amount:          float64(o.Amount) / 100.0,
			Currency:        o.Currency,
			Status:          o.Status,
			CreatedAt:       o.CreatedAt,
			LicenseType:     o.LicenseType,
			BuyerName:       o.BuyerName,
			BuyerEmail:      o.BuyerEmail,
			SpecTitle:       o.SpecTitle,
			Provider:        o.Provider,
			RazorpayOrderID: o.RazorpayOrderID,
		}
	}

	return &ProducerOrderResponse{
		Orders: orderDtos,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func (s *paymentService) sendReceiptEmail(ctx context.Context, order *domain.Order, payment *domain.Payment, license *domain.License) error {
	if s.emailSender == nil || s.userFinder == nil {
		return nil
	}
	user, err := s.userFinder.FindByID(ctx, order.UserID)
	if err != nil {
		return err
	}

	buyerEmail := user.Email
	if payment.Email != nil && strings.TrimSpace(*payment.Email) != "" {
		buyerEmail = *payment.Email
	}
	specTitle, _ := order.Notes["spec_title"].(string)
	if specTitle == "" {
		specTitle = "Blueprint purchase"
	}

	return s.emailSender.Send(ctx, sharedemail.BuildPaymentReceiptEmail(sharedemail.ReceiptData{
		BuyerName:     user.Name,
		BuyerEmail:    buyerEmail,
		SpecTitle:     specTitle,
		LicenseType:   order.LicenseType,
		AmountDisplay: formatMoney(order.Amount, order.Currency),
		OrderID:       order.ID.String(),
		PaymentID:     payment.ProviderPaymentID,
		LicenseID:     license.ID.String(),
	}, s.appBaseURL))
}

func formatMoney(amount int, currency string) string {
	switch strings.ToUpper(currency) {
	case "INR":
		return fmt.Sprintf("INR %.2f", float64(amount)/100.0)
	default:
		return fmt.Sprintf("%s %.2f", strings.ToUpper(currency), float64(amount)/100.0)
	}
}
