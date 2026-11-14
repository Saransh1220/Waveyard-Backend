package http_test

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/application"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	auth_http "github.com/saransh1220/blueprint-audio/internal/modules/auth/interfaces/http"
	"github.com/saransh1220/blueprint-audio/internal/shared/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func findCookieByName(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestAuthHandler_BrowserLogin_Success(t *testing.T) {
	mockService := new(MockAuthService)
	mockFileService := new(MockFileService)
	h := auth_http.NewAuthHandler(mockService, mockFileService, "client-id", 7*24*time.Hour, true, 15*time.Minute)
	defer mockService.AssertExpectations(t)

	mockService.On("Login", mock.Anything, application.LoginRequest{Email: "user@example.com", Password: "password123"}, true).
		Return(&application.TokenPair{
			AccessToken:  "browser-access-jwt",
			RefreshToken: "browser-refresh-token",
			IsBrowser:    true,
		}, nil).Once()

	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(`{"email":"user@example.com","password":"password123"}`))
	req.Header.Set(utils.HeaderSessionMarker, utils.SessionMarkerCookie)
	w := httptest.NewRecorder()

	h.Login(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, w.Body.String(), "browser login must not return tokens in the response body")
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))

	// Check access cookie
	accessCookie := findCookieByName(w, utils.AccessCookieNameSecure)
	require.NotNil(t, accessCookie)
	assert.Equal(t, "browser-access-jwt", accessCookie.Value)
	assert.True(t, accessCookie.HttpOnly)
	assert.True(t, accessCookie.Secure)
	assert.Equal(t, http.SameSiteLaxMode, accessCookie.SameSite)
	assert.Equal(t, "/", accessCookie.Path)
	assert.WithinDuration(t, time.Now().Add(15*time.Minute), accessCookie.Expires, 2*time.Minute)

	// Check refresh cookie
	refreshCookie := findCookieByName(w, utils.RefreshCookieName)
	require.NotNil(t, refreshCookie)
	assert.Equal(t, "browser-refresh-token", refreshCookie.Value)
	assert.True(t, refreshCookie.HttpOnly)
	assert.True(t, refreshCookie.Secure)
	assert.Equal(t, http.SameSiteLaxMode, refreshCookie.SameSite)
	assert.Equal(t, "/", refreshCookie.Path)
	assert.WithinDuration(t, time.Now().Add(7*24*time.Hour), refreshCookie.Expires, 2*time.Minute)
}

func TestAuthHandler_BrowserGoogleLogin_Success(t *testing.T) {
	mockService := new(MockAuthService)
	mockFileService := new(MockFileService)
	h := auth_http.NewAuthHandler(mockService, mockFileService, "client-id", 7*24*time.Hour, false, 15*time.Minute)
	defer mockService.AssertExpectations(t)

	mockService.On("GoogleLogin", mock.Anything, "client-id", application.GoogleLoginRequest{Token: "gtoken"}, true).
		Return(&application.TokenPair{
			AccessToken:  "google-access-jwt",
			RefreshToken: "google-refresh-token",
			IsBrowser:    true,
		}, nil).Once()

	req := httptest.NewRequest(http.MethodPost, "/auth/google", bytes.NewBufferString(`{"token":"gtoken"}`))
	req.Header.Set(utils.HeaderSessionMarker, utils.SessionMarkerCookie)
	w := httptest.NewRecorder()

	h.GoogleLogin(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, w.Body.String())
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))

	// Since secureCookie is false, uses insecure access cookie name
	accessCookie := findCookieByName(w, utils.AccessCookieNameInsecure)
	require.NotNil(t, accessCookie)
	assert.Equal(t, "google-access-jwt", accessCookie.Value)
	assert.False(t, accessCookie.Secure)
	assert.Equal(t, http.SameSiteLaxMode, accessCookie.SameSite)

	refreshCookie := findCookieByName(w, utils.RefreshCookieName)
	require.NotNil(t, refreshCookie)
	assert.Equal(t, "google-refresh-token", refreshCookie.Value)
	assert.False(t, refreshCookie.Secure)
	assert.Equal(t, http.SameSiteLaxMode, refreshCookie.SameSite)
}

func TestAuthHandler_BrowserRefresh_Success(t *testing.T) {
	mockService := new(MockAuthService)
	mockFileService := new(MockFileService)
	h := auth_http.NewAuthHandler(mockService, mockFileService, "client-id", 7*24*time.Hour, true, 15*time.Minute)
	defer mockService.AssertExpectations(t)

	mockService.On("RefreshSession", mock.Anything, "valid-refresh", true).
		Return(&application.TokenPair{
			AccessToken:  "rotated-access-jwt",
			RefreshToken: "rotated-refresh-token",
			IsBrowser:    true,
		}, nil).Once()

	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	req.Header.Set(utils.HeaderSessionMarker, utils.SessionMarkerCookie)
	req.AddCookie(&http.Cookie{Name: utils.RefreshCookieName, Value: "valid-refresh"})
	w := httptest.NewRecorder()

	h.Refresh(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, w.Body.String())

	acc := findCookieByName(w, utils.AccessCookieNameSecure)
	require.NotNil(t, acc)
	assert.Equal(t, "rotated-access-jwt", acc.Value)

	ref := findCookieByName(w, utils.RefreshCookieName)
	require.NotNil(t, ref)
	assert.Equal(t, "rotated-refresh-token", ref.Value)
}

func TestAuthHandler_BrowserRefresh_GraceWindow(t *testing.T) {
	mockService := new(MockAuthService)
	mockFileService := new(MockFileService)
	h := auth_http.NewAuthHandler(mockService, mockFileService, "client-id", 7*24*time.Hour, true, 15*time.Minute)
	defer mockService.AssertExpectations(t)

	// Grace window returns AccessToken without new RefreshToken
	mockService.On("RefreshSession", mock.Anything, "graced-refresh", true).
		Return(&application.TokenPair{
			AccessToken:  "grace-access-jwt",
			RefreshToken: "",
			IsBrowser:    true,
		}, nil).Once()

	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	req.Header.Set(utils.HeaderSessionMarker, utils.SessionMarkerCookie)
	req.AddCookie(&http.Cookie{Name: utils.RefreshCookieName, Value: "graced-refresh"})
	w := httptest.NewRecorder()

	h.Refresh(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	acc := findCookieByName(w, utils.AccessCookieNameSecure)
	require.NotNil(t, acc)
	assert.Equal(t, "grace-access-jwt", acc.Value)

	ref := findCookieByName(w, utils.RefreshCookieName)
	assert.Nil(t, ref, "grace-window refresh must not touch the refresh cookie")
}

func TestAuthHandler_BrowserRefresh_Suspended(t *testing.T) {
	mockService := new(MockAuthService)
	mockFileService := new(MockFileService)
	h := auth_http.NewAuthHandler(mockService, mockFileService, "client-id", 7*24*time.Hour, true, 15*time.Minute)
	defer mockService.AssertExpectations(t)

	mockService.On("RefreshSession", mock.Anything, "suspended-refresh", true).
		Return(nil, application.ErrAccountSuspended).Once()

	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	req.Header.Set(utils.HeaderSessionMarker, utils.SessionMarkerCookie)
	req.AddCookie(&http.Cookie{Name: utils.RefreshCookieName, Value: "suspended-refresh"})
	w := httptest.NewRecorder()

	h.Refresh(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	// Must clear all auth cookies on suspension
	acc := findCookieByName(w, utils.AccessCookieNameSecure)
	require.NotNil(t, acc)
	assert.Equal(t, -1, acc.MaxAge)

	ref := findCookieByName(w, utils.RefreshCookieName)
	require.NotNil(t, ref)
	assert.Equal(t, -1, ref.MaxAge)
}

func TestAuthHandler_BrowserRefresh_ModeMismatch(t *testing.T) {
	mockService := new(MockAuthService)
	mockFileService := new(MockFileService)
	h := auth_http.NewAuthHandler(mockService, mockFileService, "client-id", 7*24*time.Hour, true, 15*time.Minute)
	defer mockService.AssertExpectations(t)

	mockService.On("RefreshSession", mock.Anything, "token-owned-by-cookie", false).
		Return(nil, domain.ErrSessionModeMismatch).Once()

	// Legacy refresh without X-Waveyard-Session header
	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: utils.RefreshCookieName, Value: "token-owned-by-cookie"})
	w := httptest.NewRecorder()

	h.Refresh(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "invalid session mode")
}

func TestAuthHandler_BrowserLogout_ConfirmedRevocation(t *testing.T) {
	mockService := new(MockAuthService)
	mockFileService := new(MockFileService)
	h := auth_http.NewAuthHandler(mockService, mockFileService, "client-id", 7*24*time.Hour, true, 15*time.Minute)
	defer mockService.AssertExpectations(t)

	mockService.On("Logout", mock.Anything, "active-refresh", (*uuid.UUID)(nil)).Return(nil).Once()

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set(utils.HeaderSessionMarker, utils.SessionMarkerCookie)
	req.AddCookie(&http.Cookie{Name: utils.RefreshCookieName, Value: "active-refresh"})
	w := httptest.NewRecorder()

	h.Logout(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, w.Body.String())

	acc := findCookieByName(w, utils.AccessCookieNameSecure)
	require.NotNil(t, acc)
	assert.Equal(t, -1, acc.MaxAge)

	ref := findCookieByName(w, utils.RefreshCookieName)
	require.NotNil(t, ref)
	assert.Equal(t, -1, ref.MaxAge)
}

func TestAuthHandler_BrowserLogout_RevocationFailure(t *testing.T) {
	mockService := new(MockAuthService)
	mockFileService := new(MockFileService)
	h := auth_http.NewAuthHandler(mockService, mockFileService, "client-id", 7*24*time.Hour, true, 15*time.Minute)
	defer mockService.AssertExpectations(t)

	mockService.On("Logout", mock.Anything, "active-refresh", (*uuid.UUID)(nil)).Return(errors.New("db failure")).Once()

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set(utils.HeaderSessionMarker, utils.SessionMarkerCookie)
	req.AddCookie(&http.Cookie{Name: utils.RefreshCookieName, Value: "active-refresh"})
	w := httptest.NewRecorder()

	h.Logout(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "failed to revoke session")
}

func TestAuthHandler_DuplicateCookiesAndHeaders(t *testing.T) {
	mockService := new(MockAuthService)
	mockFileService := new(MockFileService)
	h := auth_http.NewAuthHandler(mockService, mockFileService, "client-id", 7*24*time.Hour, true, 15*time.Minute)

	// Multiple session marker headers
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(`{}`))
	req.Header.Add(utils.HeaderSessionMarker, utils.SessionMarkerCookie)
	req.Header.Add(utils.HeaderSessionMarker, "other")
	w := httptest.NewRecorder()
	h.Login(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "duplicate session marker header")

	// Unknown session mode value
	req = httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(`{}`))
	req.Header.Set(utils.HeaderSessionMarker, "token")
	w = httptest.NewRecorder()
	h.Login(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "unknown session mode")

	// Duplicate refresh cookies
	req = httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	req.Header.Set(utils.HeaderSessionMarker, utils.SessionMarkerCookie)
	req.AddCookie(&http.Cookie{Name: utils.RefreshCookieName, Value: "tok1"})
	req.AddCookie(&http.Cookie{Name: utils.RefreshCookieName, Value: "tok2"})
	w = httptest.NewRecorder()
	h.Refresh(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "duplicate refresh cookies")

	// Duplicate access cookies
	req = httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set(utils.HeaderSessionMarker, utils.SessionMarkerCookie)
	req.AddCookie(&http.Cookie{Name: utils.AccessCookieNameSecure, Value: "tok1"})
	req.AddCookie(&http.Cookie{Name: utils.AccessCookieNameInsecure, Value: "tok2"})
	w = httptest.NewRecorder()
	h.Logout(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "duplicate access cookies")
}
