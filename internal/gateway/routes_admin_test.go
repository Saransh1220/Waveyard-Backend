package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/saransh1220/blueprint-audio/internal/gateway/middleware"
	"github.com/saransh1220/blueprint-audio/internal/gateway/openapi"
	admin_http "github.com/saransh1220/blueprint-audio/internal/modules/admin/interfaces/http"
	analytics_http "github.com/saransh1220/blueprint-audio/internal/modules/analytics/interfaces/http"
	auth_http "github.com/saransh1220/blueprint-audio/internal/modules/auth/interfaces/http"
	catalog_http "github.com/saransh1220/blueprint-audio/internal/modules/catalog/interfaces/http"
	fx_http "github.com/saransh1220/blueprint-audio/internal/modules/fx/interfaces/http"
	legal_http "github.com/saransh1220/blueprint-audio/internal/modules/legal/interfaces/http"
	notification_http "github.com/saransh1220/blueprint-audio/internal/modules/notification/interfaces/http"
	payment_http "github.com/saransh1220/blueprint-audio/internal/modules/payment/interfaces/http"
	user_http "github.com/saransh1220/blueprint-audio/internal/modules/user/interfaces/http"
)

func TestSetupRoutesRegistersAdminRoutes(t *testing.T) {
	mux := SetupRoutes(RouterConfig{
		AuthHandler:         &auth_http.AuthHandler{},
		AuthMiddleware:      middleware.NewAuthMiddleware("test-secret"),
		SpecHandler:         &catalog_http.SpecHandler{},
		UserHandler:         &user_http.UserHandler{},
		PaymentHandler:      &payment_http.PaymentHandler{},
		AnalyticsHandler:    &analytics_http.AnalyticsHandler{},
		NotificationHandler: &notification_http.NotificationHandler{},
		AdminHandler:        admin_http.NewAdminHandler(nil, nil),
	})

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/admin/users"},
		{http.MethodGet, "/admin/specs"},
		{http.MethodGet, "/admin/orders"},
		{http.MethodPost, "/admin/orders/11111111-1111-1111-1111-111111111111/refunds"},
		{http.MethodGet, "/admin/orders/11111111-1111-1111-1111-111111111111/refunds"},
		{http.MethodGet, "/admin/payment-webhooks"},
		{http.MethodPost, "/admin/payment-webhooks/11111111-1111-1111-1111-111111111111/requeue"},
		{http.MethodGet, "/admin/licenses"},
		{http.MethodGet, "/admin/analytics/overview"},
		{http.MethodGet, "/admin/audit-log"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, req)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("expected %s %s to be registered and protected, got %d", tc.method, tc.path, response.Code)
		}
	}
}

func TestSetupRoutesRegistersFXRoutes(t *testing.T) {
	mux := SetupRoutes(RouterConfig{
		AuthHandler:         &auth_http.AuthHandler{},
		AuthMiddleware:      middleware.NewAuthMiddleware("test-secret"),
		SpecHandler:         &catalog_http.SpecHandler{},
		UserHandler:         &user_http.UserHandler{},
		PaymentHandler:      &payment_http.PaymentHandler{},
		AnalyticsHandler:    &analytics_http.AnalyticsHandler{},
		NotificationHandler: &notification_http.NotificationHandler{},
		FXHandler:           fx_http.NewFXHandler(nil, nil),
	})

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/admin/fx-rates"},
		{http.MethodPost, "/admin/fx-rates/refresh"},
		{http.MethodGet, "/fx/rates/latest"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, req)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("expected %s %s to be registered and protected, got %d", tc.method, tc.path, response.Code)
		}
	}
}

func TestSetupRoutesComprehensive(t *testing.T) {
	authMw := middleware.NewAuthMiddleware("test-secret")
	buyerMw := middleware.NewBuyerCurrencyMiddleware(nil, false)

	mux := SetupRoutes(RouterConfig{
		AuthHandler:             &auth_http.AuthHandler{},
		AuthMiddleware:          authMw,
		BuyerCurrencyMiddleware: buyerMw,
		SpecHandler:             &catalog_http.SpecHandler{},
		SpecUploadHandler:       catalog_http.NewSpecUploadHandler(nil),
		UserHandler:             &user_http.UserHandler{},
		PaymentHandler:          &payment_http.PaymentHandler{},
		AnalyticsHandler:        &analytics_http.AnalyticsHandler{},
		NotificationHandler:     &notification_http.NotificationHandler{},
		AdminHandler:            admin_http.NewAdminHandler(nil, nil),
		LegalHandler:            legal_http.NewLegalHandler(nil),
		FXHandler:               fx_http.NewFXHandler(nil, nil),
		FavoritesServer:         openapi.NewFavoritesServer(nil),
		DisableAPIDocs:          false,
		CurrencyGatewayNames:    map[string]string{"INR": "razorpay", "USD": "dodo"},
	})

	// Health check
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected /health to return 200, got %d", rec.Code)
	}

	// Config currencies
	req = httptest.NewRequest(http.MethodGet, "/config/currencies", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected /config/currencies to return 200, got %d", rec.Code)
	}

	// Verify protected routes
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/spec-uploads"},
		{http.MethodPut, "/spec-uploads/11111111-1111-1111-1111-111111111111/metadata"},
		{http.MethodPost, "/spec-uploads/11111111-1111-1111-1111-111111111111/files"},
		{http.MethodPost, "/spec-uploads/11111111-1111-1111-1111-111111111111/complete"},
		{http.MethodGet, "/spec-uploads/11111111-1111-1111-1111-111111111111"},
		{http.MethodGet, "/api/v1/legal/requirements"},
		{http.MethodPost, "/api/v1/legal/acceptances"},
		{http.MethodPost, "/api/v1/admin/legal/documents/terms/drafts"},
		{http.MethodPost, "/api/v1/admin/legal/documents/terms/publish"},
		{http.MethodGet, "/api/v1/admin/legal/audits"},
		{http.MethodGet, "/me/favorites"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, req)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("expected %s %s to be protected, got %d", tc.method, tc.path, response.Code)
		}
	}
}
