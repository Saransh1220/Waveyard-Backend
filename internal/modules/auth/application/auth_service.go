package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"log"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/infrastructure/jwt"
	sharedemail "github.com/saransh1220/blueprint-audio/internal/shared/infrastructure/email"
	"github.com/saransh1220/blueprint-audio/internal/shared/utils"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/api/idtoken"
)

var (
	ErrGoogleAuthFailed            = errors.New("google authentication failed")
	ErrGoogleClientIDNotConfigured = errors.New("google oauth client id is not configured")
	ErrAccountSuspended            = domain.ErrAccountSuspended
)

type googleAuthError struct {
	msg string
}

func (e googleAuthError) Error() string {
	return e.msg
}

func (e googleAuthError) Is(target error) bool {
	return target == ErrGoogleAuthFailed
}

func newGoogleAuthError(msg string) error {
	return googleAuthError{msg: msg}
}

func isGoogleEmailVerified(claim any) bool {
	switch value := claim.(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(strings.TrimSpace(value), "true")
	default:
		return false
	}
}

func authLogKey(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}

	var hasher hash.Hash = sha256.New()
	_, _ = hasher.Write([]byte(strings.ToLower(strings.TrimSpace(value))))
	sum := hasher.Sum(nil)
	return fmt.Sprintf("%x", sum[:6])
}

type RegisterRequest struct {
	Email              string `json:"email"`
	Password           string `json:"password"`
	Name               string `json:"name"`
	DisplayName        string `json:"display_name"`
	Role               string `json:"role"`
	RegistrationSource string `json:"registration_source"` // e.g. "waveyard", "sonically", "blueprint"
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type VerifyEmailRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type ResendVerificationRequest struct {
	Email string `json:"email"`
}

type ForgotPasswordRequest struct {
	Email string `json:"email"`
}

type ResetPasswordRequest struct {
	Email       string `json:"email"`
	Code        string `json:"code"`
	NewPassword string `json:"new_password"`
}

type AuthService struct {
	userRepo             domain.UserRepository
	sessionRepo          domain.SessionRepository
	tokenRepo            domain.EmailActionTokenRepository
	jwtSecret            string
	jwtExpiry            time.Duration
	jwtRefreshExpiry     time.Duration
	googleTokenValidator func(ctx context.Context, token string, audience string) (*idtoken.Payload, error)
	emailSender          sharedemail.Sender
	appBaseURL           string
}

type GoogleLoginRequest struct {
	Token string `json:"token"`
}

type TokenPair struct {
	AccessToken  string
	RefreshToken string
	IsBrowser    bool
	FamilyID     *uuid.UUID
}

func NewAuthService(userRepo domain.UserRepository, sessionRepo domain.SessionRepository, tokenRepo domain.EmailActionTokenRepository, emailSender sharedemail.Sender, appBaseURL string, jwtSecret string, jwtExpiry time.Duration, jwtRefreshExpiry time.Duration) *AuthService {
	if emailSender == nil {
		emailSender = sharedemail.NewSender(sharedemail.Config{})
	}
	return &AuthService{
		userRepo:             userRepo,
		sessionRepo:          sessionRepo,
		tokenRepo:            tokenRepo,
		jwtSecret:            jwtSecret,
		jwtExpiry:            jwtExpiry,
		jwtRefreshExpiry:     jwtRefreshExpiry,
		googleTokenValidator: idtoken.Validate,
		emailSender:          emailSender,
		appBaseURL:           appBaseURL,
	}
}

func (s *AuthService) Register(ctx context.Context, req RegisterRequest) (*domain.User, error) {
	if req.Email == "" {
		return nil, errors.New("email is required")
	}
	if req.DisplayName == "" {
		return nil, errors.New("display name is required")
	}
	if len(req.Password) < 8 {
		return nil, errors.New("password must be at least 8 characters")
	}
	if !utils.IsValidEmail(req.Email) {
		return nil, errors.New("invalid email format")
	}

	hashedPass, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	role := domain.UserRole(req.Role)
	if role != domain.RoleArtist && role != domain.RoleProducer {
		return nil, errors.New("invalid role")
	}

	var displayName *string
	if req.DisplayName != "" {
		displayName = &req.DisplayName
	}

	userID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("failed to generate uuid: %w", err)
	}

	source := req.RegistrationSource
	if source == "" {
		source = "waveyard"
	}

	user := &domain.User{
		ID:                 userID,
		Email:              req.Email,
		PasswordHash:       string(hashedPass),
		Name:               req.Name,
		DisplayName:        displayName,
		Role:               role,
		SystemRole:         domain.SystemRoleUser,
		Status:             domain.UserStatusActive,
		EmailVerified:      false,
		RegistrationSource: source,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, err
	}

	if err := s.sendVerificationCode(ctx, user); err != nil {
		log.Printf("AuthService.Register failed to send verification code. user_id=%s err=%v", user.ID, err)
	}

	return user, nil
}

func (s *AuthService) Login(ctx context.Context, req LoginRequest, isBrowser ...bool) (*TokenPair, error) {
	if req.Email == "" || req.Password == "" {
		return nil, errors.New("missing email or password")
	}

	user, err := s.userRepo.GetByEmail(ctx, req.Email)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return nil, domain.ErrInvalidCredentials
		}
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, domain.ErrInvalidCredentials
	}
	if !user.EmailVerified {
		return nil, domain.ErrEmailNotVerified
	}
	if user.Status == domain.UserStatusSuspended {
		return nil, ErrAccountSuspended
	}

	if len(isBrowser) > 0 && isBrowser[0] {
		return s.generateBrowserSession(ctx, user)
	}
	return s.generateSession(ctx, user)
}

func (s *AuthService) GetUser(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	return s.userRepo.GetByID(ctx, id)
}

func (s *AuthService) ValidateToken(tokenStr string) (*jwt.CustomClaims, error) {
	return jwt.ValidateToken(tokenStr, s.jwtSecret)
}

func (s *AuthService) GoogleLogin(ctx context.Context, googleClientID string, req GoogleLoginRequest, isBrowser ...bool) (*TokenPair, error) {
	if strings.TrimSpace(googleClientID) == "" {
		return nil, ErrGoogleClientIDNotConfigured
	}
	if strings.TrimSpace(req.Token) == "" {
		return nil, newGoogleAuthError("google token is required")
	}

	log.Printf("AuthService.GoogleLogin started. client_id_length=%d token_length=%d", len(googleClientID), len(req.Token))

	validate := s.googleTokenValidator
	if validate == nil {
		validate = idtoken.Validate
	}

	payload, err := validate(ctx, req.Token, googleClientID)
	if err != nil {
		log.Printf("AuthService.GoogleLogin token validate failed: %v", err)
		return nil, newGoogleAuthError("invalid google token")
	}

	email, _ := payload.Claims["email"].(string)
	emailVerified := isGoogleEmailVerified(payload.Claims["email_verified"])
	name, _ := payload.Claims["name"].(string)
	picture, _ := payload.Claims["picture"].(string)
	accountKey := authLogKey(email)

	log.Printf("AuthService.GoogleLogin token valid. account=%s", accountKey)

	if email == "" {
		log.Printf("AuthService.GoogleLogin missing email claim. account=%s", accountKey)
		return nil, newGoogleAuthError("email not provided by google")
	}
	if !emailVerified {
		log.Printf("AuthService.GoogleLogin email not verified. account=%s", accountKey)
		return nil, newGoogleAuthError("google email is not verified")
	}

	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			log.Printf("AuthService.GoogleLogin creating user. account=%s", accountKey)
			userID, uuidErr := uuid.NewV7()
			if uuidErr != nil {
				return nil, fmt.Errorf("failed to generate uuid: %w", uuidErr)
			}

			user = &domain.User{
				ID:              userID,
				Email:           email,
				PasswordHash:    "",
				Name:            name,
				DisplayName:     &name,
				Role:            domain.RoleArtist,
				SystemRole:      domain.SystemRoleUser,
				Status:          domain.UserStatusActive,
				EmailVerified:   true,
				EmailVerifiedAt: timePtr(time.Now()),
				AvatarUrl:       &picture,
				CreatedAt:       time.Now(),
				UpdatedAt:       time.Now(),
			}
			if createErr := s.userRepo.Create(ctx, user); createErr != nil {
				log.Printf("AuthService.GoogleLogin failed to create user. account=%s err=%v", accountKey, createErr)
				return nil, createErr
			}
		} else {
			log.Printf("AuthService.GoogleLogin repo error. account=%s err=%v", accountKey, err)
			return nil, err
		}
	} else {
		log.Printf("AuthService.GoogleLogin user found. account=%s user_id=%s", accountKey, user.ID)
	}

	var tokens *TokenPair
	if len(isBrowser) > 0 && isBrowser[0] {
		tokens, err = s.generateBrowserSession(ctx, user)
	} else {
		tokens, err = s.generateSession(ctx, user)
	}
	if err != nil {
		log.Printf("AuthService.GoogleLogin failed to generate session. user_id=%s err=%v", user.ID, err)
		return nil, err
	}

	log.Printf("AuthService.GoogleLogin succeeded. user_id=%s", user.ID)
	return tokens, nil
}

func (s *AuthService) VerifyEmail(ctx context.Context, req VerifyEmailRequest) error {
	if strings.TrimSpace(req.Email) == "" || strings.TrimSpace(req.Code) == "" {
		return errors.New("email and code are required")
	}

	token, err := s.tokenRepo.Consume(ctx, req.Email, domain.TokenPurposeVerifyEmail, strings.TrimSpace(req.Code))
	if err != nil {
		return err
	}

	return s.userRepo.MarkEmailVerified(ctx, token.UserID)
}

func (s *AuthService) ResendVerification(ctx context.Context, req ResendVerificationRequest) error {
	if strings.TrimSpace(req.Email) == "" {
		return errors.New("email is required")
	}
	log.Printf("AuthService.ResendVerification requested. email=%s", authLogKey(req.Email))

	user, err := s.userRepo.GetByEmail(ctx, strings.TrimSpace(req.Email))
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			log.Printf("AuthService.ResendVerification user not found. email=%s", authLogKey(req.Email))
			return nil
		}
		return err
	}
	if user.EmailVerified {
		log.Printf("AuthService.ResendVerification skipped verified user. user_id=%s", user.ID)
		return nil
	}
	return s.sendVerificationCode(ctx, user)
}

func (s *AuthService) ForgotPassword(ctx context.Context, req ForgotPasswordRequest) error {
	if strings.TrimSpace(req.Email) == "" {
		return errors.New("email is required")
	}
	log.Printf("AuthService.ForgotPassword requested. email=%s", authLogKey(req.Email))

	user, err := s.userRepo.GetByEmail(ctx, strings.TrimSpace(req.Email))
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			log.Printf("AuthService.ForgotPassword user not found. email=%s", authLogKey(req.Email))
			return nil
		}
		return err
	}

	code, err := s.createEmailActionToken(ctx, user, domain.TokenPurposeResetPassword, 15*time.Minute)
	if err != nil {
		return err
	}

	name := user.Name
	if user.DisplayName != nil && strings.TrimSpace(*user.DisplayName) != "" {
		name = *user.DisplayName
	}
	return s.emailSender.Send(ctx, sharedemail.BuildPasswordResetEmail(user.Email, name, code, s.appBaseURL))
}

func (s *AuthService) ResetPassword(ctx context.Context, req ResetPasswordRequest) error {
	if strings.TrimSpace(req.Email) == "" || strings.TrimSpace(req.Code) == "" || strings.TrimSpace(req.NewPassword) == "" {
		return errors.New("email, code and new password are required")
	}
	if len(req.NewPassword) < 8 {
		return errors.New("password must be at least 8 characters")
	}

	token, err := s.tokenRepo.Consume(ctx, req.Email, domain.TokenPurposeResetPassword, strings.TrimSpace(req.Code))
	if err != nil {
		return err
	}

	hashedPass, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := s.sessionRepo.RevokeAllForUser(ctx, token.UserID); err != nil {
		return fmt.Errorf("failed to revoke old sessions: %w", err)
	}
	if err := s.userRepo.UpdatePassword(ctx, token.UserID, string(hashedPass)); err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}
	return nil
}

func (s *AuthService) generateBrowserSession(ctx context.Context, user *domain.User) (*TokenPair, error) {
	famID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("failed to generate uuid: %w", err)
	}
	now := time.Now()
	browserSession := &domain.BrowserSession{
		ID:        famID,
		UserID:    user.ID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.sessionRepo.CreateBrowserSession(ctx, browserSession); err != nil {
		return nil, err
	}

	accessToken, err := jwt.GenerateBrowserToken(s.jwtSecret, s.jwtExpiry, user.ID, famID, string(user.Role), string(user.SystemRole))
	if err != nil {
		return nil, err
	}

	refreshTokenString, err := jwt.GenerateRefreshToken()
	if err != nil {
		return nil, err
	}

	sessionID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("failed to generate uuid: %w", err)
	}

	session := &domain.UserSession{
		ID:               sessionID,
		UserID:           user.ID,
		RefreshToken:     refreshTokenString,
		IsRevoked:        false,
		ExpiresAt:        now.Add(s.jwtRefreshExpiry),
		CreatedAt:        now,
		UpdatedAt:        now,
		BrowserSessionID: &famID,
	}

	if err := s.sessionRepo.Create(ctx, session); err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshTokenString,
		IsBrowser:    true,
		FamilyID:     &famID,
	}, nil
}

func (s *AuthService) generateSession(ctx context.Context, user *domain.User) (*TokenPair, error) {
	tokens, _, err := s.issueSession(ctx, user)
	return tokens, err
}

// issueSession creates an access token, a refresh token and the session row
// that backs it, and returns the new session's id alongside the tokens.
func (s *AuthService) issueSession(ctx context.Context, user *domain.User) (*TokenPair, uuid.UUID, error) {
	accessToken, err := jwt.GenerateToken(s.jwtSecret, s.jwtExpiry, user.ID, string(user.Role), string(user.SystemRole))
	if err != nil {
		return nil, uuid.Nil, err
	}

	refreshTokenString, err := jwt.GenerateRefreshToken()
	if err != nil {
		return nil, uuid.Nil, err
	}

	expiresAt := time.Now().Add(s.jwtRefreshExpiry)
	sessionID, err := uuid.NewV7()
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("failed to generate uuid: %w", err)
	}

	session := &domain.UserSession{
		ID:           sessionID,
		UserID:       user.ID,
		RefreshToken: refreshTokenString,
		IsRevoked:    false,
		ExpiresAt:    expiresAt,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := s.sessionRepo.Create(ctx, session); err != nil {
		return nil, uuid.Nil, err
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshTokenString,
	}, sessionID, nil
}

// refreshGraceWindow is how long after a rotation the old refresh token may
// still be presented. Two tabs share one cookie jar, so the loser of a refresh
// race arrives holding the token the winner just replaced.
const refreshGraceWindow = 10 * time.Second

var errSessionRevoked = errors.New("session has been revoked")

// RefreshSession exchanges a valid refresh token for a new access token and optionally a
// new refresh token. When isBrowser is true, it operates in browser mode: upgrades legacy sessions
// to a browser session family, sets sid on the issued JWT, and returns IsBrowser=true.
// In grace window, no new refresh token is issued (RefreshToken=""), returning only access token.
func (s *AuthService) RefreshSession(ctx context.Context, refreshToken string, isBrowser ...bool) (*TokenPair, error) {
	if refreshToken == "" {
		return nil, errors.New("refresh token is required")
	}
	isBrowserMode := len(isBrowser) > 0 && isBrowser[0]

	newRefreshToken, err := jwt.GenerateRefreshToken()
	if err != nil {
		return nil, err
	}
	newSessionID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("failed to generate uuid: %w", err)
	}
	newDigestBytes := sha256.Sum256([]byte(newRefreshToken))
	newDigestHex := hex.EncodeToString(newDigestBytes[:])
	expiresAt := time.Now().Add(s.jwtRefreshExpiry)

	rotated, userID, role, systemRole, familyID, err := s.sessionRepo.RotateSessionTx(ctx, refreshToken, newSessionID, newDigestHex, expiresAt, isBrowserMode)
	if err != nil {
		if errors.Is(err, domain.ErrSessionNotFound) {
			return nil, errors.New("invalid refresh token")
		}
		if errors.Is(err, domain.ErrSessionExpired) {
			return nil, errors.New("refresh token expired")
		}
		if errors.Is(err, domain.ErrSessionRevoked) {
			return nil, errSessionRevoked
		}
		if errors.Is(err, domain.ErrAccountSuspended) {
			return nil, ErrAccountSuspended
		}
		if errors.Is(err, domain.ErrSessionModeMismatch) {
			return nil, domain.ErrSessionModeMismatch
		}
		return nil, err
	}

	if familyID != nil {
		accessToken, err := jwt.GenerateBrowserToken(s.jwtSecret, s.jwtExpiry, userID, *familyID, role, systemRole)
		if err != nil {
			return nil, err
		}
		issuedRefresh := ""
		if rotated {
			issuedRefresh = newRefreshToken
		}
		return &TokenPair{
			AccessToken:  accessToken,
			RefreshToken: issuedRefresh,
			IsBrowser:    true,
			FamilyID:     familyID,
		}, nil
	}

	accessToken, err := jwt.GenerateToken(s.jwtSecret, s.jwtExpiry, userID, role, systemRole)
	if err != nil {
		return nil, err
	}
	issuedRefresh := ""
	if rotated {
		issuedRefresh = newRefreshToken
	}
	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: issuedRefresh,
		IsBrowser:    false,
		FamilyID:     nil,
	}, nil
}

// discardSession best-effort revokes a session that was created but never handed out.
func (s *AuthService) discardSession(ctx context.Context, refreshToken string) {
	if err := s.sessionRepo.Revoke(ctx, refreshToken); err != nil {
		log.Printf("AuthService.discardSession failed to revoke unused session: %v", err)
	}
}

func (s *AuthService) Logout(ctx context.Context, refreshToken string, accessFamilyID ...*uuid.UUID) error {
	if refreshToken != "" {
		if err := s.sessionRepo.RevokeBrowserFamilyByToken(ctx, refreshToken); err != nil {
			return err
		}
	}
	if len(accessFamilyID) > 0 && accessFamilyID[0] != nil {
		if err := s.sessionRepo.RevokeBrowserSession(ctx, *accessFamilyID[0]); err != nil {
			return err
		}
	}
	return nil
}

func (s *AuthService) IsBrowserSessionActive(ctx context.Context, id uuid.UUID) (bool, error) {
	return s.sessionRepo.IsBrowserSessionActive(ctx, id)
}

func (s *AuthService) sendVerificationCode(ctx context.Context, user *domain.User) error {
	code, err := s.createEmailActionToken(ctx, user, domain.TokenPurposeVerifyEmail, 15*time.Minute)
	if err != nil {
		return err
	}
	log.Printf("AuthService.sendVerificationCode token created. user_id=%s email=%s expires_in=%s", user.ID, authLogKey(user.Email), 15*time.Minute)

	name := user.Name
	if user.DisplayName != nil && strings.TrimSpace(*user.DisplayName) != "" {
		name = *user.DisplayName
	}
	return s.emailSender.Send(ctx, sharedemail.BuildVerificationEmail(user.Email, name, code, s.appBaseURL))
}

func (s *AuthService) createEmailActionToken(ctx context.Context, user *domain.User, purpose domain.TokenPurpose, ttl time.Duration) (string, error) {
	code, err := generateEmailCode()
	if err != nil {
		return "", err
	}
	if err := s.tokenRepo.InvalidateActive(ctx, user.ID, purpose); err != nil {
		return "", err
	}
	tokenID, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("failed to generate uuid: %w", err)
	}
	token := &domain.EmailActionToken{
		ID:        tokenID,
		UserID:    user.ID,
		Email:     user.Email,
		Purpose:   purpose,
		Code:      code,
		ExpiresAt: time.Now().Add(ttl),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := s.tokenRepo.Create(ctx, token); err != nil {
		return "", err
	}
	log.Printf("AuthService.createEmailActionToken stored token. user_id=%s email=%s purpose=%s expires_at=%s", user.ID, authLogKey(user.Email), purpose, token.ExpiresAt.UTC().Format(time.RFC3339))
	return code, nil
}

func generateEmailCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func timePtr(t time.Time) *time.Time {
	return &t
}
