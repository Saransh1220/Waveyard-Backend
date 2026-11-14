package application

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestResendVerificationBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("missing email", func(t *testing.T) {
		service, _, _, _, _ := newAuthServiceHarness(t)
		require.EqualError(t, service.ResendVerification(ctx, ResendVerificationRequest{}), "email is required")
	})

	t.Run("unknown email is intentionally silent", func(t *testing.T) {
		service, users, _, _, _ := newAuthServiceHarness(t)
		users.On("GetByEmail", ctx, "missing@example.com").Return(nil, domain.ErrUserNotFound).Once()
		require.NoError(t, service.ResendVerification(ctx, ResendVerificationRequest{Email: " missing@example.com "}))
	})

	t.Run("repository error", func(t *testing.T) {
		service, users, _, _, _ := newAuthServiceHarness(t)
		users.On("GetByEmail", ctx, "error@example.com").Return(nil, errors.New("read failed")).Once()
		require.EqualError(t, service.ResendVerification(ctx, ResendVerificationRequest{Email: "error@example.com"}), "read failed")
	})

	t.Run("verified user", func(t *testing.T) {
		service, users, _, _, _ := newAuthServiceHarness(t)
		user := &domain.User{ID: uuid.New(), Email: "verified@example.com", EmailVerified: true}
		users.On("GetByEmail", ctx, user.Email).Return(user, nil).Once()
		require.NoError(t, service.ResendVerification(ctx, ResendVerificationRequest{Email: user.Email}))
	})

	t.Run("unverified user receives a new code", func(t *testing.T) {
		service, users, _, tokens, email := newAuthServiceHarness(t)
		displayName := "Display Name"
		user := &domain.User{ID: uuid.New(), Email: "new@example.com", Name: "Name", DisplayName: &displayName}
		users.On("GetByEmail", ctx, user.Email).Return(user, nil).Once()
		tokens.On("InvalidateActive", ctx, user.ID, domain.TokenPurposeVerifyEmail).Return(nil).Once()
		tokens.On("Create", ctx, mock.MatchedBy(func(token *domain.EmailActionToken) bool {
			return token.UserID == user.ID && token.Purpose == domain.TokenPurposeVerifyEmail
		})).Return(nil).Once()
		email.On("Send", ctx, mock.Anything).Return(nil).Once()
		require.NoError(t, service.ResendVerification(ctx, ResendVerificationRequest{Email: user.Email}))
	})
}

func TestRefreshSessionAndLogoutBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("missing token", func(t *testing.T) {
		service, _, _, _, _ := newAuthServiceHarness(t)
		_, err := service.RefreshSession(ctx, "")
		require.ErrorIs(t, err, domain.ErrSessionNotFound)
		require.NoError(t, service.Logout(ctx, ""))
	})

	t.Run("repository error", func(t *testing.T) {
		service, _, sessions, _, _ := newAuthServiceHarness(t)
		sessions.On("RotateSessionTx", ctx, "error", mock.Anything, mock.Anything, mock.Anything, false).
			Return(false, uuid.Nil, "", "", nil, errors.New("read failed")).Once()
		_, err := service.RefreshSession(ctx, "error")
		require.EqualError(t, err, "read failed")
	})

	t.Run("unknown token", func(t *testing.T) {
		service, _, sessions, _, _ := newAuthServiceHarness(t)
		sessions.On("RotateSessionTx", ctx, "unknown", mock.Anything, mock.Anything, mock.Anything, false).
			Return(false, uuid.Nil, "", "", nil, domain.ErrSessionNotFound).Once()
		_, err := service.RefreshSession(ctx, "unknown")
		require.ErrorIs(t, err, domain.ErrSessionNotFound)
	})

	t.Run("revoked token", func(t *testing.T) {
		service, _, sessions, _, _ := newAuthServiceHarness(t)
		sessions.On("RotateSessionTx", ctx, "revoked", mock.Anything, mock.Anything, mock.Anything, false).
			Return(false, uuid.Nil, "", "", nil, domain.ErrSessionRevoked).Once()
		_, err := service.RefreshSession(ctx, "revoked")
		require.EqualError(t, err, "session has been revoked")
	})

	t.Run("expired token", func(t *testing.T) {
		service, _, sessions, _, _ := newAuthServiceHarness(t)
		sessions.On("RotateSessionTx", ctx, "expired", mock.Anything, mock.Anything, mock.Anything, false).
			Return(false, uuid.Nil, "", "", nil, domain.ErrSessionExpired).Once()
		_, err := service.RefreshSession(ctx, "expired")
		require.ErrorIs(t, err, domain.ErrSessionExpired)
	})

	t.Run("suspended user is refused", func(t *testing.T) {
		service, _, sessions, _, _ := newAuthServiceHarness(t)
		sessions.On("RotateSessionTx", ctx, "banned", mock.Anything, mock.Anything, mock.Anything, false).
			Return(false, uuid.Nil, "", "", nil, domain.ErrAccountSuspended).Once()
		tokens, err := service.RefreshSession(ctx, "banned")
		require.ErrorIs(t, err, ErrAccountSuspended)
		assert.Nil(t, tokens)
	})

	t.Run("success rotates the refresh token, then logout", func(t *testing.T) {
		service, _, sessions, _, _ := newAuthServiceHarness(t)
		userID := uuid.New()
		sessions.On("RotateSessionTx", ctx, "valid", mock.Anything, mock.Anything, mock.Anything, false).
			Return(true, userID, string(domain.RoleProducer), string(domain.SystemRoleUser), nil, nil).Once()
		tokens, err := service.RefreshSession(ctx, "valid")
		require.NoError(t, err)
		assert.NotEmpty(t, tokens.AccessToken)
		assert.NotEmpty(t, tokens.RefreshToken)
		assert.NotEqual(t, "valid", tokens.RefreshToken)

		sessions.On("RevokeBrowserFamilyByToken", ctx, "valid").Return(nil).Once()
		require.NoError(t, service.Logout(ctx, "valid"))
	})
}

func TestRefreshSessionGraceWindow(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	familyID := uuid.New()

	t.Run("recently rotated token gets an access token only in grace window", func(t *testing.T) {
		service, _, sessions, _, _ := newAuthServiceHarness(t)
		sessions.On("RotateSessionTx", ctx, "old", mock.Anything, mock.Anything, mock.Anything, false).
			Return(false, userID, string(domain.RoleArtist), string(domain.SystemRoleUser), nil, nil).Once()
		tokens, err := service.RefreshSession(ctx, "old")
		require.NoError(t, err)
		assert.NotEmpty(t, tokens.AccessToken)
		assert.Empty(t, tokens.RefreshToken, "no new refresh token inside the grace window")
	})

	t.Run("grace window in browser mode preserves family id", func(t *testing.T) {
		service, _, sessions, _, _ := newAuthServiceHarness(t)
		sessions.On("RotateSessionTx", ctx, "old", mock.Anything, mock.Anything, mock.Anything, true).
			Return(false, userID, string(domain.RoleArtist), string(domain.SystemRoleUser), &familyID, nil).Once()
		tokens, err := service.RefreshSession(ctx, "old", true)
		require.NoError(t, err)
		assert.NotEmpty(t, tokens.AccessToken)
		assert.Empty(t, tokens.RefreshToken)
		assert.True(t, tokens.IsBrowser)
		assert.Equal(t, &familyID, tokens.FamilyID)
	})

	t.Run("logout by access family id", func(t *testing.T) {
		service, _, sessions, _, _ := newAuthServiceHarness(t)
		sessions.On("RevokeBrowserSession", ctx, familyID).Return(nil).Once()
		require.NoError(t, service.Logout(ctx, "", &familyID))
	})

	t.Run("is browser session active check", func(t *testing.T) {
		service, _, sessions, _, _ := newAuthServiceHarness(t)
		sessions.On("IsBrowserSessionActive", ctx, familyID).Return(true, nil).Once()
		active, err := service.IsBrowserSessionActive(ctx, familyID)
		require.NoError(t, err)
		assert.True(t, active)
	})
}
