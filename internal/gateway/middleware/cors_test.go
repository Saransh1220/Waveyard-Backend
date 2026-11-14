package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCORSMiddleware_PreflightRequest(t *testing.T) {
	allowedOrigins := "http://localhost:4200,https://example.com"

	// Mock handler
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := CORSMiddleware(nextHandler, allowedOrigins)

	// Preflight request
	req := httptest.NewRequest("OPTIONS", "/orders", nil)
	req.Header.Set("Origin", "http://localhost:4200")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "authorization, content-type, idempotency-key, x-skip-loading, x-waveyard-session, x-waveyard-expected-viewer")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Verify CORS headers
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "http://localhost:4200", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, rec.Header().Get("Access-Control-Allow-Methods"), "POST")
	for _, name := range []string{"Idempotency-Key", "X-Skip-Loading", "X-Waveyard-Session", "X-Waveyard-Expected-Viewer"} {
		assert.Contains(t, rec.Header().Get("Access-Control-Allow-Headers"), name)
	}
}

func TestCORSMiddleware_ActualRequest(t *testing.T) {
	allowedOrigins := "http://localhost:4200"

	nextCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	handler := CORSMiddleware(nextHandler, allowedOrigins)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://localhost:4200")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, nextCalled)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "http://localhost:4200", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "success", rec.Body.String())
}

func TestCORSMiddleware_MultipleOrigins(t *testing.T) {
	allowedOrigins := "http://localhost:4200,https://example.com,https://app.example.com"

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := CORSMiddleware(nextHandler, allowedOrigins)

	testCases := []struct {
		name   string
		origin string
		expect string
	}{
		{"first_origin", "http://localhost:4200", "http://localhost:4200"},
		{"second_origin", "https://example.com", "https://example.com"},
		{"third_origin", "https://app.example.com", "https://app.example.com"},
		{"unauthorized_origin", "https://evil.com", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("Origin", tc.origin)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if tc.expect != "" {
				assert.Equal(t, tc.expect, rec.Header().Get("Access-Control-Allow-Origin"))
			} else {
				assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
			}
		})
	}
}

func TestCORSMiddleware_Credentials(t *testing.T) {
	allowedOrigins := "http://localhost:4200"

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := CORSMiddleware(nextHandler, allowedOrigins)

	req := httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("Origin", "http://localhost:4200")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
}
