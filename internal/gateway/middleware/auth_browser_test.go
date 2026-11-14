package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/infrastructure/jwt"
	"github.com/saransh1220/blueprint-audio/internal/shared/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthMiddleware_BrowserCookie_Success(t *testing.T) {
	secret := "secret-1234567890"
	userID := uuid.New()
	sessionID := uuid.New()

	token, err := jwt.GenerateBrowserToken(secret, time.Hour, userID, sessionID, "user", "user")
	require.NoError(t, err)

	activeChecked := false
	lookup := func(ctx context.Context, sid uuid.UUID) (bool, error) {
		assert.Equal(t, sessionID, sid)
		activeChecked = true
		return true, nil
	}

	m := NewAuthMiddleware(secret).WithBrowserSessionLookup(lookup)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		assert.Equal(t, userID, r.Context().Value(ContextKeyUserId))
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set(utils.HeaderSessionMarker, utils.SessionMarkerCookie)
	req.AddCookie(&http.Cookie{Name: utils.AccessCookieNameSecure, Value: token})

	rec := httptest.NewRecorder()
	m.RequireAuth(next).ServeHTTP(rec, req)

	assert.True(t, nextCalled)
	assert.True(t, activeChecked)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAuthMiddleware_BrowserCookie_RevokedSession(t *testing.T) {
	secret := "secret-1234567890"
	userID := uuid.New()
	sessionID := uuid.New()

	token, err := jwt.GenerateBrowserToken(secret, time.Hour, userID, sessionID, "user", "user")
	require.NoError(t, err)

	lookup := func(ctx context.Context, sid uuid.UUID) (bool, error) {
		return false, nil // Session revoked in DB
	}

	m := NewAuthMiddleware(secret).WithBrowserSessionLookup(lookup)
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set(utils.HeaderSessionMarker, utils.SessionMarkerCookie)
	req.AddCookie(&http.Cookie{Name: utils.AccessCookieNameSecure, Value: token})

	rec := httptest.NewRecorder()
	m.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "session revoked")
}

func TestAuthMiddleware_BrowserCookie_DBOutage(t *testing.T) {
	secret := "secret-1234567890"
	userID := uuid.New()
	sessionID := uuid.New()

	token, err := jwt.GenerateBrowserToken(secret, time.Hour, userID, sessionID, "user", "user")
	require.NoError(t, err)

	lookup := func(ctx context.Context, sid uuid.UUID) (bool, error) {
		return false, errors.New("db down")
	}

	m := NewAuthMiddleware(secret).WithBrowserSessionLookup(lookup)
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set(utils.HeaderSessionMarker, utils.SessionMarkerCookie)
	req.AddCookie(&http.Cookie{Name: utils.AccessCookieNameSecure, Value: token})

	rec := httptest.NewRecorder()
	m.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), "unable to verify session")
}

func TestAuthMiddleware_InvalidCredentialCombination(t *testing.T) {
	secret := "secret-1234567890"
	m := NewAuthMiddleware(secret)

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set(utils.HeaderSessionMarker, utils.SessionMarkerCookie)
	req.Header.Set("Authorization", "Bearer something")
	req.AddCookie(&http.Cookie{Name: utils.AccessCookieNameSecure, Value: "tok"})

	rec := httptest.NewRecorder()
	m.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid credential combination")
}

func TestAuthMiddleware_PurposeIsolation(t *testing.T) {
	secret := "secret-1234567890"
	userID := uuid.New()
	sessionID := uuid.New()

	// 1. Browser token presented to legacy Bearer endpoint must be rejected
	browserToken, err := jwt.GenerateBrowserToken(secret, time.Hour, userID, sessionID, "user", "user")
	require.NoError(t, err)

	m := NewAuthMiddleware(secret)
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+browserToken)

	rec := httptest.NewRecorder()
	m.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid token purpose")

	// 2. Legacy Bearer token presented as a browser cookie must be rejected
	bearerToken, err := jwt.GenerateToken(secret, time.Hour, userID, "user", "user")
	require.NoError(t, err)

	req = httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set(utils.HeaderSessionMarker, utils.SessionMarkerCookie)
	req.AddCookie(&http.Cookie{Name: utils.AccessCookieNameSecure, Value: bearerToken})

	rec = httptest.NewRecorder()
	m.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid token purpose")
}

func TestAuthMiddleware_ExpectedViewer(t *testing.T) {
	secret := "secret-1234567890"
	userID := uuid.New()
	sessionID := uuid.New()

	token, err := jwt.GenerateBrowserToken(secret, time.Hour, userID, sessionID, "user", "user")
	require.NoError(t, err)

	lookup := func(ctx context.Context, sid uuid.UUID) (bool, error) {
		return true, nil
	}
	m := NewAuthMiddleware(secret).WithBrowserSessionLookup(lookup)

	// Matching viewer -> succeeds
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set(utils.HeaderSessionMarker, utils.SessionMarkerCookie)
	req.Header.Set(utils.HeaderExpectedViewer, userID.String())
	req.AddCookie(&http.Cookie{Name: utils.AccessCookieNameSecure, Value: token})
	rec := httptest.NewRecorder()
	m.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Mismatch viewer -> 409 SESSION_CHANGED
	otherUser := uuid.New()
	req = httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set(utils.HeaderSessionMarker, utils.SessionMarkerCookie)
	req.Header.Set(utils.HeaderExpectedViewer, otherUser.String())
	req.AddCookie(&http.Cookie{Name: utils.AccessCookieNameSecure, Value: token})
	rec = httptest.NewRecorder()
	m.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "SESSION_CHANGED")

	// Guest viewer with authenticated cookie -> 409 SESSION_CHANGED
	req = httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set(utils.HeaderSessionMarker, utils.SessionMarkerCookie)
	req.Header.Set(utils.HeaderExpectedViewer, "guest")
	req.AddCookie(&http.Cookie{Name: utils.AccessCookieNameSecure, Value: token})
	rec = httptest.NewRecorder()
	m.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "SESSION_CHANGED")
}

func TestAuthMiddleware_FlexibleAuth_ExpectedViewerAndErrors(t *testing.T) {
	secret := "secret-1234567890"
	userID := uuid.New()
	sessionID := uuid.New()

	lookup := func(ctx context.Context, sid uuid.UUID) (bool, error) {
		return false, errors.New("db down")
	}
	m := NewAuthMiddleware(secret).WithBrowserSessionLookup(lookup)

	// 1. Guest request with Expected-Viewer: guest -> succeeds as guest
	req := httptest.NewRequest(http.MethodGet, "/catalog/home", nil)
	req.Header.Set(utils.HeaderExpectedViewer, "guest")
	rec := httptest.NewRecorder()
	m.FlexibleAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Nil(t, r.Context().Value(ContextKeyUserId))
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 2. Guest request without token but with Expected-Viewer: <userId> -> 409 SESSION_CHANGED
	req = httptest.NewRequest(http.MethodGet, "/catalog/home", nil)
	req.Header.Set(utils.HeaderExpectedViewer, userID.String())
	rec = httptest.NewRecorder()
	m.FlexibleAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "SESSION_CHANGED")

	// 3. Infrastructure failure (503) must not be masked as guest in FlexibleAuth
	token, err := jwt.GenerateBrowserToken(secret, time.Hour, userID, sessionID, "user", "user")
	require.NoError(t, err)

	req = httptest.NewRequest(http.MethodGet, "/catalog/home", nil)
	req.Header.Set(utils.HeaderSessionMarker, utils.SessionMarkerCookie)
	req.AddCookie(&http.Cookie{Name: utils.AccessCookieNameSecure, Value: token})
	rec = httptest.NewRecorder()
	m.FlexibleAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), "unable to verify session")
}
