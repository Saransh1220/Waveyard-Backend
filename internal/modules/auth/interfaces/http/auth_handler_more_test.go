package http_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/gateway/middleware"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/application"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	auth_http "github.com/saransh1220/blueprint-audio/internal/modules/auth/interfaces/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestAuthHandler_LoginAndMeBranches(t *testing.T) {
	mockService := new(MockAuthService)
	mockFileService := new(MockFileService)
	const refreshTTL = 12 * time.Hour
	h := auth_http.NewAuthHandler(mockService, mockFileService, "test-client-id", refreshTTL, true)
	t.Cleanup(func() {
		mockService.AssertExpectations(t)
		mockFileService.AssertExpectations(t)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString("bad"))
	h.Login(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(`{"email":"x","password":"y"}`))
	mockService.On("Login", mock.Anything, application.LoginRequest{Email: "x", Password: "y"}).Return((*application.TokenPair)(nil), domain.ErrInvalidCredentials).Once()
	h.Login(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(`{"email":"x","password":"y"}`))
	mockService.On("Login", mock.Anything, application.LoginRequest{Email: "x", Password: "y"}).Return((*application.TokenPair)(nil), errors.New("db")).Once()
	h.Login(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(`{"email":"x","password":"y"}`))
	mockService.On("Login", mock.Anything, application.LoginRequest{Email: "x", Password: "y"}).Return((*application.TokenPair)(nil), domain.ErrEmailNotVerified).Once()
	h.Login(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(`{"email":"x","password":"y"}`))
	mockService.On("Login", mock.Anything, application.LoginRequest{Email: "x", Password: "y"}).Return(&application.TokenPair{AccessToken: "token", RefreshToken: "refresh-1"}, nil).Once()
	h.Login(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	cookies := w.Result().Cookies()
	if assert.Len(t, cookies, 1) {
		assert.Equal(t, "refresh_token", cookies[0].Name)
		assert.Equal(t, "refresh-1", cookies[0].Value)
		assert.True(t, cookies[0].HttpOnly)
		assert.True(t, cookies[0].Secure)
		assert.Equal(t, http.SameSiteNoneMode, cookies[0].SameSite)
		assert.WithinDuration(t, time.Now().Add(refreshTTL), cookies[0].Expires, time.Minute)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/me", nil)
	h.Me(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	userID := uuid.New()
	ctx := context.WithValue(context.Background(), middleware.ContextKeyUserId, userID)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/me", nil).WithContext(ctx)
	mockService.On("GetUser", mock.Anything, userID).Return(nil, domain.ErrUserNotFound).Once()
	h.Me(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/me", nil).WithContext(ctx)
	mockService.On("GetUser", mock.Anything, userID).Return(nil, errors.New("db")).Once()
	h.Me(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	avatar := "http://storage/u/a.jpg"
	user := &domain.User{ID: userID, Email: "a@a.com", AvatarUrl: &avatar}
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/me", nil).WithContext(ctx)
	mockService.On("GetUser", mock.Anything, userID).Return(user, nil).Once()
	mockFileService.On("GetKeyFromUrl", avatar).Return("u/a.jpg", nil).Once()
	mockFileService.On("GetPresignedURL", mock.Anything, "u/a.jpg", mock.Anything).Return("signed", nil).Once()
	h.Me(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthHandler_RefreshAndLogout(t *testing.T) {
	service := new(MockAuthService)
	files := new(MockFileService)
	h := auth_http.NewAuthHandler(service, files, "client", time.Hour, true)
	t.Cleanup(func() { service.AssertExpectations(t); files.AssertExpectations(t) })
	w := httptest.NewRecorder()
	h.Refresh(w, httptest.NewRequest(http.MethodGet, "/refresh", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	w = httptest.NewRecorder()
	h.Refresh(w, httptest.NewRequest(http.MethodPost, "/refresh", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	service.On("RefreshSession", mock.Anything, "old").Return(&application.TokenPair{AccessToken: "new-access", RefreshToken: "rotated"}, nil).Once()
	req := httptest.NewRequest(http.MethodPost, "/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: "old"})
	w = httptest.NewRecorder()
	h.Refresh(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "new-access")
	rotated := w.Result().Cookies()
	if assert.NotEmpty(t, rotated) {
		assert.Equal(t, "refresh_token", rotated[0].Name)
		assert.Equal(t, "rotated", rotated[0].Value)
	}

	service.On("RefreshSession", mock.Anything, "graced").Return(&application.TokenPair{AccessToken: "grace-access"}, nil).Once()
	req = httptest.NewRequest(http.MethodPost, "/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: "graced"})
	w = httptest.NewRecorder()
	h.Refresh(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "grace-access")
	assert.Empty(t, w.Result().Cookies(), "grace-window refresh must not overwrite the cookie another tab just received")

	service.On("RefreshSession", mock.Anything, "banned").Return(nil, application.ErrAccountSuspended).Once()
	req = httptest.NewRequest(http.MethodPost, "/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: "banned"})
	w = httptest.NewRecorder()
	h.Refresh(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Empty(t, w.Result().Cookies())
	service.On("Logout", mock.Anything, "old").Return(nil).Once()
	req = httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: "old"})
	w = httptest.NewRecorder()
	h.Logout(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	cookies := w.Result().Cookies()
	if assert.NotEmpty(t, cookies) {
		assert.Equal(t, "refresh_token", cookies[0].Name)
		assert.Equal(t, -1, cookies[0].MaxAge)
	}
}

func TestAuthHandler_RegisterMethodAndDecode(t *testing.T) {
	mockService := new(MockAuthService)
	mockFileService := new(MockFileService)
	h := auth_http.NewAuthHandler(mockService, mockFileService, "test-client-id", time.Hour*720, true)
	t.Cleanup(func() {
		mockService.AssertExpectations(t)
		mockFileService.AssertExpectations(t)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/register", nil)
	h.Register(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/register", bytes.NewBufferString("bad"))
	h.Register(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAuthHandler_GoogleLoginBranches(t *testing.T) {
	mockService := new(MockAuthService)
	mockFileService := new(MockFileService)
	const refreshTTL = 12 * time.Hour
	h := auth_http.NewAuthHandler(mockService, mockFileService, "test-client-id", refreshTTL, true)
	t.Cleanup(func() {
		mockService.AssertExpectations(t)
		mockFileService.AssertExpectations(t)
	})

	// bad json
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/google", bytes.NewBufferString("bad"))
	h.GoogleLogin(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// auth failure — wrapped ErrGoogleAuthFailed so handler returns 401
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/auth/google", bytes.NewBufferString(`{"token":"x"}`))
	mockService.On("GoogleLogin", mock.Anything, "test-client-id", application.GoogleLoginRequest{Token: "x"}).Return((*application.TokenPair)(nil), fmt.Errorf("invalid google token: %w", application.ErrGoogleAuthFailed)).Once()
	h.GoogleLogin(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// internal error — plain error (not ErrGoogleAuthFailed) → 500
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/auth/google", bytes.NewBufferString(`{"token":"z"}`))
	mockService.On("GoogleLogin", mock.Anything, "test-client-id", application.GoogleLoginRequest{Token: "z"}).Return((*application.TokenPair)(nil), errors.New("db down")).Once()
	h.GoogleLogin(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// success
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/auth/google", bytes.NewBufferString(`{"token":"y"}`))
	mockService.On("GoogleLogin", mock.Anything, "test-client-id", application.GoogleLoginRequest{Token: "y"}).Return(&application.TokenPair{AccessToken: "jwt-token", RefreshToken: "refresh-google"}, nil).Once()
	h.GoogleLogin(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "jwt-token")
	cookies := w.Result().Cookies()
	if assert.Len(t, cookies, 1) {
		assert.Equal(t, "refresh_token", cookies[0].Name)
		assert.Equal(t, "refresh-google", cookies[0].Value)
		assert.True(t, cookies[0].HttpOnly)
		assert.True(t, cookies[0].Secure)
		assert.Equal(t, http.SameSiteNoneMode, cookies[0].SameSite)
		assert.WithinDuration(t, time.Now().Add(refreshTTL), cookies[0].Expires, time.Minute)
	}
}

func TestAuthHandler_LoginUsesDevCookieSettings(t *testing.T) {
	mockService := new(MockAuthService)
	mockFileService := new(MockFileService)
	const refreshTTL = 12 * time.Hour
	h := auth_http.NewAuthHandler(mockService, mockFileService, "test-client-id", refreshTTL, false)
	t.Cleanup(func() {
		mockService.AssertExpectations(t)
		mockFileService.AssertExpectations(t)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(`{"email":"x","password":"y"}`))
	mockService.On("Login", mock.Anything, application.LoginRequest{Email: "x", Password: "y"}).Return(&application.TokenPair{AccessToken: "token", RefreshToken: "refresh-1"}, nil).Once()
	h.Login(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	cookies := w.Result().Cookies()
	if assert.Len(t, cookies, 1) {
		assert.Equal(t, "refresh_token", cookies[0].Name)
		assert.Equal(t, "refresh-1", cookies[0].Value)
		assert.True(t, cookies[0].HttpOnly)
		assert.False(t, cookies[0].Secure)
		assert.Equal(t, http.SameSiteStrictMode, cookies[0].SameSite)
		assert.WithinDuration(t, time.Now().Add(refreshTTL), cookies[0].Expires, time.Minute)
	}
}

func TestAuthHandler_EmailActionEndpoints(t *testing.T) {
	mockService := new(MockAuthService)
	mockFileService := new(MockFileService)
	h := auth_http.NewAuthHandler(mockService, mockFileService, "test-client-id", 12*time.Hour, false)
	t.Cleanup(func() {
		mockService.AssertExpectations(t)
		mockFileService.AssertExpectations(t)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/verify-email", bytes.NewBufferString(`{"email":"user@example.com","code":"123456"}`))
	mockService.On("VerifyEmail", mock.Anything, application.VerifyEmailRequest{Email: "user@example.com", Code: "123456"}).Return(nil).Once()
	h.VerifyEmail(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/auth/resend-verification", bytes.NewBufferString(`{"email":"user@example.com"}`))
	mockService.On("ResendVerification", mock.Anything, application.ResendVerificationRequest{Email: "user@example.com"}).Return(nil).Once()
	h.ResendVerification(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewBufferString(`{"email":"user@example.com"}`))
	mockService.On("ForgotPassword", mock.Anything, application.ForgotPasswordRequest{Email: "user@example.com"}).Return(nil).Once()
	h.ForgotPassword(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/auth/reset-password", bytes.NewBufferString(`{"email":"user@example.com","code":"123456","new_password":"newpassword123"}`))
	mockService.On("ResetPassword", mock.Anything, application.ResetPasswordRequest{Email: "user@example.com", Code: "123456", NewPassword: "newpassword123"}).Return(nil).Once()
	h.ResetPassword(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}
