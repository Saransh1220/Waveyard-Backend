package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/gateway/middleware"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/application"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
	paymenthttp "github.com/saransh1220/blueprint-audio/internal/modules/payment/interfaces/http"
	"github.com/saransh1220/blueprint-audio/internal/shared/money"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type refundMockService struct {
	mockPaymentService
	createOrderWithOfferFn func(ctx context.Context, input application.CreateOrderInput) (*domain.Order, error)
	verifyPaymentFn        func(ctx context.Context, orderID, userID uuid.UUID, paymentID, signature string) (*domain.License, error)
	handleWebhookFn        func(ctx context.Context, provider string, payload []byte, headers map[string]string) error
	initiateRefundFn       func(ctx context.Context, input application.CreateRefundInput) (*application.OrderRefundResponse, error)
	getOrderRefundsFn      func(ctx context.Context, orderID uuid.UUID) ([]domain.OrderRefund, error)
	listWebhookEventsFn    func(ctx context.Context, filter domain.WebhookEventFilter, limit, offset int) ([]domain.PaymentWebhookEvent, int, error)
	requeueWebhookFn       func(ctx context.Context, eventID uuid.UUID) error
}

func (m refundMockService) CreateOrderWithOffer(ctx context.Context, input application.CreateOrderInput) (*domain.Order, error) {
	if m.createOrderWithOfferFn != nil {
		return m.createOrderWithOfferFn(ctx, input)
	}
	return m.mockPaymentService.CreateOrderWithOffer(ctx, input)
}

func (m refundMockService) VerifyPayment(ctx context.Context, orderID, userID uuid.UUID, paymentID, signature string) (*domain.License, error) {
	if m.verifyPaymentFn != nil {
		return m.verifyPaymentFn(ctx, orderID, userID, paymentID, signature)
	}
	return m.mockPaymentService.VerifyPayment(ctx, orderID, userID, paymentID, signature)
}

func (m refundMockService) HandleWebhook(ctx context.Context, provider string, payload []byte, headers map[string]string) error {
	if m.handleWebhookFn != nil {
		return m.handleWebhookFn(ctx, provider, payload, headers)
	}
	return m.mockPaymentService.HandleWebhook(ctx, provider, payload, headers)
}

func (m refundMockService) InitiateRefund(ctx context.Context, input application.CreateRefundInput) (*application.OrderRefundResponse, error) {
	if m.initiateRefundFn != nil {
		return m.initiateRefundFn(ctx, input)
	}
	return nil, nil
}

func (m refundMockService) GetOrderRefunds(ctx context.Context, orderID uuid.UUID) ([]domain.OrderRefund, error) {
	if m.getOrderRefundsFn != nil {
		return m.getOrderRefundsFn(ctx, orderID)
	}
	return nil, nil
}

func (m refundMockService) ListWebhookEvents(ctx context.Context, filter domain.WebhookEventFilter, limit, offset int) ([]domain.PaymentWebhookEvent, int, error) {
	if m.listWebhookEventsFn != nil {
		return m.listWebhookEventsFn(ctx, filter, limit, offset)
	}
	return nil, 0, nil
}

func (m refundMockService) RequeueWebhookEvent(ctx context.Context, eventID uuid.UUID) error {
	if m.requeueWebhookFn != nil {
		return m.requeueWebhookFn(ctx, eventID)
	}
	return nil
}

func TestPaymentHandler_AdminRefunds(t *testing.T) {
	oid := uuid.New()
	adminID := uuid.New()

	t.Run("CreateRefund requires admin auth", func(t *testing.T) {
		h := paymenthttp.NewPaymentHandler(refundMockService{})
		req := httptest.NewRequest(http.MethodPost, "/admin/orders/"+oid.String()+"/refunds", strings.NewReader(`{"reason":"test"}`))
		w := httptest.NewRecorder()
		h.CreateRefund(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("CreateRefund invalid order id returns 400", func(t *testing.T) {
		h := paymenthttp.NewPaymentHandler(refundMockService{})
		req := httptest.NewRequest(http.MethodPost, "/admin/orders/not-a-uuid/refunds", strings.NewReader(`{"reason":"test"}`))
		req.SetPathValue("id", "not-a-uuid")
		req = req.WithContext(context.WithValue(req.Context(), middleware.ContextKeyUserId, adminID))
		w := httptest.NewRecorder()
		h.CreateRefund(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("CreateRefund invalid body or empty reason returns 400", func(t *testing.T) {
		h := paymenthttp.NewPaymentHandler(refundMockService{})

		// invalid body
		req := httptest.NewRequest(http.MethodPost, "/admin/orders/"+oid.String()+"/refunds", strings.NewReader(`{invalid json`))
		req.SetPathValue("id", oid.String())
		req = req.WithContext(context.WithValue(req.Context(), middleware.ContextKeyUserId, adminID))
		w := httptest.NewRecorder()
		h.CreateRefund(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)

		// empty reason
		req = httptest.NewRequest(http.MethodPost, "/admin/orders/"+oid.String()+"/refunds", strings.NewReader(`{"reason":"   "}`))
		req.SetPathValue("id", oid.String())
		req = req.WithContext(context.WithValue(req.Context(), middleware.ContextKeyUserId, adminID))
		w = httptest.NewRecorder()
		h.CreateRefund(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("CreateRefund error mappings", func(t *testing.T) {
		cases := []struct {
			name       string
			err        error
			wantStatus int
			wantCode   string
		}{
			{"order not found", domain.ErrOrderNotFound, http.StatusNotFound, "ORDER_NOT_FOUND"},
			{"refund exists", domain.ErrRefundAlreadyExists, http.StatusConflict, "REFUND_ALREADY_EXISTS"},
			{"refund not eligible", domain.ErrRefundNotEligible, http.StatusBadRequest, "REFUND_NOT_ELIGIBLE"},
			{"provider failed", domain.ErrRefundProviderFailed, http.StatusBadGateway, "PROVIDER_REFUND_FAILED"},
			{"internal error", errors.New("boom"), http.StatusInternalServerError, "INTERNAL_ERROR"},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				svc := refundMockService{
					initiateRefundFn: func(ctx context.Context, input application.CreateRefundInput) (*application.OrderRefundResponse, error) {
						return nil, tc.err
					},
				}
				h := paymenthttp.NewPaymentHandler(svc)
				req := httptest.NewRequest(http.MethodPost, "/admin/orders/"+oid.String()+"/refunds", strings.NewReader(`{"reason":"fraud"}`))
				req.SetPathValue("id", oid.String())
				req = req.WithContext(context.WithValue(req.Context(), middleware.ContextKeyUserId, adminID))
				w := httptest.NewRecorder()
				h.CreateRefund(w, req)

				assert.Equal(t, tc.wantStatus, w.Code)
				var resp map[string]string
				_ = json.NewDecoder(w.Body).Decode(&resp)
				assert.Equal(t, tc.wantCode, resp["code"])
			})
		}
	})

	t.Run("CreateRefund success returns 201 with response", func(t *testing.T) {
		rfID := uuid.New()
		svc := refundMockService{
			initiateRefundFn: func(ctx context.Context, input application.CreateRefundInput) (*application.OrderRefundResponse, error) {
				assert.Equal(t, oid, input.OrderID)
				assert.Equal(t, adminID, input.AdminID)
				assert.Equal(t, "key-123", input.IdempotencyKey)
				assert.Equal(t, "duplicate", input.Reason)
				provID := "rfnd_1"
				return &application.OrderRefundResponse{
					ID:               rfID,
					OrderID:          oid,
					Amount:           1500,
					Currency:         "USD",
					Status:           domain.RefundStatusSucceeded,
					Reason:           "duplicate",
					ProviderRefundID: &provID,
					CreatedAt:        time.Now(),
				}, nil
			},
		}
		h := paymenthttp.NewPaymentHandler(svc)
		req := httptest.NewRequest(http.MethodPost, "/admin/orders/"+oid.String()+"/refunds", strings.NewReader(`{"reason":"duplicate","notes":"customer double ordered"}`))
		req.Header.Set("Idempotency-Key", "key-123")
		req.SetPathValue("id", oid.String())
		req = req.WithContext(context.WithValue(req.Context(), middleware.ContextKeyUserId, adminID))
		w := httptest.NewRecorder()
		h.CreateRefund(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		var resp application.OrderRefundResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.Equal(t, rfID, resp.ID)
		assert.Equal(t, domain.RefundStatusSucceeded, resp.Status)
	})

	t.Run("GetRefunds branches", func(t *testing.T) {
		h := paymenthttp.NewPaymentHandler(refundMockService{})

		// invalid id
		req := httptest.NewRequest(http.MethodGet, "/admin/orders/bad-id/refunds", nil)
		req.SetPathValue("id", "bad-id")
		w := httptest.NewRecorder()
		h.GetRefunds(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)

		// service error
		errSvc := refundMockService{
			getOrderRefundsFn: func(ctx context.Context, orderID uuid.UUID) ([]domain.OrderRefund, error) {
				return nil, errors.New("db error")
			},
		}
		h = paymenthttp.NewPaymentHandler(errSvc)
		req = httptest.NewRequest(http.MethodGet, "/admin/orders/"+oid.String()+"/refunds", nil)
		req.SetPathValue("id", oid.String())
		w = httptest.NewRecorder()
		h.GetRefunds(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)

		// success
		rfID := uuid.New()
		okSvc := refundMockService{
			getOrderRefundsFn: func(ctx context.Context, orderID uuid.UUID) ([]domain.OrderRefund, error) {
				return []domain.OrderRefund{{ID: rfID, OrderID: oid}}, nil
			},
		}
		h = paymenthttp.NewPaymentHandler(okSvc)
		req = httptest.NewRequest(http.MethodGet, "/admin/orders/"+oid.String()+"/refunds", nil)
		req.SetPathValue("id", oid.String())
		w = httptest.NewRecorder()
		h.GetRefunds(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("ListWebhookEvents branches", func(t *testing.T) {
		eID := uuid.New()
		filterOID := uuid.New()

		svc := refundMockService{
			listWebhookEventsFn: func(ctx context.Context, filter domain.WebhookEventFilter, limit, offset int) ([]domain.PaymentWebhookEvent, int, error) {
				assert.Equal(t, "dodo", filter.Provider)
				assert.Equal(t, &filterOID, filter.OrderID)
				assert.Equal(t, 10, limit)
				return []domain.PaymentWebhookEvent{{ID: eID, Provider: "dodo"}}, 1, nil
			},
		}
		h := paymenthttp.NewPaymentHandler(svc)
		req := httptest.NewRequest(http.MethodGet, "/admin/payment-webhooks?provider=dodo&order_id="+filterOID.String()+"&page=1&limit=10", nil)
		w := httptest.NewRecorder()
		h.ListWebhookEvents(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// service error
		errSvc := refundMockService{
			listWebhookEventsFn: func(ctx context.Context, filter domain.WebhookEventFilter, limit, offset int) ([]domain.PaymentWebhookEvent, int, error) {
				return nil, 0, errors.New("db error")
			},
		}
		h = paymenthttp.NewPaymentHandler(errSvc)
		req = httptest.NewRequest(http.MethodGet, "/admin/payment-webhooks", nil)
		w = httptest.NewRecorder()
		h.ListWebhookEvents(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("RequeueWebhookEvent branches", func(t *testing.T) {
		h := paymenthttp.NewPaymentHandler(refundMockService{})

		// invalid id
		req := httptest.NewRequest(http.MethodPost, "/admin/payment-webhooks/bad-id/requeue", nil)
		req.SetPathValue("id", "bad-id")
		w := httptest.NewRecorder()
		h.RequeueWebhookEvent(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)

		// service error
		eID := uuid.New()
		errSvc := refundMockService{
			requeueWebhookFn: func(ctx context.Context, eventID uuid.UUID) error {
				return errors.New("requeue fail")
			},
		}
		h = paymenthttp.NewPaymentHandler(errSvc)
		req = httptest.NewRequest(http.MethodPost, "/admin/payment-webhooks/"+eID.String()+"/requeue", nil)
		req.SetPathValue("id", eID.String())
		w = httptest.NewRecorder()
		h.RequeueWebhookEvent(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)

		// success
		okSvc := refundMockService{
			requeueWebhookFn: func(ctx context.Context, eventID uuid.UUID) error {
				return nil
			},
		}
		h = paymenthttp.NewPaymentHandler(okSvc)
		req = httptest.NewRequest(http.MethodPost, "/admin/payment-webhooks/"+eID.String()+"/requeue", nil)
		req.SetPathValue("id", eID.String())
		w = httptest.NewRecorder()
		h.RequeueWebhookEvent(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestPaymentHandler_OrderAndWebhookIdempotencyBranches(t *testing.T) {
	uid := uuid.New()
	specID := uuid.New()
	licID := uuid.New()

	t.Run("CreateOrder IdempotencyConflict and InFlight and MoneyErrors", func(t *testing.T) {
		cases := []struct {
			name       string
			err        error
			wantStatus int
			wantCode   string
		}{
			{"idempotency conflict", domain.ErrIdempotencyConflict, http.StatusConflict, "IDEMPOTENCY_CONFLICT"},
			{"in flight", domain.ErrOrderReservedInFlight, http.StatusAccepted, "CHECKOUT_IN_FLIGHT"},
			{"invalid currency", money.ErrInvalidCurrency, http.StatusBadRequest, "INVALID_CURRENCY"},
			{"price unavailable", money.ErrPriceUnavailable, http.StatusConflict, "PRICE_UNAVAILABLE"},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				svc := refundMockService{
					createOrderWithOfferFn: func(ctx context.Context, input application.CreateOrderInput) (*domain.Order, error) {
						return nil, tc.err
					},
				}
				h := paymenthttp.NewPaymentHandler(svc)
				body := `{"spec_id":"` + specID.String() + `","license_option_id":"` + licID.String() + `"}`
				req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(body))
				req.Header.Set("Idempotency-Key", "key-xyz")
				req = req.WithContext(context.WithValue(req.Context(), middleware.ContextKeyUserId, uid))
				w := httptest.NewRecorder()
				h.CreateOrder(w, req)

				assert.Equal(t, tc.wantStatus, w.Code)
				var resp map[string]any
				_ = json.NewDecoder(w.Body).Decode(&resp)
				assert.Equal(t, tc.wantCode, resp["code"])
			})
		}
	})

	t.Run("VerifyPayment completes by webhook returns 409", func(t *testing.T) {
		oid := uuid.New()
		svc := refundMockService{
			verifyPaymentFn: func(ctx context.Context, orderID, userID uuid.UUID, paymentID, signature string) (*domain.License, error) {
				return nil, domain.ErrProviderCompletesByWebhook
			},
		}
		h := paymenthttp.NewPaymentHandler(svc)
		body := `{"order_id":"` + oid.String() + `","razorpay_payment_id":"pay_123","razorpay_signature":"sig"}`
		req := httptest.NewRequest(http.MethodPost, "/payments/verify", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), middleware.ContextKeyUserId, uid))
		w := httptest.NewRecorder()
		h.VerifyPayment(w, req)

		assert.Equal(t, http.StatusConflict, w.Code)
	})

	t.Run("ProviderWebhook anomaly and rejection returns 200 ignored", func(t *testing.T) {
		anomalies := []error{
			domain.ErrOrderNotFound,
			domain.ErrProviderMismatch,
			domain.ErrAmountMismatch,
			domain.ErrDistinctCaptureAnomaly,
			domain.ErrOrderNotPayable,
		}

		for _, anomaly := range anomalies {
			svc := refundMockService{
				handleWebhookFn: func(ctx context.Context, provider string, payload []byte, headers map[string]string) error {
					return anomaly
				},
			}
			h := paymenthttp.NewPaymentHandler(svc)
			req := httptest.NewRequest(http.MethodPost, "/webhooks/dodo", strings.NewReader(`{}`))
			req.SetPathValue("provider", "dodo")
			w := httptest.NewRecorder()
			h.ProviderWebhook(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			var resp map[string]any
			_ = json.NewDecoder(w.Body).Decode(&resp)
			assert.Equal(t, "ignored", resp["status"])
		}
	})

	t.Run("ProviderWebhook unknown provider returns 404, generic returns 500", func(t *testing.T) {
		svc := refundMockService{
			handleWebhookFn: func(ctx context.Context, provider string, payload []byte, headers map[string]string) error {
				return errors.New("provider stripe is unknown or not supported")
			},
		}
		h := paymenthttp.NewPaymentHandler(svc)
		req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(`{}`))
		req.SetPathValue("provider", "stripe")
		w := httptest.NewRecorder()
		h.ProviderWebhook(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)

		svc500 := refundMockService{
			handleWebhookFn: func(ctx context.Context, provider string, payload []byte, headers map[string]string) error {
				return errors.New("db connection failure")
			},
		}
		h500 := paymenthttp.NewPaymentHandler(svc500)
		req500 := httptest.NewRequest(http.MethodPost, "/webhooks/dodo", strings.NewReader(`{}`))
		req500.SetPathValue("provider", "dodo")
		w500 := httptest.NewRecorder()
		h500.ProviderWebhook(w500, req500)
		assert.Equal(t, http.StatusInternalServerError, w500.Code)
	})
}

func (m refundMockService) ReconcileOrderCheckout(context.Context, uuid.UUID, string) error {
	return nil
}
