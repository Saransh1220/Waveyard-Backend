package http_test

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/saransh1220/blueprint-audio/internal/modules/auth/application"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	auth_http "github.com/saransh1220/blueprint-audio/internal/modules/auth/interfaces/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newFlowsHandler(t *testing.T, secure bool) (*auth_http.AuthHandler, *MockAuthService) {
	t.Helper()
	service := new(MockAuthService)
	files := new(MockFileService)
	t.Cleanup(func() {
		service.AssertExpectations(t)
		files.AssertExpectations(t)
	})
	return auth_http.NewAuthHandler(service, files, "client", 12*time.Hour, secure), service
}

func post(handler http.HandlerFunc, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body)))
	return w
}

func findCookie(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestAuthHandler_VerifyEmailBranches(t *testing.T) {
	h, service := newFlowsHandler(t, false)

	assert.Equal(t, http.StatusBadRequest, post(h.VerifyEmail, "not json").Code)

	service.On("VerifyEmail", mock.Anything, application.VerifyEmailRequest{Email: "a@a.com", Code: "1"}).Return(domain.ErrInvalidOrExpiredCode).Once()
	assert.Equal(t, http.StatusUnauthorized, post(h.VerifyEmail, `{"email":"a@a.com","code":"1"}`).Code, "a wrong or expired code is 401")

	service.On("VerifyEmail", mock.Anything, application.VerifyEmailRequest{Email: "", Code: ""}).Return(errors.New("email and code are required")).Once()
	w := post(h.VerifyEmail, `{}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "email and code are required")
}

func TestAuthHandler_ResendVerificationBranches(t *testing.T) {
	h, service := newFlowsHandler(t, false)

	assert.Equal(t, http.StatusBadRequest, post(h.ResendVerification, "{").Code)

	service.On("ResendVerification", mock.Anything, application.ResendVerificationRequest{Email: ""}).Return(errors.New("email is required")).Once()
	w := post(h.ResendVerification, `{}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "email is required")

	service.On("ResendVerification", mock.Anything, application.ResendVerificationRequest{Email: "a@a.com"}).Return(nil).Once()
	w = post(h.ResendVerification, `{"email":"a@a.com"}`)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "if the account exists", "the reply never reveals whether the account exists")
}

func TestAuthHandler_ForgotPasswordBranches(t *testing.T) {
	h, service := newFlowsHandler(t, false)

	assert.Equal(t, http.StatusBadRequest, post(h.ForgotPassword, "nope").Code)

	service.On("ForgotPassword", mock.Anything, application.ForgotPasswordRequest{Email: "a@a.com"}).Return(errors.New("smtp password=secret")).Once()
	w := post(h.ForgotPassword, `{"email":"a@a.com"}`)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "secret", "internal errors are not echoed to the client")
	assert.Contains(t, w.Body.String(), "internal server error")
}

func TestAuthHandler_ResetPasswordBranches(t *testing.T) {
	h, service := newFlowsHandler(t, false)
	req := application.ResetPasswordRequest{Email: "a@a.com", Code: "1", NewPassword: "longenough1"}
	body := `{"email":"a@a.com","code":"1","new_password":"longenough1"}`

	assert.Equal(t, http.StatusBadRequest, post(h.ResetPassword, "{").Code)

	service.On("ResetPassword", mock.Anything, req).Return(domain.ErrInvalidOrExpiredCode).Once()
	w := post(h.ResetPassword, body)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "invalid or expired code")

	service.On("ResetPassword", mock.Anything, req).Return(errors.New("password must be at least 8 characters")).Once()
	w = post(h.ResetPassword, body)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "at least 8 characters")

	service.On("ResetPassword", mock.Anything, req).Return(errors.New("email, code and new password are required")).Once()
	assert.Equal(t, http.StatusBadRequest, post(h.ResetPassword, body).Code)

	service.On("ResetPassword", mock.Anything, req).Return(errors.New("failed to revoke old sessions: db down")).Once()
	w = post(h.ResetPassword, body)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "db down", "internal errors are not echoed to the client")
}

func TestAuthHandler_RefreshFailuresPreserveCookies(t *testing.T) {
	h, service := newFlowsHandler(t, true)

	refresh := func(token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
		r.AddCookie(&http.Cookie{Name: "refresh_token", Value: token})
		w := httptest.NewRecorder()
		h.Refresh(w, r)
		return w
	}

	service.On("RefreshSession", mock.Anything, "bad").Return(nil, domain.ErrSessionNotFound).Once()
	w := refresh("bad")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Nil(t, findCookie(w, "refresh_token"), "a transient or unknown failure must not wipe the cookie")

	service.On("RefreshSession", mock.Anything, "banned").Return(nil, application.ErrAccountSuspended).Once()
	w = refresh("banned")
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Empty(t, w.Result().Cookies())
}

func TestAuthHandler_RotatedRefreshCookieAttributes(t *testing.T) {
	h, service := newFlowsHandler(t, true)
	service.On("RefreshSession", mock.Anything, "old").Return(&application.TokenPair{AccessToken: "a", RefreshToken: "rotated"}, nil).Once()

	r := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	r.AddCookie(&http.Cookie{Name: "refresh_token", Value: "old"})
	w := httptest.NewRecorder()
	h.Refresh(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	cookie := findCookie(w, "refresh_token")
	require.NotNil(t, cookie)
	assert.Equal(t, "rotated", cookie.Value)
	assert.True(t, cookie.HttpOnly, "JavaScript must never see the refresh token")
	assert.True(t, cookie.Secure)
	assert.Equal(t, http.SameSiteNoneMode, cookie.SameSite)
	assert.Equal(t, "/", cookie.Path)
	assert.WithinDuration(t, time.Now().Add(12*time.Hour), cookie.Expires, time.Minute)
}

func TestAuthHandler_LogoutBranches(t *testing.T) {
	h, service := newFlowsHandler(t, false)

	w := httptest.NewRecorder()
	h.Logout(w, httptest.NewRequest(http.MethodGet, "/auth/logout", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)

	w = httptest.NewRecorder()
	h.Logout(w, httptest.NewRequest(http.MethodPost, "/auth/logout", nil))
	assert.Equal(t, http.StatusOK, w.Code, "logging out without a cookie still succeeds")
	cookie := findCookie(w, "refresh_token")
	require.NotNil(t, cookie)
	assert.Equal(t, -1, cookie.MaxAge)

	service.On("Logout", mock.Anything, "tok").Return(errors.New("db down")).Once()
	r := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	r.AddCookie(&http.Cookie{Name: "refresh_token", Value: "tok"})
	w = httptest.NewRecorder()
	h.Logout(w, r)
	assert.Equal(t, http.StatusOK, w.Code, "the cookie is cleared even when revocation fails")
	cookie = findCookie(w, "refresh_token")
	require.NotNil(t, cookie)
	assert.Empty(t, cookie.Value)
}

func TestAuthHandler_RegisterAndLoginRemainingBranches(t *testing.T) {
	h, service := newFlowsHandler(t, false)

	w := httptest.NewRecorder()
	h.Register(w, httptest.NewRequest(http.MethodGet, "/register", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)

	service.On("Login", mock.Anything, application.LoginRequest{Email: "a@a.com", Password: "pw"}).Return((*application.TokenPair)(nil), domain.ErrEmailNotVerified).Once()
	assert.Equal(t, http.StatusForbidden, post(h.Login, `{"email":"a@a.com","password":"pw"}`).Code)

	service.On("Login", mock.Anything, application.LoginRequest{Email: "a@a.com", Password: "pw"}).Return((*application.TokenPair)(nil), application.ErrAccountSuspended).Once()
	w = post(h.Login, `{"email":"a@a.com","password":"pw"}`)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "account suspended")
	assert.Nil(t, findCookie(w, "refresh_token"), "no cookie on a failed login")
}

func TestAuthHandler_MeWithoutIdentity(t *testing.T) {
	h, _ := newFlowsHandler(t, false)
	w := httptest.NewRecorder()
	h.Me(w, httptest.NewRequest(http.MethodGet, "/me", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
