package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/infrastructure/jwt"
	sharedemail "github.com/saransh1220/blueprint-audio/internal/shared/infrastructure/email"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestIsGoogleEmailVerified(t *testing.T) {
	cases := []struct {
		name  string
		claim any
		want  bool
	}{
		{"bool true", true, true},
		{"bool false", false, false},
		{"string true", "true", true},
		{"string true padded and upper-case", "  TRUE ", true},
		{"string false", "false", false},
		{"nil", nil, false},
		{"number", 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isGoogleEmailVerified(tc.claim))
		})
	}
}

func TestAuthLogKey(t *testing.T) {
	assert.Equal(t, "unknown", authLogKey(""))
	assert.Equal(t, "unknown", authLogKey("   "))

	key := authLogKey("User@Example.com")
	assert.Equal(t, key, authLogKey("  user@example.com "), "key is case and whitespace insensitive")
	assert.NotEqual(t, key, authLogKey("other@example.com"))
	assert.Len(t, key, 12, "6 bytes of the digest, hex encoded")
	assert.NotContains(t, key, "example", "the log key must not leak the address")
}

func TestNewAuthServiceDefaultsEmailSender(t *testing.T) {
	svc := NewAuthService(new(mockUserRepository), new(mockSessionRepository), new(mockTokenRepository), nil, "http://app", "secret", time.Hour, time.Hour)
	require.NotNil(t, svc.emailSender, "a nil sender is replaced by the disabled sender")
	require.NotNil(t, svc.googleTokenValidator)
}

func TestVerifyEmailBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("blank input", func(t *testing.T) {
		svc, _, _, _, _ := newAuthServiceHarness(t)
		assert.EqualError(t, svc.VerifyEmail(ctx, VerifyEmailRequest{Email: " ", Code: "123456"}), "email and code are required")
		assert.EqualError(t, svc.VerifyEmail(ctx, VerifyEmailRequest{Email: "a@a.com", Code: " "}), "email and code are required")
	})

	t.Run("invalid code", func(t *testing.T) {
		svc, _, _, tokens, _ := newAuthServiceHarness(t)
		tokens.On("Consume", ctx, "a@a.com", domain.TokenPurposeVerifyEmail, "111111").Return(nil, domain.ErrInvalidOrExpiredCode).Once()
		err := svc.VerifyEmail(ctx, VerifyEmailRequest{Email: "a@a.com", Code: "111111"})
		assert.ErrorIs(t, err, domain.ErrInvalidOrExpiredCode)
	})

	t.Run("code is trimmed before consuming", func(t *testing.T) {
		svc, users, _, tokens, _ := newAuthServiceHarness(t)
		userID := uuid.New()
		tokens.On("Consume", ctx, "a@a.com", domain.TokenPurposeVerifyEmail, "123456").Return(&domain.EmailActionToken{UserID: userID}, nil).Once()
		users.On("MarkEmailVerified", ctx, userID).Return(errors.New("write failed")).Once()
		assert.EqualError(t, svc.VerifyEmail(ctx, VerifyEmailRequest{Email: "a@a.com", Code: " 123456 "}), "write failed")
	})
}

func TestForgotPasswordBranches(t *testing.T) {
	ctx := context.Background()
	displayName := "Display"
	user := &domain.User{ID: uuid.New(), Email: "a@a.com", Name: "Real Name", DisplayName: &displayName}

	t.Run("blank email", func(t *testing.T) {
		svc, _, _, _, _ := newAuthServiceHarness(t)
		assert.EqualError(t, svc.ForgotPassword(ctx, ForgotPasswordRequest{Email: "  "}), "email is required")
	})

	t.Run("repository error", func(t *testing.T) {
		svc, users, _, _, _ := newAuthServiceHarness(t)
		users.On("GetByEmail", ctx, "a@a.com").Return(nil, errors.New("read failed")).Once()
		assert.EqualError(t, svc.ForgotPassword(ctx, ForgotPasswordRequest{Email: "a@a.com"}), "read failed")
	})

	t.Run("invalidate error stops before storing a code", func(t *testing.T) {
		svc, users, _, tokens, _ := newAuthServiceHarness(t)
		users.On("GetByEmail", ctx, "a@a.com").Return(user, nil).Once()
		tokens.On("InvalidateActive", ctx, user.ID, domain.TokenPurposeResetPassword).Return(errors.New("invalidate failed")).Once()
		assert.EqualError(t, svc.ForgotPassword(ctx, ForgotPasswordRequest{Email: "a@a.com"}), "invalidate failed")
	})

	t.Run("store error", func(t *testing.T) {
		svc, users, _, tokens, _ := newAuthServiceHarness(t)
		users.On("GetByEmail", ctx, "a@a.com").Return(user, nil).Once()
		tokens.On("InvalidateActive", ctx, user.ID, domain.TokenPurposeResetPassword).Return(nil).Once()
		tokens.On("Create", ctx, mock.Anything).Return(errors.New("insert failed")).Once()
		assert.EqualError(t, svc.ForgotPassword(ctx, ForgotPasswordRequest{Email: "a@a.com"}), "insert failed")
	})

	t.Run("sends the code with a 15 minute expiry", func(t *testing.T) {
		svc, users, _, tokens, email := newAuthServiceHarness(t)
		users.On("GetByEmail", ctx, "a@a.com").Return(user, nil).Once()
		tokens.On("InvalidateActive", ctx, user.ID, domain.TokenPurposeResetPassword).Return(nil).Once()
		var stored *domain.EmailActionToken
		tokens.On("Create", ctx, mock.Anything).Run(func(args mock.Arguments) {
			stored = args.Get(1).(*domain.EmailActionToken)
		}).Return(nil).Once()
		var sent sharedemail.Message
		email.On("Send", ctx, mock.Anything).Run(func(args mock.Arguments) {
			sent = args.Get(1).(sharedemail.Message)
		}).Return(nil).Once()

		require.NoError(t, svc.ForgotPassword(ctx, ForgotPasswordRequest{Email: " a@a.com "}))

		require.NotNil(t, stored)
		assert.Equal(t, domain.TokenPurposeResetPassword, stored.Purpose)
		assert.Len(t, stored.Code, 6)
		assert.WithinDuration(t, time.Now().Add(15*time.Minute), stored.ExpiresAt, 5*time.Second)
		assert.Equal(t, []string{"a@a.com"}, sent.To)
		assert.Contains(t, sent.Text+sent.HTML, stored.Code, "the emailed code is the stored code")
	})

	t.Run("send failure is returned", func(t *testing.T) {
		svc, users, _, tokens, email := newAuthServiceHarness(t)
		blank := " "
		noName := &domain.User{ID: uuid.New(), Email: "b@b.com", Name: "Fallback", DisplayName: &blank}
		users.On("GetByEmail", ctx, "b@b.com").Return(noName, nil).Once()
		tokens.On("InvalidateActive", ctx, noName.ID, domain.TokenPurposeResetPassword).Return(nil).Once()
		tokens.On("Create", ctx, mock.Anything).Return(nil).Once()
		email.On("Send", ctx, mock.Anything).Return(errors.New("smtp down")).Once()
		assert.EqualError(t, svc.ForgotPassword(ctx, ForgotPasswordRequest{Email: "b@b.com"}), "smtp down")
	})
}

func TestResetPasswordBranches(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	valid := ResetPasswordRequest{Email: "a@a.com", Code: "654321", NewPassword: "longenough1"}

	t.Run("missing fields", func(t *testing.T) {
		svc, _, _, _, _ := newAuthServiceHarness(t)
		for _, req := range []ResetPasswordRequest{
			{Code: "1", NewPassword: "longenough1"},
			{Email: "a@a.com", NewPassword: "longenough1"},
			{Email: "a@a.com", Code: "1", NewPassword: "   "},
		} {
			assert.EqualError(t, svc.ResetPassword(ctx, req), "email, code and new password are required")
		}
	})

	t.Run("short password", func(t *testing.T) {
		svc, _, _, _, _ := newAuthServiceHarness(t)
		assert.EqualError(t, svc.ResetPassword(ctx, ResetPasswordRequest{Email: "a@a.com", Code: "1", NewPassword: "short"}), "password must be at least 8 characters")
	})

	t.Run("invalid code leaves sessions alone", func(t *testing.T) {
		svc, _, _, tokens, _ := newAuthServiceHarness(t)
		tokens.On("Consume", ctx, "a@a.com", domain.TokenPurposeResetPassword, "654321").Return(nil, domain.ErrInvalidOrExpiredCode).Once()
		assert.ErrorIs(t, svc.ResetPassword(ctx, valid), domain.ErrInvalidOrExpiredCode)
	})

	t.Run("transaction failure is returned", func(t *testing.T) {
		svc, _, sessions, tokens, _ := newAuthServiceHarness(t)
		tokens.On("Consume", ctx, "a@a.com", domain.TokenPurposeResetPassword, "654321").Return(&domain.EmailActionToken{UserID: userID}, nil).Once()
		sessions.On("ResetPasswordAndRevokeSessions", ctx, userID, mock.Anything).Return(errors.New("write failed")).Once()
		assert.EqualError(t, svc.ResetPassword(ctx, valid), "write failed")
	})

	t.Run("stores a bcrypt hash of the new password", func(t *testing.T) {
		svc, _, sessions, tokens, _ := newAuthServiceHarness(t)
		tokens.On("Consume", ctx, "a@a.com", domain.TokenPurposeResetPassword, "654321").Return(&domain.EmailActionToken{UserID: userID}, nil).Once()
		sessions.On("ResetPasswordAndRevokeSessions", ctx, userID, mock.MatchedBy(func(hash string) bool {
			return bcrypt.CompareHashAndPassword([]byte(hash), []byte("longenough1")) == nil
		})).Return(nil).Once()
		require.NoError(t, svc.ResetPassword(ctx, valid))
	})
}

func TestRegisterAndLoginAdditionalBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("register survives a failed verification email", func(t *testing.T) {
		svc, users, _, tokens, email := newAuthServiceHarness(t)
		users.On("Create", ctx, mock.Anything).Return(nil).Once()
		tokens.On("InvalidateActive", ctx, mock.Anything, domain.TokenPurposeVerifyEmail).Return(nil).Once()
		tokens.On("Create", ctx, mock.Anything).Return(nil).Once()
		email.On("Send", ctx, mock.Anything).Return(errors.New("smtp down")).Once()

		user, err := svc.Register(ctx, RegisterRequest{Email: "new@example.com", Password: "password123", Name: "New", DisplayName: "New", Role: "artist"})
		require.NoError(t, err)
		assert.False(t, user.EmailVerified)
		assert.Equal(t, "waveyard", user.RegistrationSource, "empty source defaults to waveyard")
	})

	t.Run("register keeps an explicit source", func(t *testing.T) {
		svc, users, _, tokens, email := newAuthServiceHarness(t)
		users.On("Create", ctx, mock.Anything).Return(nil).Once()
		tokens.On("InvalidateActive", ctx, mock.Anything, domain.TokenPurposeVerifyEmail).Return(nil).Once()
		tokens.On("Create", ctx, mock.Anything).Return(nil).Once()
		email.On("Send", ctx, mock.Anything).Return(nil).Once()

		user, err := svc.Register(ctx, RegisterRequest{Email: "new@example.com", Password: "password123", Name: "New", DisplayName: "New", Role: "producer", RegistrationSource: "sonically"})
		require.NoError(t, err)
		assert.Equal(t, "sonically", user.RegistrationSource)
	})

	t.Run("login surfaces repository errors", func(t *testing.T) {
		svc, users, _, _, _ := newAuthServiceHarness(t)
		users.On("GetByEmail", ctx, "a@a.com").Return(nil, errors.New("db down")).Once()
		_, err := svc.Login(ctx, LoginRequest{Email: "a@a.com", Password: "password123"})
		assert.EqualError(t, err, "db down")
	})

	t.Run("login rejects suspended accounts", func(t *testing.T) {
		svc, users, _, _, _ := newAuthServiceHarness(t)
		hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
		require.NoError(t, err)
		users.On("GetByEmail", ctx, "a@a.com").Return(&domain.User{ID: uuid.New(), PasswordHash: string(hash), EmailVerified: true, Status: domain.UserStatusSuspended}, nil).Once()
		_, err = svc.Login(ctx, LoginRequest{Email: "a@a.com", Password: "password123"})
		assert.ErrorIs(t, err, ErrAccountSuspended)
	})

	t.Run("login fails when the session cannot be stored", func(t *testing.T) {
		svc, users, sessions, _, _ := newAuthServiceHarness(t)
		hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
		require.NoError(t, err)
		users.On("GetByEmail", ctx, "a@a.com").Return(&domain.User{ID: uuid.New(), PasswordHash: string(hash), EmailVerified: true, Status: domain.UserStatusActive}, nil).Once()
		sessions.On("CreateAuthenticated", ctx, mock.Anything, mock.Anything).Return(errors.New("insert failed")).Once()
		_, err = svc.Login(ctx, LoginRequest{Email: "a@a.com", Password: "password123"})
		assert.EqualError(t, err, "insert failed")
	})
}

func TestRefreshSessionRotationFailures(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	familyID := uuid.New()

	t.Run("rotation db write failure returns error", func(t *testing.T) {
		svc, _, sessions, _, _ := newAuthServiceHarness(t)
		sessions.On("RotateSessionTx", ctx, "old", mock.Anything, mock.Anything, mock.Anything, false).
			Return(false, uuid.Nil, "", "", nil, errors.New("write failed")).Once()
		_, err := svc.RefreshSession(ctx, "old")
		require.Error(t, err)
		assert.EqualError(t, err, "write failed")
	})

	t.Run("session not found", func(t *testing.T) {
		svc, _, sessions, _, _ := newAuthServiceHarness(t)
		sessions.On("RotateSessionTx", ctx, "old", mock.Anything, mock.Anything, mock.Anything, false).
			Return(false, uuid.Nil, "", "", nil, domain.ErrSessionNotFound).Once()
		_, err := svc.RefreshSession(ctx, "old")
		assert.ErrorIs(t, err, domain.ErrSessionNotFound)
	})

	t.Run("session expired", func(t *testing.T) {
		svc, _, sessions, _, _ := newAuthServiceHarness(t)
		sessions.On("RotateSessionTx", ctx, "old", mock.Anything, mock.Anything, mock.Anything, false).
			Return(false, uuid.Nil, "", "", nil, domain.ErrSessionExpired).Once()
		_, err := svc.RefreshSession(ctx, "old")
		assert.ErrorIs(t, err, domain.ErrSessionExpired)
	})

	t.Run("session revoked", func(t *testing.T) {
		svc, _, sessions, _, _ := newAuthServiceHarness(t)
		sessions.On("RotateSessionTx", ctx, "old", mock.Anything, mock.Anything, mock.Anything, false).
			Return(false, uuid.Nil, "", "", nil, domain.ErrSessionRevoked).Once()
		_, err := svc.RefreshSession(ctx, "old")
		assert.EqualError(t, err, "session has been revoked")
	})

	t.Run("suspended user", func(t *testing.T) {
		svc, _, sessions, _, _ := newAuthServiceHarness(t)
		sessions.On("RotateSessionTx", ctx, "old", mock.Anything, mock.Anything, mock.Anything, false).
			Return(false, uuid.Nil, "", "", nil, domain.ErrAccountSuspended).Once()
		_, err := svc.RefreshSession(ctx, "old")
		assert.ErrorIs(t, err, ErrAccountSuspended)
	})

	t.Run("mode mismatch: legacy refresh on cookie-mode session", func(t *testing.T) {
		svc, _, sessions, _, _ := newAuthServiceHarness(t)
		sessions.On("RotateSessionTx", ctx, "old", mock.Anything, mock.Anything, mock.Anything, false).
			Return(false, uuid.Nil, "", "", nil, domain.ErrSessionModeMismatch).Once()
		_, err := svc.RefreshSession(ctx, "old")
		assert.ErrorIs(t, err, domain.ErrSessionModeMismatch)
	})

	t.Run("grace refresh issues access token only", func(t *testing.T) {
		svc, _, sessions, _, _ := newAuthServiceHarness(t)
		sessions.On("RotateSessionTx", ctx, "old", mock.Anything, mock.Anything, mock.Anything, false).
			Return(false, userID, string(domain.RoleArtist), string(domain.SystemRoleUser), nil, nil).Once()
		tokens, err := svc.RefreshSession(ctx, "old")
		require.NoError(t, err)
		assert.NotEmpty(t, tokens.AccessToken)
		assert.Empty(t, tokens.RefreshToken)
		claims, err := svc.ValidateToken(tokens.AccessToken)
		require.NoError(t, err)
		assert.Equal(t, userID, claims.UserID)
		assert.Equal(t, string(domain.RoleArtist), claims.Role)
	})

	t.Run("browser mode refresh issues browser token with sid", func(t *testing.T) {
		svc, _, sessions, _, _ := newAuthServiceHarness(t)
		sessions.On("RotateSessionTx", ctx, "old", mock.Anything, mock.Anything, mock.Anything, true).
			Return(true, userID, string(domain.RoleProducer), string(domain.SystemRoleUser), &familyID, nil).Once()
		tokens, err := svc.RefreshSession(ctx, "old", true)
		require.NoError(t, err)
		assert.NotEmpty(t, tokens.AccessToken)
		assert.NotEmpty(t, tokens.RefreshToken)
		assert.True(t, tokens.IsBrowser)
		assert.Equal(t, &familyID, tokens.FamilyID)
		claims, err := svc.ValidateToken(tokens.AccessToken)
		require.NoError(t, err)
		assert.Equal(t, userID, claims.UserID)
		assert.Equal(t, string(domain.RoleProducer), claims.Role)
		assert.Equal(t, jwt.PurposeBrowser, claims.Purpose)
		assert.Equal(t, &familyID, claims.SessionID)
	})
}

func TestSendVerificationCodeUsesDisplayNameFallback(t *testing.T) {
	ctx := context.Background()
	svc, users, _, tokens, email := newAuthServiceHarness(t)
	user := &domain.User{ID: uuid.New(), Email: "n@n.com", Name: "Plain Name"}
	users.On("GetByEmail", ctx, "n@n.com").Return(user, nil).Once()
	tokens.On("InvalidateActive", ctx, user.ID, domain.TokenPurposeVerifyEmail).Return(nil).Once()
	tokens.On("Create", ctx, mock.Anything).Return(nil).Once()
	email.On("Send", ctx, mock.Anything).Return(errors.New("smtp down")).Once()

	assert.EqualError(t, svc.ResendVerification(ctx, ResendVerificationRequest{Email: "n@n.com"}), "smtp down")
}
