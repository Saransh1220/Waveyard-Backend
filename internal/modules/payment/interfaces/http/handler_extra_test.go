package http_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/application"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
	paymenthttp "github.com/saransh1220/blueprint-audio/internal/modules/payment/interfaces/http"
	sharedmoney "github.com/saransh1220/blueprint-audio/internal/shared/money"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPaymentHandler_DodoWebhook(t *testing.T) {
	t.Run("success forwards payload and headers", func(t *testing.T) {
		var gotPayload []byte
		var gotHeaders map[string]string
		h := paymenthttp.NewPaymentHandler(mockPaymentService{
			dodoWebhookFn: func(_ context.Context, p []byte, hd map[string]string) error {
				gotPayload, gotHeaders = p, hd
				return nil
			},
		})
		r := httptest.NewRequest(http.MethodPost, "/webhooks/dodo", strings.NewReader(`{"a":1}`))
		r.Header.Set("webhook-id", "id1")
		r.Header.Set("webhook-timestamp", "123")
		r.Header.Set("webhook-signature", "sig")
		w := httptest.NewRecorder()
		h.DodoWebhook(w, r)
		require.Equal(t, http.StatusOK, w.Code)
		assert.JSONEq(t, `{"received":true}`, w.Body.String())
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
		assert.Equal(t, `{"a":1}`, string(gotPayload))
		assert.Equal(t, map[string]string{"webhook-id": "id1", "webhook-timestamp": "123", "webhook-signature": "sig"}, gotHeaders)
	})
	t.Run("service error is 400 on malformed", func(t *testing.T) {
		h := paymenthttp.NewPaymentHandler(mockPaymentService{
			dodoWebhookFn: func(context.Context, []byte, map[string]string) error { return domain.ErrMalformedPayload },
		})
		w := httptest.NewRecorder()
		h.DodoWebhook(w, httptest.NewRequest(http.MethodPost, "/webhooks/dodo", strings.NewReader("x")))
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
	t.Run("service error is 401 on bad signature", func(t *testing.T) {
		h := paymenthttp.NewPaymentHandler(mockPaymentService{
			dodoWebhookFn: func(context.Context, []byte, map[string]string) error { return domain.ErrInvalidSignature },
		})
		w := httptest.NewRecorder()
		h.DodoWebhook(w, httptest.NewRequest(http.MethodPost, "/webhooks/dodo", strings.NewReader("x")))
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
	t.Run("unreadable body is 400", func(t *testing.T) {
		h := paymenthttp.NewPaymentHandler(mockPaymentService{})
		r := httptest.NewRequest(http.MethodPost, "/webhooks/dodo", errReader{})
		w := httptest.NewRecorder()
		h.DodoWebhook(w, r)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid webhook body")
	})
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read fail") }

func plainReq(method, path, body string) *http.Request {
	return httptest.NewRequest(method, path, strings.NewReader(body))
}

func TestPaymentHandler_Unauthorized(t *testing.T) {
	h := paymenthttp.NewPaymentHandler(mockPaymentService{})
	lid := uuid.NewString()
	cases := map[string]func(w http.ResponseWriter){
		"create":    func(w http.ResponseWriter) { h.CreateOrder(w, plainReq("POST", "/o", "{}")) },
		"verify":    func(w http.ResponseWriter) { h.VerifyPayment(w, plainReq("POST", "/v", "{}")) },
		"getOrder":  func(w http.ResponseWriter) { h.GetOrder(w, plainReq("GET", "/o/1", "")) },
		"status":    func(w http.ResponseWriter) { h.GetOrderStatus(w, plainReq("GET", "/o/1/status", "")) },
		"orders":    func(w http.ResponseWriter) { h.GetUserOrders(w, plainReq("GET", "/o", "")) },
		"licenses":  func(w http.ResponseWriter) { h.GetUserLicenses(w, plainReq("GET", "/l", "")) },
		"producers": func(w http.ResponseWriter) { h.GetProducerOrders(w, plainReq("GET", "/p", "")) },
		"downloads": func(w http.ResponseWriter) {
			r := plainReq("GET", "/d", "")
			r.SetPathValue("id", lid)
			h.GetLicenseDownloads(w, r)
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			call(w)
			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Contains(t, w.Body.String(), "unauthorized")
		})
	}
}

func TestPaymentHandler_BadInput(t *testing.T) {
	h := paymenthttp.NewPaymentHandler(mockPaymentService{})
	good := uuid.NewString()

	tests := []struct {
		name string
		call func(w http.ResponseWriter)
		body string
	}{
		{"create bad json", func(w http.ResponseWriter) { h.CreateOrder(w, authedReq("POST", "/o", "{nope")) }, "invalid request body"},
		{"create bad spec", func(w http.ResponseWriter) {
			h.CreateOrder(w, authedReq("POST", "/o", `{"spec_id":"x","license_option_id":"`+good+`"}`))
		}, "invalid spec_id"},
		{"create bad license", func(w http.ResponseWriter) {
			h.CreateOrder(w, authedReq("POST", "/o", `{"spec_id":"`+good+`","license_option_id":"x"}`))
		}, "invalid license_option_id"},
		{"verify bad json", func(w http.ResponseWriter) { h.VerifyPayment(w, authedReq("POST", "/v", "{nope")) }, "invalid request body"},
		{"verify bad order", func(w http.ResponseWriter) {
			h.VerifyPayment(w, authedReq("POST", "/v", `{"order_id":"x"}`))
		}, "invalid order_id"},
		{"getOrder bad id", func(w http.ResponseWriter) {
			r := authedReq("GET", "/o/x", "")
			r.SetPathValue("id", "x")
			h.GetOrder(w, r)
		}, "invalid order_id"},
		{"downloads bad id", func(w http.ResponseWriter) {
			r := authedReq("GET", "/d/x", "")
			r.SetPathValue("id", "x")
			h.GetLicenseDownloads(w, r)
		}, "invalid license ID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tt.call(w)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), tt.body)
		})
	}
}

func TestPaymentHandler_ServiceErrorStatuses(t *testing.T) {
	boom := errors.New("boom")
	h := paymenthttp.NewPaymentHandler(mockPaymentService{
		createOrderFn:   func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*domain.Order, error) { return nil, boom },
		verifyFn:        func(context.Context, uuid.UUID, uuid.UUID, string, string) (*domain.License, error) { return nil, boom },
		getOrderFn:      func(context.Context, uuid.UUID, uuid.UUID) (*domain.Order, error) { return nil, boom },
		getUserOrdersFn: func(context.Context, uuid.UUID, int) ([]domain.Order, error) { return nil, boom },
		getUserLicensesFn: func(context.Context, uuid.UUID, int, string, string) ([]domain.License, int, error) {
			return nil, 0, boom
		},
		getProducerOrdersFn: func(context.Context, uuid.UUID, int, int) (*application.ProducerOrderResponse, error) {
			return nil, boom
		},
	})
	oid := uuid.NewString()

	w := httptest.NewRecorder()
	h.CreateOrder(w, authedReq("POST", "/o", `{"spec_id":"`+oid+`","license_option_id":"`+oid+`"}`))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "failed to create order\n", w.Body.String())

	w = httptest.NewRecorder()
	h.VerifyPayment(w, authedReq("POST", "/v", `{"order_id":"`+oid+`"}`))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "payment verification failed\n", w.Body.String())

	w = httptest.NewRecorder()
	r := authedReq("GET", "/o/"+oid, "")
	r.SetPathValue("id", oid)
	h.GetOrder(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)

	w = httptest.NewRecorder()
	h.GetUserOrders(w, authedReq("GET", "/o", ""))
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	w = httptest.NewRecorder()
	h.GetUserLicenses(w, authedReq("GET", "/l", ""))
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	w = httptest.NewRecorder()
	h.GetProducerOrders(w, authedReq("GET", "/p", ""))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "failed to fetch producer orders")
}

func TestPaymentHandler_GetLicenseDownloads_ErrorMapping(t *testing.T) {
	tests := []struct {
		err  string
		want int
	}{
		{"license not found", http.StatusNotFound},
		{"spec not found", http.StatusNotFound},
		{"unauthorized: you do not own this license", http.StatusForbidden},
		{"license is not active", http.StatusForbidden},
		{"license has been revoked", http.StatusForbidden},
		{"something else", http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.err, func(t *testing.T) {
			h := paymenthttp.NewPaymentHandler(mockPaymentService{
				getDownloadsFn: func(context.Context, uuid.UUID, uuid.UUID) (*application.LicenseDownloadsResponse, error) {
					return nil, errors.New(tt.err)
				},
			})
			id := uuid.NewString()
			r := authedReq("GET", "/d/"+id, "")
			r.SetPathValue("id", id)
			w := httptest.NewRecorder()
			h.GetLicenseDownloads(w, r)
			assert.Equal(t, tt.want, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
			assert.Contains(t, w.Body.String(), tt.err)
		})
	}
}

func TestPaymentHandler_PaginationParsing(t *testing.T) {
	var gotOrdersPage, gotLicPage, gotProdPage, gotProdLimit int
	var gotQ, gotType string
	h := paymenthttp.NewPaymentHandler(mockPaymentService{
		getUserOrdersFn: func(_ context.Context, _ uuid.UUID, p int) ([]domain.Order, error) {
			gotOrdersPage = p
			return nil, nil
		},
		getUserLicensesFn: func(_ context.Context, _ uuid.UUID, p int, q, ty string) ([]domain.License, int, error) {
			gotLicPage, gotQ, gotType = p, q, ty
			return nil, 0, nil
		},
		getProducerOrdersFn: func(_ context.Context, _ uuid.UUID, p, l int) (*application.ProducerOrderResponse, error) {
			gotProdPage, gotProdLimit = p, l
			return &application.ProducerOrderResponse{}, nil
		},
	})

	// invalid / non-positive values fall back to defaults
	for _, q := range []string{"?page=abc", "?page=0", "?page=-2", ""} {
		h.GetUserOrders(httptest.NewRecorder(), authedReq("GET", "/o"+q, ""))
		assert.Equal(t, 1, gotOrdersPage, q)
	}
	h.GetUserOrders(httptest.NewRecorder(), authedReq("GET", "/o?page=4", ""))
	assert.Equal(t, 4, gotOrdersPage)

	h.GetUserLicenses(httptest.NewRecorder(), authedReq("GET", "/l?page=zzz&q=beat&type=Basic", ""))
	assert.Equal(t, 1, gotLicPage)
	assert.Equal(t, "beat", gotQ)
	assert.Equal(t, "Basic", gotType)
	w := httptest.NewRecorder()
	h.GetUserLicenses(w, authedReq("GET", "/l?page=3", ""))
	assert.Equal(t, 3, gotLicPage)
	assert.Contains(t, w.Body.String(), `"page":3`)

	h.GetProducerOrders(httptest.NewRecorder(), authedReq("GET", "/p", ""))
	assert.Equal(t, 1, gotProdPage)
	assert.Equal(t, 10, gotProdLimit)
	h.GetProducerOrders(httptest.NewRecorder(), authedReq("GET", "/p?page=-1&limit=0", ""))
	assert.Equal(t, 1, gotProdPage)
	assert.Equal(t, 10, gotProdLimit)
	h.GetProducerOrders(httptest.NewRecorder(), authedReq("GET", "/p?page=2&limit=25", ""))
	assert.Equal(t, 2, gotProdPage)
	assert.Equal(t, 25, gotProdLimit)
}

func TestPaymentHandler_CreateOrder_PricingResponses(t *testing.T) {
	specID := uuid.New()
	loID := uuid.New()
	priceID := uuid.New()

	t.Run("returns 409 with PRICE_CHANGED and current offer when price mismatches", func(t *testing.T) {
		h := paymenthttp.NewPaymentHandler(mockPaymentService{
			createOrderFn: func(ctx context.Context, u, s, l uuid.UUID) (*domain.Order, error) {
				return nil, &application.PriceMismatchError{
					Message: "price changed",
					CurrentOffer: sharedmoney.SelectedPriceOffer{
						PriceID:       &priceID,
						Revision:      3,
						AmountMinor:   2000,
						AmountMajor:   20.00,
						Currency:      "USD",
						Selection:     sharedmoney.SelectionExact,
						IsPurchasable: true,
					},
				}
			},
		})
		body := `{"spec_id":"` + specID.String() + `","license_option_id":"` + loID.String() + `","currency":"USD","expected_amount_minor":1500}`
		w := httptest.NewRecorder()
		h.CreateOrder(w, authedReq("POST", "/orders", body))
		require.Equal(t, http.StatusConflict, w.Code)
		assert.Contains(t, w.Body.String(), `"PRICE_CHANGED"`)
		assert.Contains(t, w.Body.String(), `"amount_minor":2000`)
	})

	t.Run("returns 400 for unsupported currency", func(t *testing.T) {
		h := paymenthttp.NewPaymentHandler(mockPaymentService{
			createOrderFn: func(ctx context.Context, u, s, l uuid.UUID) (*domain.Order, error) {
				return nil, sharedmoney.ErrInvalidCurrency
			},
		})
		body := `{"spec_id":"` + specID.String() + `","license_option_id":"` + loID.String() + `","currency":"EUR"}`
		w := httptest.NewRecorder()
		h.CreateOrder(w, authedReq("POST", "/orders", body))
		require.Equal(t, http.StatusBadRequest, w.Code)
	})
}
