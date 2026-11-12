package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/saransh1220/blueprint-audio/internal/gateway/middleware"
	analytics_http "github.com/saransh1220/blueprint-audio/internal/modules/analytics/interfaces/http"
	auth_http "github.com/saransh1220/blueprint-audio/internal/modules/auth/interfaces/http"
	catalog_http "github.com/saransh1220/blueprint-audio/internal/modules/catalog/interfaces/http"
	notification_http "github.com/saransh1220/blueprint-audio/internal/modules/notification/interfaces/http"
	payment_http "github.com/saransh1220/blueprint-audio/internal/modules/payment/interfaces/http"
	user_http "github.com/saransh1220/blueprint-audio/internal/modules/user/interfaces/http"
)

func TestSetupRoutes(t *testing.T) {
	// Create mock handlers
	authHandler := &auth_http.AuthHandler{}
	authMiddleware := middleware.NewAuthMiddleware("test-secret")
	specHandler := &catalog_http.SpecHandler{}
	userHandler := &user_http.UserHandler{}
	paymentHandler := &payment_http.PaymentHandler{}
	analyticsHandler := &analytics_http.AnalyticsHandler{}
	notificationHandler := &notification_http.NotificationHandler{}

	config := RouterConfig{
		AuthHandler:         authHandler,
		AuthMiddleware:      authMiddleware,
		SpecHandler:         specHandler,
		UserHandler:         userHandler,
		PaymentHandler:      paymentHandler,
		AnalyticsHandler:    analyticsHandler,
		NotificationHandler: notificationHandler,
	}

	mux := SetupRoutes(config)

	// Test that mux is created
	if mux == nil {
		t.Fatal("Expected mux to be created, got nil")
	}
}

func TestSetupRoutes_HealthCheck(t *testing.T) {
	config := RouterConfig{
		AuthHandler:         &auth_http.AuthHandler{},
		AuthMiddleware:      middleware.NewAuthMiddleware("test-secret"),
		SpecHandler:         &catalog_http.SpecHandler{},
		UserHandler:         &user_http.UserHandler{},
		PaymentHandler:      &payment_http.PaymentHandler{},
		AnalyticsHandler:    &analytics_http.AnalyticsHandler{},
		NotificationHandler: &notification_http.NotificationHandler{},
	}

	mux := SetupRoutes(config)

	// Create a test request to /health
	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create a response recorder
	rr := &responseRecorder{}
	mux.ServeHTTP(rr, req)

	// Check status code
	if rr.statusCode != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.statusCode)
	}

	// Check body
	if rr.body != "OK" {
		t.Errorf("Expected body 'OK', got '%s'", rr.body)
	}
}

func TestSetupRoutes_MetricsEndpoint(t *testing.T) {
	config := RouterConfig{
		AuthHandler:         &auth_http.AuthHandler{},
		AuthMiddleware:      middleware.NewAuthMiddleware("test-secret"),
		MetricsToken:        "test-scrape-credential",
		SpecHandler:         &catalog_http.SpecHandler{},
		UserHandler:         &user_http.UserHandler{},
		PaymentHandler:      &payment_http.PaymentHandler{},
		AnalyticsHandler:    &analytics_http.AnalyticsHandler{},
		NotificationHandler: &notification_http.NotificationHandler{},
	}
	mux := SetupRoutes(config)

	for _, tc := range []struct {
		path string
		auth string
		want int
	}{
		{"/metrics", "", http.StatusUnauthorized},
		{"/metrics", "Bearer wrong-token", http.StatusUnauthorized},
		{"/metrics", "Bearer test-scrape-credential", http.StatusOK},
		{"/orders", "Bearer test-scrape-credential", http.StatusUnauthorized},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.Header.Set("Authorization", tc.auth)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Errorf("%s: expected %d, got %d", tc.path, tc.want, w.Code)
		}
	}
}

func TestSetupRoutes_APIDocumentation(t *testing.T) {
	config := RouterConfig{
		AuthHandler:         &auth_http.AuthHandler{},
		AuthMiddleware:      middleware.NewAuthMiddleware("test-secret"),
		SpecHandler:         &catalog_http.SpecHandler{},
		UserHandler:         &user_http.UserHandler{},
		PaymentHandler:      &payment_http.PaymentHandler{},
		AnalyticsHandler:    &analytics_http.AnalyticsHandler{},
		NotificationHandler: &notification_http.NotificationHandler{},
	}

	mux := SetupRoutes(config)
	request := httptest.NewRequest(http.MethodGet, "/docs/", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected API docs to be enabled by default, got %d", response.Code)
	}

	config.DisableAPIDocs = true
	mux = SetupRoutes(config)
	request = httptest.NewRequest(http.MethodGet, "/docs/", nil)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected disabled API docs to return 404, got %d", response.Code)
	}
}

// responseRecorder is a helper to capture HTTP responses
type responseRecorder struct {
	statusCode int
	body       string
}

func (rr *responseRecorder) Header() http.Header {
	return http.Header{}
}

func (rr *responseRecorder) WriteHeader(statusCode int) {
	rr.statusCode = statusCode
}

func (rr *responseRecorder) Write(b []byte) (int, error) {
	rr.body = string(b)
	return len(b), nil
}
