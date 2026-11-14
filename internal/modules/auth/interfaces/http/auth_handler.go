package http

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/gateway/middleware"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/application"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/infrastructure/jwt"
	"github.com/saransh1220/blueprint-audio/internal/shared/utils"
)

// AuthService defines the interface for auth operations
type AuthService interface {
	Register(ctx context.Context, req application.RegisterRequest) (*domain.User, error)
	Login(ctx context.Context, req application.LoginRequest, isBrowser ...bool) (*application.TokenPair, error)
	GetUser(ctx context.Context, id uuid.UUID) (*domain.User, error)
	GoogleLogin(ctx context.Context, googleClientID string, req application.GoogleLoginRequest, isBrowser ...bool) (*application.TokenPair, error)
	RefreshSession(ctx context.Context, refreshToken string, isBrowser ...bool) (*application.TokenPair, error)
	Logout(ctx context.Context, refreshToken string, accessFamilyID ...*uuid.UUID) error
	VerifyEmail(ctx context.Context, req application.VerifyEmailRequest) error
	ResendVerification(ctx context.Context, req application.ResendVerificationRequest) error
	ForgotPassword(ctx context.Context, req application.ForgotPasswordRequest) error
	ResetPassword(ctx context.Context, req application.ResetPasswordRequest) error
}

// FileService defines the interface for file operations
type FileService interface {
	GetKeyFromUrl(fileUrl string) (string, error)
	GetPresignedURL(ctx context.Context, objectName string, expiry time.Duration) (string, error)
}

type AuthHandler struct {
	service        AuthService
	fileService    FileService
	googleClientID string
	refreshExpiry  time.Duration
	secureCookie   bool
	jwtExpiry      time.Duration
}

func NewAuthHandler(service AuthService, fileService FileService, googleClientID string, refreshExpiry time.Duration, secureCookie bool, jwtExpiry ...time.Duration) *AuthHandler {
	expiry := 24 * time.Hour
	if len(jwtExpiry) > 0 && jwtExpiry[0] > 0 {
		expiry = jwtExpiry[0]
	}
	return &AuthHandler{
		service:        service,
		fileService:    fileService,
		googleClientID: googleClientID,
		refreshExpiry:  refreshExpiry,
		secureCookie:   secureCookie,
		jwtExpiry:      expiry,
	}
}

func (h *AuthHandler) setAccessCookie(w http.ResponseWriter, token string, expires time.Time) {
	name := utils.AccessCookieName(h.secureCookie)
	maxAge := int(time.Until(expires).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	http.SetCookie(w, utils.BuildAuthCookie(name, token, expires, maxAge, h.secureCookie))
}

func (h *AuthHandler) setRefreshCookie(w http.ResponseWriter, value string, expires time.Time, maxAge int, isBrowser bool) {
	if isBrowser {
		http.SetCookie(w, utils.BuildAuthCookie(utils.RefreshCookieName, value, expires, maxAge, h.secureCookie))
	} else {
		http.SetCookie(w, utils.BuildCookie(utils.RefreshCookieName, value, "/", expires, maxAge, h.secureCookie, true))
	}
}

func (h *AuthHandler) clearAuthCookies(w http.ResponseWriter) {
	http.SetCookie(w, utils.BuildDeleteAuthCookie(utils.AccessCookieNameSecure, h.secureCookie))
	http.SetCookie(w, utils.BuildDeleteAuthCookie(utils.AccessCookieNameInsecure, h.secureCookie))
	http.SetCookie(w, utils.BuildDeleteAuthCookie(utils.RefreshCookieName, h.secureCookie))
}

func (h *AuthHandler) clearLegacyRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, utils.BuildCookie(utils.RefreshCookieName, "", "/", time.Unix(0, 0), -1, h.secureCookie, true))
}

func checkDuplicateCookies(r *http.Request) error {
	accessCount := 0
	refreshCount := 0
	for _, c := range r.Cookies() {
		if c.Name == utils.AccessCookieNameSecure || c.Name == utils.AccessCookieNameInsecure {
			accessCount++
		}
		if c.Name == utils.RefreshCookieName {
			refreshCount++
		}
	}
	if accessCount > 1 {
		return errors.New("duplicate access cookies")
	}
	if refreshCount > 1 {
		return errors.New("duplicate refresh cookies")
	}
	return nil
}

func isBrowserSessionRequest(r *http.Request) (bool, error) {
	values := r.Header.Values(utils.HeaderSessionMarker)
	if len(values) == 0 {
		return false, nil
	}
	if len(values) > 1 {
		return false, errors.New("duplicate session marker header")
	}
	val := strings.TrimSpace(values[0])
	if val != utils.SessionMarkerCookie {
		return false, errors.New("unknown session mode")
	}
	return true, nil
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req application.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid input: "+err.Error(), http.StatusBadRequest)
		return
	}

	user, err := h.service.Register(r.Context(), req)
	if err != nil {
		if err == domain.ErrUserAlreadyExists {
			http.Error(w, `{"error": "user already exists"}`, http.StatusConflict)
			return
		}
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(user); err != nil {
		http.Error(w, `{"error": "failed to encode response"}`, http.StatusInternalServerError)
	}
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := checkDuplicateCookies(r); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	isBrowser, err := isBrowserSessionRequest(r)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	var req application.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	tokens, err := h.service.Login(r.Context(), req, isBrowser)
	if err != nil {
		if err == domain.ErrInvalidCredentials {
			http.Error(w, `{"error": "invalid credentials"}`, http.StatusUnauthorized)
			return
		}
		if err == domain.ErrEmailNotVerified {
			http.Error(w, `{"error": "email not verified"}`, http.StatusForbidden)
			return
		}
		if errors.Is(err, application.ErrAccountSuspended) {
			http.Error(w, `{"error": "account suspended"}`, http.StatusForbidden)
			return
		}
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	if isBrowser {
		h.setAccessCookie(w, tokens.AccessToken, time.Now().Add(h.jwtExpiry))
		h.setRefreshCookie(w, tokens.RefreshToken, time.Now().Add(h.refreshExpiry), int(h.refreshExpiry.Seconds()), true)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Set HTTP-Only Cookie for the Refresh Token
	h.setRefreshCookie(w, tokens.RefreshToken, time.Now().Add(h.refreshExpiry), 0, false)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"token": tokens.AccessToken})
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	userId, ok := r.Context().Value(middleware.ContextKeyUserId).(uuid.UUID)
	if !ok {
		http.Error(w, `{"error": "user not authenticated"}`, http.StatusUnauthorized)
		return
	}

	user, err := h.service.GetUser(r.Context(), userId)
	if err != nil {
		if err == domain.ErrUserNotFound {
			http.Error(w, `{"error": "user not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	// Generate presigned URLs for private profile images.
	for _, imageURL := range []*string{user.AvatarUrl, user.BannerURL} {
		if imageURL == nil || *imageURL == "" {
			continue
		}
		key, err := h.fileService.GetKeyFromUrl(*imageURL)
		if err != nil {
			continue
		}
		if presignedURL, err := h.fileService.GetPresignedURL(r.Context(), key, 3600*time.Second); err == nil {
			*imageURL = presignedURL
		}
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func (h *AuthHandler) GoogleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := checkDuplicateCookies(r); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	isBrowser, err := isBrowserSessionRequest(r)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	var req application.GoogleLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("GoogleLogin Error: invalid request body - %v", err)
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	tokens, err := h.service.GoogleLogin(r.Context(), h.googleClientID, req, isBrowser)
	if err != nil {
		log.Printf("GoogleLogin Auth Service Error: %v", err)
		if errors.Is(err, application.ErrGoogleAuthFailed) {
			http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusUnauthorized)
			return
		}
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	if isBrowser {
		h.setAccessCookie(w, tokens.AccessToken, time.Now().Add(h.jwtExpiry))
		h.setRefreshCookie(w, tokens.RefreshToken, time.Now().Add(h.refreshExpiry), int(h.refreshExpiry.Seconds()), true)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Set HTTP-Only Cookie for the Refresh Token
	h.setRefreshCookie(w, tokens.RefreshToken, time.Now().Add(h.refreshExpiry), 0, false)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"token": tokens.AccessToken})
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := checkDuplicateCookies(r); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	isBrowser, err := isBrowserSessionRequest(r)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	// Read refresh token from HttpOnly cookie
	cookie, err := r.Cookie("refresh_token")
	if err != nil || cookie.Value == "" {
		http.Error(w, `{"error": "refresh token missing"}`, http.StatusUnauthorized)
		return
	}

	tokens, err := h.service.RefreshSession(r.Context(), cookie.Value, isBrowser)
	if err != nil {
		log.Printf("Refresh Error (Service): %v", err)
		if errors.Is(err, application.ErrAccountSuspended) {
			if isBrowser {
				h.clearAuthCookies(w)
			} else {
				h.clearLegacyRefreshCookie(w)
			}
			http.Error(w, `{"error": "account suspended"}`, http.StatusForbidden)
			return
		}
		if errors.Is(err, domain.ErrSessionModeMismatch) {
			http.Error(w, `{"error": "invalid session mode"}`, http.StatusUnauthorized)
			return
		}
		// Do not delete cookies on ordinary invalid refresh responses
		http.Error(w, `{"error": "invalid or expired refresh token"}`, http.StatusUnauthorized)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	if isBrowser {
		h.setAccessCookie(w, tokens.AccessToken, time.Now().Add(h.jwtExpiry))
		if tokens.RefreshToken != "" {
			h.setRefreshCookie(w, tokens.RefreshToken, time.Now().Add(h.refreshExpiry), int(h.refreshExpiry.Seconds()), true)
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Rotation: the response carries a brand-new refresh token cookie if not in grace window.
	if tokens.RefreshToken != "" {
		h.setRefreshCookie(w, tokens.RefreshToken, time.Now().Add(h.refreshExpiry), 0, false)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"token": tokens.AccessToken})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := checkDuplicateCookies(r); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	isBrowser, err := isBrowserSessionRequest(r)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	var refreshToken string
	cookie, err := r.Cookie("refresh_token")
	if err == nil && cookie.Value != "" {
		refreshToken = cookie.Value
	}

	// Try extracting family ID from access cookie if present (unverified signature parse for revocation scope)
	var accessFamilyID *uuid.UUID
	accessCookieName := utils.AccessCookieName(h.secureCookie)
	if accCookie, err := r.Cookie(accessCookieName); err == nil && accCookie.Value != "" {
		if claims, err := jwt.ValidateToken(accCookie.Value, ""); err == nil && claims != nil {
			accessFamilyID = claims.SessionID
		}
	} else if inaccCookie, err := r.Cookie(utils.AccessCookieNameInsecure); err == nil && inaccCookie.Value != "" {
		if claims, err := jwt.ValidateToken(inaccCookie.Value, ""); err == nil && claims != nil {
			accessFamilyID = claims.SessionID
		}
	}

	if refreshToken != "" || accessFamilyID != nil {
		if err := h.service.Logout(r.Context(), refreshToken, accessFamilyID); err != nil {
			log.Printf("Failed to revoke session: %v", err)
			if isBrowser {
				http.Error(w, `{"error": "failed to revoke session"}`, http.StatusServiceUnavailable)
				return
			}
		}
	}

	w.Header().Set("Cache-Control", "no-store")
	if isBrowser {
		h.clearAuthCookies(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	h.clearLegacyRefreshCookie(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "logged out successfully"})
}

func (h *AuthHandler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req application.VerifyEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if err := h.service.VerifyEmail(r.Context(), req); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, domain.ErrInvalidOrExpiredCode) {
			status = http.StatusUnauthorized
		}
		http.Error(w, `{"error":"`+err.Error()+`"}`, status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "email verified"})
}

func (h *AuthHandler) ResendVerification(w http.ResponseWriter, r *http.Request) {
	var req application.ResendVerificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if err := h.service.ResendVerification(r.Context(), req); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "if the account exists, a verification email has been sent"})
}

func (h *AuthHandler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req application.ForgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if err := h.service.ForgotPassword(r.Context(), req); err != nil {
		log.Printf("ForgotPassword error: %v", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "if the account exists, a reset email has been sent"})
}

func (h *AuthHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req application.ResetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if err := h.service.ResetPassword(r.Context(), req); err != nil {
		if errors.Is(err, domain.ErrInvalidOrExpiredCode) {
			http.Error(w, `{"error":"invalid or expired code"}`, http.StatusUnauthorized)
			return
		}
		if err.Error() == "email, code and new password are required" || err.Error() == "password must be at least 8 characters" {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
			return
		}
		log.Printf("ResetPassword error: %v", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "password reset successful"})
}
