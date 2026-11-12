package http

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/gateway/middleware"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/application"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
	"github.com/saransh1220/blueprint-audio/internal/shared/money"
)

type PaymentHandler struct {
	service application.PaymentService
}

func NewPaymentHandler(service application.PaymentService) *PaymentHandler {
	return &PaymentHandler{service: service}
}

func (h *PaymentHandler) ProviderWebhook(w http.ResponseWriter, r *http.Request) {
	provider := strings.ToLower(strings.TrimSpace(r.PathValue("provider")))
	if provider == "" {
		provider = domain.ProviderDodo
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024) // 1MB cap
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid webhook body", http.StatusBadRequest)
		return
	}

	headers := make(map[string]string)
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[strings.ToLower(k)] = v[0]
		}
	}

	if err := h.service.HandleWebhook(r.Context(), provider, payload, headers); err != nil {
		traceID := middleware.GetRequestID(r.Context())
		switch {
		case errors.Is(err, domain.ErrInvalidSignature),
			errors.Is(err, domain.ErrTimestampToleranceExceeded),
			errors.Is(err, domain.ErrMissingWebhookSecret):
			log.Printf("[ALERT][PAYMENT_UNAUTHORIZED] trace_id=%s provider=%s error=%v", traceID, provider, err)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		case errors.Is(err, domain.ErrMalformedPayload),
			errors.Is(err, domain.ErrMissingPaymentID):
			log.Printf("[ALERT][PAYMENT_BAD_REQUEST] trace_id=%s provider=%s error=%v", traceID, provider, err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		case errors.Is(err, domain.ErrOrderNotFound),
			errors.Is(err, domain.ErrProviderMismatch),
			errors.Is(err, domain.ErrAmountMismatch),
			errors.Is(err, domain.ErrDistinctCaptureAnomaly),
			errors.Is(err, domain.ErrOrderNotPayable):
			// Permanent business rejection or anomaly: alert, return 200 so provider stops retrying!
			log.Printf("[ALERT][PAYMENT_REJECTED] trace_id=%s provider=%s reason=%v", traceID, provider, err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"received": true, "status": "ignored"})
			return
		case strings.Contains(strings.ToLower(err.Error()), "not supported") || strings.Contains(strings.ToLower(err.Error()), "unknown"):
			log.Printf("[ALERT][PAYMENT_UNKNOWN_PROVIDER] trace_id=%s provider=%s error=%v", traceID, provider, err)
			http.Error(w, "not found", http.StatusNotFound)
			return
		default:
			log.Printf("[ALERT][PAYMENT_FAILURE] trace_id=%s provider=%s error=%v", traceID, provider, err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"received": true})
}

func (h *PaymentHandler) DodoWebhook(w http.ResponseWriter, r *http.Request) {
	h.ProviderWebhook(w, r)
}

func readIdempotencyKey(r *http.Request, fallback string) (string, bool) {
	values := r.Header.Values("Idempotency-Key")
	if len(values) > 1 {
		return "", false
	}
	key := strings.TrimSpace(fallback)
	if len(values) == 1 {
		key = strings.TrimSpace(values[0])
		if key == "" {
			return "", false
		}
	}
	if len(key) > 128 {
		return "", false
	}
	for _, char := range key {
		if char < '!' || char > '~' || char == ',' {
			return "", false
		}
	}
	return key, true
}

func (h *PaymentHandler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(middleware.ContextKeyUserId).(uuid.UUID)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// Parse request body
	var req struct {
		SpecID              string  `json:"spec_id"`
		LicenseOptionID     string  `json:"license_option_id"`
		Currency            string  `json:"currency,omitempty"`
		ExpectedPriceID     *string `json:"expected_price_id,omitempty"`
		ExpectedRevision    *int64  `json:"expected_revision,omitempty"`
		ExpectedAmountMinor *int    `json:"expected_amount_minor,omitempty"`
		ExpectedCurrency    *string `json:"expected_currency,omitempty"`
		IdempotencyKey      string  `json:"idempotency_key,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	// Parse UUIDs
	specID, err := uuid.Parse(req.SpecID)
	if err != nil {
		http.Error(w, "invalid spec_id", http.StatusBadRequest)
		return
	}
	licenseOptionID, err := uuid.Parse(req.LicenseOptionID)
	if err != nil {
		http.Error(w, "invalid license_option_id", http.StatusBadRequest)
		return
	}

	currency := strings.TrimSpace(req.Currency)
	if currency == "" {
		currency = middleware.BuyerCurrencyFromContext(r.Context())
	}

	var expectedPriceID *uuid.UUID
	if req.ExpectedPriceID != nil && strings.TrimSpace(*req.ExpectedPriceID) != "" {
		parsedPriceID, err := uuid.Parse(strings.TrimSpace(*req.ExpectedPriceID))
		if err != nil {
			http.Error(w, "invalid expected_price_id", http.StatusBadRequest)
			return
		}
		expectedPriceID = &parsedPriceID
	}

	idempotencyKey, valid := readIdempotencyKey(r, req.IdempotencyKey)
	if !valid {
		http.Error(w, "invalid Idempotency-Key", http.StatusBadRequest)
		return
	}

	input := application.CreateOrderInput{
		UserID:              userID,
		SpecID:              specID,
		LicenseOptionID:     licenseOptionID,
		Currency:            currency,
		ExpectedPriceID:     expectedPriceID,
		ExpectedRevision:    req.ExpectedRevision,
		ExpectedAmountMinor: req.ExpectedAmountMinor,
		ExpectedCurrency:    req.ExpectedCurrency,
		IdempotencyKey:      idempotencyKey,
	}

	order, err := h.service.CreateOrderWithOffer(r.Context(), input)
	if err != nil {
		if errors.Is(err, domain.ErrIdempotencyConflict) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{
				"code":    "IDEMPOTENCY_CONFLICT",
				"message": "An order with this idempotency key already exists with different parameters",
			})
			return
		}
		if errors.Is(err, domain.ErrOrderReservedInFlight) {
			w.Header().Set("Content-Type", "application/json")
			retry := 2
			response := map[string]any{
				"code": "CHECKOUT_IN_FLIGHT", "message": "Checkout creation is in progress",
			}
			var pending *domain.CheckoutPendingError
			if errors.As(err, &pending) {
				response["order_id"] = pending.OrderID
				if pending.Reconciling {
					retry = 30
					response["code"] = "CHECKOUT_RECONCILING"
					response["message"] = "Checkout outcome is being verified"
				}
			}
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			response["retry_after"] = retry
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(response)
			return
		}
		var priceMismatch *application.PriceMismatchError
		if errors.As(err, &priceMismatch) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"code":          "PRICE_CHANGED",
				"message":       priceMismatch.Message,
				"current_offer": priceMismatch.CurrentOffer,
			})
			return
		}
		if errors.Is(err, money.ErrInvalidCurrency) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"code":    "INVALID_CURRENCY",
				"message": "Requested currency is invalid or unsupported",
			})
			return
		}
		if errors.Is(err, money.ErrPriceUnavailable) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"code":    "PRICE_UNAVAILABLE",
				"message": "Selected license cannot be purchased in this currency",
			})
			return
		}
		traceID := middleware.GetRequestID(r.Context())
		log.Printf("[ALERT][PAYMENT_ORDER_ERROR] trace_id=%s user_id=%s error=%v", traceID, userID, err)
		http.Error(w, "failed to create order", http.StatusInternalServerError)
		return
	}
	// Return order
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(order)
}

func (h *PaymentHandler) VerifyPayment(w http.ResponseWriter, r *http.Request) {
	// Auth check
	userID, ok := r.Context().Value(middleware.ContextKeyUserId).(uuid.UUID)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse request
	var req struct {
		OrderID           string `json:"order_id"`
		RazorpayPaymentID string `json:"razorpay_payment_id"`
		RazorpaySignature string `json:"razorpay_signature"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Parse order ID
	orderID, err := uuid.Parse(req.OrderID)
	if err != nil {
		http.Error(w, "invalid order_id", http.StatusBadRequest)
		return
	}

	// Verify payment and issue license
	license, err := h.service.VerifyPayment(
		r.Context(),
		orderID,
		userID,
		req.RazorpayPaymentID,
		req.RazorpaySignature,
	)
	if err != nil {
		if errors.Is(err, domain.ErrProviderCompletesByWebhook) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{
				"error":   "PROVIDER_COMPLETES_BY_WEBHOOK",
				"message": "This order is confirmed via webhook",
			})
			return
		}

		traceID := middleware.GetRequestID(r.Context())
		log.Printf("[ALERT][PAYMENT_FAILURE] trace_id=%s order_id=%s user_id=%s error=%v", traceID, orderID, userID, err)
		http.Error(w, "payment verification failed", http.StatusBadRequest)
		return
	}

	// Return success with license
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"license": license,
		"message": "Payment successful! License issued.",
	})
}

func (h *PaymentHandler) CreateRefund(w http.ResponseWriter, r *http.Request) {
	adminID, ok := r.Context().Value(middleware.ContextKeyUserId).(uuid.UUID)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	orderIDStr := r.PathValue("id")
	orderID, err := uuid.Parse(orderIDStr)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"code": "INVALID_ORDER_ID", "message": "invalid order id"})
		return
	}

	idempotencyKey, valid := readIdempotencyKey(r, "")
	if !valid {
		http.Error(w, "invalid Idempotency-Key", http.StatusBadRequest)
		return
	}

	var body struct {
		Reason string `json:"reason"`
		Notes  string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"code": "INVALID_BODY", "message": "invalid request body"})
		return
	}

	if strings.TrimSpace(body.Reason) == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"code": "REASON_REQUIRED", "message": "refund reason is required"})
		return
	}

	resp, err := h.service.InitiateRefund(r.Context(), application.CreateRefundInput{
		OrderID:        orderID,
		AdminID:        adminID,
		IdempotencyKey: idempotencyKey,
		Reason:         body.Reason,
		Notes:          body.Notes,
	})
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrOrderNotFound):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"code": "ORDER_NOT_FOUND", "message": "order not found"})
			return
		case errors.Is(err, domain.ErrRefundAlreadyExists):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"code": "REFUND_ALREADY_EXISTS", "message": "a refund operation already exists for this payment"})
			return
		case errors.Is(err, domain.ErrRefundNotEligible):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"code": "REFUND_NOT_ELIGIBLE", "message": "order is not eligible for refund"})
			return
		case errors.Is(err, domain.ErrRefundProviderFailed):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"code": "PROVIDER_REFUND_FAILED", "message": "refund outcome needs review"})
			return
		default:
			log.Printf("[ALERT][REFUND_ERROR] order_id=%s admin_id=%s error=%v", orderID, adminID, err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"code": "INTERNAL_ERROR", "message": "failed to process refund"})
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

func (h *PaymentHandler) GetRefunds(w http.ResponseWriter, r *http.Request) {
	orderIDStr := r.PathValue("id")
	orderID, err := uuid.Parse(orderIDStr)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"code": "INVALID_ORDER_ID", "message": "invalid order id"})
		return
	}

	refunds, err := h.service.GetOrderRefunds(r.Context(), orderID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"code": "INTERNAL_ERROR", "message": "failed to load refunds"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"data": refunds})
}

func (h *PaymentHandler) ListWebhookEvents(w http.ResponseWriter, r *http.Request) {
	filter := domain.WebhookEventFilter{
		Provider: r.URL.Query().Get("provider"),
		Status:   r.URL.Query().Get("status"),
	}
	if orderIDStr := r.URL.Query().Get("order_id"); orderIDStr != "" {
		if id, err := uuid.Parse(orderIDStr); err == nil {
			filter.OrderID = &id
		}
	}

	page := 1
	if pStr := r.URL.Query().Get("page"); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil && p > 0 {
			page = p
		}
	}
	limit := 20
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	offset := (page - 1) * limit
	events, total, err := h.service.ListWebhookEvents(r.Context(), filter, limit, offset)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"code": "INTERNAL_ERROR", "message": "failed to load webhook events"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"data": events,
		"metadata": map[string]any{
			"total": total,
			"page":  page,
			"limit": limit,
		},
	})
}

func (h *PaymentHandler) RequeueWebhookEvent(w http.ResponseWriter, r *http.Request) {
	eventIDStr := r.PathValue("id")
	eventID, err := uuid.Parse(eventIDStr)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"code": "INVALID_EVENT_ID", "message": "invalid event id"})
		return
	}

	if err := h.service.RequeueWebhookEvent(r.Context(), eventID); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"code": "INTERNAL_ERROR", "message": "failed to requeue event"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"requeued": true})
}

func (h *PaymentHandler) GetOrder(w http.ResponseWriter, r *http.Request) {
	// Auth check
	userID, ok := r.Context().Value(middleware.ContextKeyUserId).(uuid.UUID)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse order ID from URL path
	orderIDStr := r.PathValue("id")
	orderID, err := uuid.Parse(orderIDStr)
	if err != nil {
		http.Error(w, "invalid order_id", http.StatusBadRequest)
		return
	}

	order, err := h.service.GetOrder(r.Context(), orderID, userID)
	if err != nil {
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(order)
}

func (h *PaymentHandler) GetOrderStatus(w http.ResponseWriter, r *http.Request) {
	requesterID, ok := r.Context().Value(middleware.ContextKeyUserId).(uuid.UUID)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	orderIDStr := r.PathValue("id")
	orderID, err := uuid.Parse(orderIDStr)
	if err != nil {
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}

	status, err := h.service.GetOrderStatus(r.Context(), orderID, requesterID)
	if err != nil {
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (h *PaymentHandler) GetUserOrders(w http.ResponseWriter, r *http.Request) {
	// Auth check
	userID, ok := r.Context().Value(middleware.ContextKeyUserId).(uuid.UUID)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse pagination
	page := 1
	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	// Fetch orders
	orders, err := h.service.GetUserOrders(r.Context(), userID, page)
	if err != nil {
		http.Error(w, "failed to fetch orders", http.StatusInternalServerError)
		return
	}

	// Return orders
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(orders)
}

func (h *PaymentHandler) GetUserLicenses(w http.ResponseWriter, r *http.Request) {
	// Auth check
	userID, ok := r.Context().Value(middleware.ContextKeyUserId).(uuid.UUID)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse pagination and filters
	page := 1
	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	search := r.URL.Query().Get("q")
	licenseType := r.URL.Query().Get("type")

	// Fetch licenses
	licenses, total, err := h.service.GetUserLicenses(r.Context(), userID, page, search, licenseType)
	if err != nil {
		http.Error(w, "failed to fetch licenses", http.StatusInternalServerError)
		return
	}

	// Return licenses
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": licenses,
		"metadata": map[string]interface{}{
			"total":    total,
			"page":     page,
			"per_page": 5, // Limit 5 for testing
		},
	})
}

func (h *PaymentHandler) GetLicenseDownloads(w http.ResponseWriter, r *http.Request) {
	licenseIDStr := r.PathValue("id")
	licenseID, err := uuid.Parse(licenseIDStr)

	if err != nil {
		http.Error(w, `{"error": "invalid license ID"}`, http.StatusBadRequest)
		return
	}

	userID, ok := r.Context().Value(middleware.ContextKeyUserId).(uuid.UUID)

	if !ok {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	downloads, err := h.service.GetLicenseDownloads(r.Context(), licenseID, userID)

	if err != nil {
		statusCode := http.StatusInternalServerError
		if err.Error() == "license not found" || err.Error() == "spec not found" {
			statusCode = http.StatusNotFound
		} else if err.Error() == "unauthorized: you do not own this license" {
			statusCode = http.StatusForbidden
		} else if err.Error() == "license is not active" || err.Error() == "license has been revoked" {
			statusCode = http.StatusForbidden
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(downloads)
}

func (h *PaymentHandler) GetProducerOrders(w http.ResponseWriter, r *http.Request) {
	// Get authenticated user (producer)
	producerID, ok := r.Context().Value(middleware.ContextKeyUserId).(uuid.UUID)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse pagination
	page := 1
	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	limit := 10
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	// Fetch orders
	response, err := h.service.GetProducerOrders(r.Context(), producerID, page, limit)
	if err != nil {
		log.Printf("PaymentHandler.GetProducerOrders failed: %v", err)
		http.Error(w, "failed to fetch producer orders", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (h *PaymentHandler) ReconcileOrderCheckout(w http.ResponseWriter, r *http.Request) {
	orderID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid order id", http.StatusBadRequest)
		return
	}
	reference := strings.TrimSpace(r.URL.Query().Get("provider_reference"))
	if len(reference) > 255 || strings.IndexFunc(reference, func(c rune) bool {
		return !(c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '_')
	}) >= 0 {
		http.Error(w, "invalid provider reference", http.StatusBadRequest)
		return
	}
	err = h.service.ReconcileOrderCheckout(r.Context(), orderID, reference)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, domain.ErrOrderNotFound):
		http.Error(w, "order not found", http.StatusNotFound)
	case errors.Is(err, domain.ErrProviderMismatch), errors.Is(err, domain.ErrAmountMismatch):
		http.Error(w, "provider reference does not match this order", http.StatusConflict)
	case errors.Is(err, domain.ErrOrderReservedInFlight):
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusAccepted)
	default:
		log.Printf("[CHECKOUT_RECONCILIATION] order_id=%s error=%v", orderID, err)
		http.Error(w, "could not reconcile checkout", http.StatusInternalServerError)
	}
}
