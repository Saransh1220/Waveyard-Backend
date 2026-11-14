package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/saransh1220/blueprint-audio/internal/shared/utils"
	"github.com/stretchr/testify/assert"
)

func TestCSRFGuardMiddleware_SafeMethods(t *testing.T) {
	allowed := "http://localhost:4200,https://app.waveyard.studio"
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})
	handler := CSRFGuardMiddleware(next, allowed)

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		nextCalled = false
		req := httptest.NewRequest(method, "/api/resource", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.True(t, nextCalled, "safe method %s should pass", method)
		assert.Equal(t, http.StatusOK, rec.Code)
	}
}

func TestCSRFGuardMiddleware_Webhooks(t *testing.T) {
	allowed := "http://localhost:4200"
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})
	handler := CSRFGuardMiddleware(next, allowed)

	// Webhook without Origin or credentials succeeds
	req := httptest.NewRequest(http.MethodPost, "/webhooks/dodo", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.True(t, nextCalled)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Webhook with browser auth cookie is rejected
	nextCalled = false
	req = httptest.NewRequest(http.MethodPost, "/webhooks/dodo", nil)
	req.AddCookie(&http.Cookie{Name: utils.AccessCookieNameSecure, Value: "token"})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.False(t, nextCalled)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "CSRF_REJECTED")
}

func TestCSRFGuardMiddleware_MarkedRequests(t *testing.T) {
	allowed := "http://localhost:4200,https://qa.waveyard.studio"
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := CSRFGuardMiddleware(next, allowed)

	// Missing Origin
	req := httptest.NewRequest(http.MethodPost, "/orders", nil)
	req.Header.Set(utils.HeaderSessionMarker, utils.SessionMarkerCookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "missing or invalid origin header")

	// Null Origin
	req = httptest.NewRequest(http.MethodPost, "/orders", nil)
	req.Header.Set(utils.HeaderSessionMarker, utils.SessionMarkerCookie)
	req.Header.Set("Origin", "null")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// Disallowed Origin
	req = httptest.NewRequest(http.MethodPost, "/orders", nil)
	req.Header.Set(utils.HeaderSessionMarker, utils.SessionMarkerCookie)
	req.Header.Set("Origin", "https://malicious.example.com")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "origin not allowed")

	// Multiple Origin headers
	req = httptest.NewRequest(http.MethodPost, "/orders", nil)
	req.Header.Set(utils.HeaderSessionMarker, utils.SessionMarkerCookie)
	req.Header.Add("Origin", "http://localhost:4200")
	req.Header.Add("Origin", "https://qa.waveyard.studio")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// Allowed Origin
	req = httptest.NewRequest(http.MethodPost, "/orders", nil)
	req.Header.Set(utils.HeaderSessionMarker, utils.SessionMarkerCookie)
	req.Header.Set("Origin", "http://localhost:4200")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCSRFGuardMiddleware_UnmarkedWithBrowserCredentials(t *testing.T) {
	allowed := "http://localhost:4200"
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := CSRFGuardMiddleware(next, allowed)

	// Unmarked request carrying browser access cookie
	req := httptest.NewRequest(http.MethodPost, "/orders", nil)
	req.AddCookie(&http.Cookie{Name: utils.AccessCookieNameSecure, Value: "jwt-cookie"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "unmarked mutation with browser credentials")
}
