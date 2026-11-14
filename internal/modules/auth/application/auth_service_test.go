package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	sharedemail "github.com/saransh1220/blueprint-audio/internal/shared/infrastructure/email"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/api/idtoken"
)

type mockUserRepository struct{ mock.Mock }

func (m *mockUserRepository) Create(ctx context.Context, user *domain.User) error {
	return m.Called(ctx, user).Error(0)
}
func (m *mockUserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	args := m.Called(ctx, email)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}
func (m *mockUserRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}
func (m *mockUserRepository) MarkEmailVerified(ctx context.Context, id uuid.UUID) error {
	return m.Called(ctx, id).Error(0)
}
func (m *mockUserRepository) UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string) error {
	return m.Called(ctx, id, passwordHash).Error(0)
}
func (m *mockUserRepository) UpdateProfile(ctx context.Context, id uuid.UUID, bio *string, avatarUrl *string, bannerURL *string, displayName *string, instagramURL, twitterURL, youtubeURL, spotifyURL *string, storeCurrency *string) error {
	return m.Called(ctx, id, bio, avatarUrl, displayName, instagramURL, twitterURL, youtubeURL, spotifyURL, storeCurrency).Error(0)
}
func (m *mockUserRepository) UpdateSystemRole(ctx context.Context, id uuid.UUID, role domain.SystemRole) error {
	return m.Called(ctx, id, role).Error(0)
}
func (m *mockUserRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.UserStatus) error {
	return m.Called(ctx, id, status).Error(0)
}
func (m *mockUserRepository) CountBySystemRole(ctx context.Context, role domain.SystemRole) (int, error) {
	args := m.Called(ctx, role)
	return args.Int(0), args.Error(1)
}
func (m *mockUserRepository) BootstrapSuperAdmin(ctx context.Context, email string) error {
	return m.Called(ctx, email).Error(0)
}
func (m *mockUserRepository) SetPreferredCurrency(ctx context.Context, id uuid.UUID, currency *string) error {
	return m.Called(ctx, id, currency).Error(0)
}
func (m *mockUserRepository) GetPreferredCurrency(ctx context.Context, id uuid.UUID) (*string, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*string), args.Error(1)
}

type mockSessionRepository struct{ mock.Mock }

func (m *mockSessionRepository) Create(ctx context.Context, session *domain.UserSession) error {
	return m.Called(ctx, session).Error(0)
}
func (m *mockSessionRepository) GetByToken(ctx context.Context, token string) (*domain.UserSession, error) {
	args := m.Called(ctx, token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.UserSession), args.Error(1)
}
func (m *mockSessionRepository) Revoke(ctx context.Context, token string) error {
	return m.Called(ctx, token).Error(0)
}
func (m *mockSessionRepository) RevokeAllForUser(ctx context.Context, userID uuid.UUID) error {
	return m.Called(ctx, userID).Error(0)
}
func (m *mockSessionRepository) MarkRotated(ctx context.Context, token string, successorID uuid.UUID) (bool, error) {
	args := m.Called(ctx, token, successorID)
	return args.Bool(0), args.Error(1)
}
func (m *mockSessionRepository) IsActive(ctx context.Context, id uuid.UUID) (bool, error) {
	args := m.Called(ctx, id)
	return args.Bool(0), args.Error(1)
}
func (m *mockSessionRepository) CreateBrowserSession(ctx context.Context, session *domain.BrowserSession) error {
	return m.Called(ctx, session).Error(0)
}
func (m *mockSessionRepository) GetBrowserSession(ctx context.Context, id uuid.UUID) (*domain.BrowserSession, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.BrowserSession), args.Error(1)
}
func (m *mockSessionRepository) IsBrowserSessionActive(ctx context.Context, id uuid.UUID) (bool, error) {
	args := m.Called(ctx, id)
	return args.Bool(0), args.Error(1)
}
func (m *mockSessionRepository) RevokeBrowserSession(ctx context.Context, id uuid.UUID) error {
	return m.Called(ctx, id).Error(0)
}
func (m *mockSessionRepository) RevokeBrowserFamilyByToken(ctx context.Context, token string) error {
	return m.Called(ctx, token).Error(0)
}
func (m *mockSessionRepository) RotateSessionTx(ctx context.Context, refreshToken string, newSessionID uuid.UUID, newRefreshTokenDigest string, expiresAt time.Time, isBrowserMode bool) (bool, uuid.UUID, string, string, *uuid.UUID, error) {
	args := m.Called(ctx, refreshToken, newSessionID, newRefreshTokenDigest, expiresAt, isBrowserMode)
	var famID *uuid.UUID
	if args.Get(4) != nil {
		famID = args.Get(4).(*uuid.UUID)
	}
	return args.Bool(0), args.Get(1).(uuid.UUID), args.String(2), args.String(3), famID, args.Error(5)
}

type mockTokenRepository struct{ mock.Mock }

func (m *mockTokenRepository) Create(ctx context.Context, token *domain.EmailActionToken) error {
	return m.Called(ctx, token).Error(0)
}
func (m *mockTokenRepository) Consume(ctx context.Context, email string, purpose domain.TokenPurpose, code string) (*domain.EmailActionToken, error) {
	args := m.Called(ctx, email, purpose, code)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.EmailActionToken), args.Error(1)
}
func (m *mockTokenRepository) InvalidateActive(ctx context.Context, userID uuid.UUID, purpose domain.TokenPurpose) error {
	return m.Called(ctx, userID, purpose).Error(0)
}

type mockEmailSender struct{ mock.Mock }

func (m *mockEmailSender) Send(ctx context.Context, msg sharedemail.Message) error {
	return m.Called(ctx, msg).Error(0)
}

func newAuthServiceHarness(t *testing.T) (*AuthService, *mockUserRepository, *mockSessionRepository, *mockTokenRepository, *mockEmailSender) {
	t.Helper()

	repo := new(mockUserRepository)
	sessionRepo := new(mockSessionRepository)
	tokenRepo := new(mockTokenRepository)
	emailSender := new(mockEmailSender)
	t.Cleanup(func() {
		repo.AssertExpectations(t)
		sessionRepo.AssertExpectations(t)
		tokenRepo.AssertExpectations(t)
		emailSender.AssertExpectations(t)
	})

	return NewAuthService(repo, sessionRepo, tokenRepo, emailSender, "http://localhost:4200", "secret", time.Hour, time.Hour*720), repo, sessionRepo, tokenRepo, emailSender
}

func TestRegister_Success(t *testing.T) {
	svc, repo, _, tokenRepo, emailSender := newAuthServiceHarness(t)
	ctx := context.Background()

	req := RegisterRequest{
		Email:       "test@example.com",
		Password:    "password123",
		Name:        "Test User",
		DisplayName: "Test User",
		Role:        "artist",
	}

	repo.On("Create", ctx, mock.AnythingOfType("*domain.User")).Return(nil).Once()
	tokenRepo.On("InvalidateActive", ctx, mock.AnythingOfType("uuid.UUID"), domain.TokenPurposeVerifyEmail).Return(nil).Once()
	tokenRepo.On("Create", ctx, mock.AnythingOfType("*domain.EmailActionToken")).Return(nil).Once()
	emailSender.On("Send", ctx, mock.AnythingOfType("email.Message")).Return(nil).Once()

	user, err := svc.Register(ctx, req)
	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, req.Email, user.Email)
	assert.Equal(t, domain.RoleArtist, user.Role)
	assert.NotZero(t, user.ID)
	assert.Equal(t, uuid.Version(7), user.ID.Version())
	assert.False(t, user.EmailVerified)
}

func TestRegister_InvalidInput(t *testing.T) {
	svc, _, _, _, _ := newAuthServiceHarness(t)
	ctx := context.Background()

	_, err := svc.Register(ctx, RegisterRequest{
		Password:    "password123",
		Name:        "Test",
		DisplayName: "Test",
	})
	assert.EqualError(t, err, "email is required")

	_, err = svc.Register(ctx, RegisterRequest{
		Email:    "test@example.com",
		Password: "password123",
		Name:     "Test",
	})
	assert.EqualError(t, err, "display name is required")

	_, err = svc.Register(ctx, RegisterRequest{
		Email:       "test@example.com",
		Password:    "short",
		Name:        "Test",
		DisplayName: "Test",
	})
	assert.EqualError(t, err, "password must be at least 8 characters")
}

func TestRegister_InvalidRoleAndEmail(t *testing.T) {
	svc, _, _, _, _ := newAuthServiceHarness(t)
	ctx := context.Background()

	_, err := svc.Register(ctx, RegisterRequest{
		Email:       "invalid-email",
		Password:    "password123",
		Name:        "Test",
		DisplayName: "Test",
		Role:        "artist",
	})
	assert.EqualError(t, err, "invalid email format")

	_, err = svc.Register(ctx, RegisterRequest{
		Email:       "test@example.com",
		Password:    "password123",
		Name:        "Test",
		DisplayName: "Test",
		Role:        "invalid_role",
	})
	assert.EqualError(t, err, "invalid role")
}

func TestRegister_RepoError(t *testing.T) {
	svc, repo, _, _, _ := newAuthServiceHarness(t)
	ctx := context.Background()

	req := RegisterRequest{
		Email:       "repoerr@example.com",
		Password:    "password123",
		Name:        "Test",
		DisplayName: "Test",
		Role:        "producer",
	}
	repo.On("Create", ctx, mock.AnythingOfType("*domain.User")).Return(assert.AnError).Once()

	_, err := svc.Register(ctx, req)
	assert.ErrorIs(t, err, assert.AnError)
}

func TestLogin(t *testing.T) {
	ctx := context.Background()

	t.Run("missing fields", func(t *testing.T) {
		svc, _, _, _, _ := newAuthServiceHarness(t)
		_, err := svc.Login(ctx, LoginRequest{})
		assert.EqualError(t, err, "missing email or password")
	})

	t.Run("repo user not found maps to invalid credentials", func(t *testing.T) {
		svc, repo, _, _, _ := newAuthServiceHarness(t)
		repo.On("GetByEmail", ctx, "missing@example.com").Return(nil, domain.ErrUserNotFound).Once()

		_, err := svc.Login(ctx, LoginRequest{Email: "missing@example.com", Password: "password123"})
		assert.ErrorIs(t, err, domain.ErrInvalidCredentials)
	})

	t.Run("wrong password", func(t *testing.T) {
		svc, repo, _, _, _ := newAuthServiceHarness(t)

		hash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.DefaultCost)
		assert.NoError(t, err)
		user := &domain.User{ID: uuid.New(), Email: "a@a.com", PasswordHash: string(hash), Role: domain.RoleArtist, EmailVerified: true}
		repo.On("GetByEmail", ctx, "a@a.com").Return(user, nil).Once()

		_, err = svc.Login(ctx, LoginRequest{Email: "a@a.com", Password: "wrong-password"})
		assert.ErrorIs(t, err, domain.ErrInvalidCredentials)
	})

	t.Run("unverified email", func(t *testing.T) {
		svc, repo, _, _, _ := newAuthServiceHarness(t)
		hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
		assert.NoError(t, err)
		user := &domain.User{ID: uuid.New(), Email: "a@a.com", PasswordHash: string(hash), Role: domain.RoleArtist, EmailVerified: false}
		repo.On("GetByEmail", ctx, "a@a.com").Return(user, nil).Once()

		_, err = svc.Login(ctx, LoginRequest{Email: "a@a.com", Password: "password123"})
		assert.ErrorIs(t, err, domain.ErrEmailNotVerified)
	})

	t.Run("success", func(t *testing.T) {
		svc, repo, sessionRepo, _, _ := newAuthServiceHarness(t)

		hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
		assert.NoError(t, err)
		user := &domain.User{ID: uuid.New(), Email: "a@a.com", PasswordHash: string(hash), Role: domain.RoleProducer, EmailVerified: true}
		repo.On("GetByEmail", ctx, "a@a.com").Return(user, nil).Once()
		sessionRepo.On("Create", mock.Anything, mock.AnythingOfType("*domain.UserSession")).
			Run(func(args mock.Arguments) {
				session := args.Get(1).(*domain.UserSession)
				assert.Equal(t, uuid.Version(7), session.ID.Version())
			}).
			Return(nil).Once()

		token, err := svc.Login(ctx, LoginRequest{Email: "a@a.com", Password: "password123"})
		assert.NoError(t, err)
		assert.NotEmpty(t, token)
	})

	t.Run("repo generic error", func(t *testing.T) {
		svc, repo, _, _, _ := newAuthServiceHarness(t)
		repo.On("GetByEmail", ctx, "x@example.com").Return(nil, errors.New("db down")).Once()

		_, err := svc.Login(ctx, LoginRequest{Email: "x@example.com", Password: "password123"})
		assert.EqualError(t, err, "db down")
	})
}

func TestVerifyEmailAndResetPassword(t *testing.T) {
	ctx := context.Background()

	t.Run("verify email", func(t *testing.T) {
		svc, repo, _, tokenRepo, _ := newAuthServiceHarness(t)
		userID := uuid.New()
		tokenRepo.On("Consume", ctx, "user@example.com", domain.TokenPurposeVerifyEmail, "123456").
			Return(&domain.EmailActionToken{UserID: userID}, nil).Once()
		repo.On("MarkEmailVerified", ctx, userID).Return(nil).Once()

		err := svc.VerifyEmail(ctx, VerifyEmailRequest{Email: "user@example.com", Code: "123456"})
		assert.NoError(t, err)
	})

	t.Run("forgot password unknown user is safe", func(t *testing.T) {
		svc, repo, _, _, _ := newAuthServiceHarness(t)
		repo.On("GetByEmail", ctx, "missing@example.com").Return(nil, domain.ErrUserNotFound).Once()
		err := svc.ForgotPassword(ctx, ForgotPasswordRequest{Email: "missing@example.com"})
		assert.NoError(t, err)
	})

	t.Run("reset password", func(t *testing.T) {
		svc, repo, sessionRepo, tokenRepo, _ := newAuthServiceHarness(t)
		userID := uuid.New()
		tokenRepo.On("Consume", ctx, "user@example.com", domain.TokenPurposeResetPassword, "654321").
			Return(&domain.EmailActionToken{UserID: userID}, nil).Once()
		repo.On("UpdatePassword", ctx, userID, mock.AnythingOfType("string")).Return(nil).Once()
		sessionRepo.On("RevokeAllForUser", ctx, userID).Return(nil).Once()

		err := svc.ResetPassword(ctx, ResetPasswordRequest{Email: "user@example.com", Code: "654321", NewPassword: "newpassword123"})
		assert.NoError(t, err)
	})
}

func TestGetUser(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, _, _ := newAuthServiceHarness(t)
	id := uuid.New()
	expected := &domain.User{ID: id}
	repo.On("GetByID", ctx, id).Return(expected, nil).Once()

	user, err := svc.GetUser(ctx, id)
	assert.NoError(t, err)
	assert.Equal(t, expected, user)
}

func TestValidateToken_Invalid(t *testing.T) {
	svc, _, _, _, _ := newAuthServiceHarness(t)

	claims, err := svc.ValidateToken("invalid-token")
	assert.Error(t, err)
	assert.Nil(t, claims)
}

func TestGoogleLogin_InvalidToken(t *testing.T) {
	svc, _, _, _, _ := newAuthServiceHarness(t)
	ctx := context.Background()
	svc.googleTokenValidator = func(ctx context.Context, token string, audience string) (*idtoken.Payload, error) {
		return nil, errors.New("invalid google token")
	}

	_, err := svc.GoogleLogin(ctx, "fake-google-client-id", GoogleLoginRequest{Token: "not-a-google-token"})
	assert.EqualError(t, err, "invalid google token")
	assert.ErrorIs(t, err, ErrGoogleAuthFailed)
}

func TestGoogleLogin_MissingEmail(t *testing.T) {
	svc, _, _, _, _ := newAuthServiceHarness(t)
	svc.googleTokenValidator = func(ctx context.Context, token string, audience string) (*idtoken.Payload, error) {
		return &idtoken.Payload{Claims: map[string]interface{}{"name": "Tester", "email_verified": true}}, nil
	}

	_, err := svc.GoogleLogin(context.Background(), "google-client", GoogleLoginRequest{Token: "token"})
	assert.EqualError(t, err, "email not provided by google")
	assert.ErrorIs(t, err, ErrGoogleAuthFailed)
}

func TestGoogleLogin_RepoError(t *testing.T) {
	svc, repo, _, _, _ := newAuthServiceHarness(t)
	svc.googleTokenValidator = func(ctx context.Context, token string, audience string) (*idtoken.Payload, error) {
		return &idtoken.Payload{Claims: map[string]interface{}{"email": "user@example.com", "name": "Tester", "email_verified": true}}, nil
	}

	repo.On("GetByEmail", mock.Anything, "user@example.com").Return(nil, errors.New("repo down")).Once()

	_, err := svc.GoogleLogin(context.Background(), "google-client", GoogleLoginRequest{Token: "token"})
	assert.EqualError(t, err, "repo down")
}

func TestGoogleLogin_CreateUserError(t *testing.T) {
	svc, repo, _, _, _ := newAuthServiceHarness(t)
	svc.googleTokenValidator = func(ctx context.Context, token string, audience string) (*idtoken.Payload, error) {
		return &idtoken.Payload{Claims: map[string]interface{}{
			"email":          "new@example.com",
			"name":           "New User",
			"picture":        "https://img.example.com/a.jpg",
			"email_verified": true,
		}}, nil
	}

	repo.On("GetByEmail", mock.Anything, "new@example.com").Return(nil, domain.ErrUserNotFound).Once()
	repo.On("Create", mock.Anything, mock.AnythingOfType("*domain.User")).
		Run(func(args mock.Arguments) {
			user := args.Get(1).(*domain.User)
			assert.Equal(t, uuid.Version(7), user.ID.Version())
			assert.True(t, user.EmailVerified)
		}).
		Return(errors.New("create failed")).Once()

	_, err := svc.GoogleLogin(context.Background(), "google-client", GoogleLoginRequest{Token: "token"})
	assert.EqualError(t, err, "create failed")
}

func TestGoogleLogin_CreateUserSuccess(t *testing.T) {
	svc, repo, sessionRepo, _, _ := newAuthServiceHarness(t)
	sessionRepo.On("Create", mock.Anything, mock.AnythingOfType("*domain.UserSession")).
		Run(func(args mock.Arguments) {
			session := args.Get(1).(*domain.UserSession)
			assert.Equal(t, uuid.Version(7), session.ID.Version())
		}).
		Return(nil)
	svc.googleTokenValidator = func(ctx context.Context, token string, audience string) (*idtoken.Payload, error) {
		return &idtoken.Payload{Claims: map[string]interface{}{
			"email":          "new2@example.com",
			"name":           "New User2",
			"picture":        "https://img.example.com/b.jpg",
			"email_verified": true,
		}}, nil
	}

	repo.On("GetByEmail", mock.Anything, "new2@example.com").Return(nil, domain.ErrUserNotFound).Once()
	repo.On("Create", mock.Anything, mock.AnythingOfType("*domain.User")).Return(nil).Once()

	token, err := svc.GoogleLogin(context.Background(), "google-client", GoogleLoginRequest{Token: "token"})
	assert.NoError(t, err)
	assert.NotEmpty(t, token)
}

func TestGoogleLogin_ExistingUserSuccess(t *testing.T) {
	svc, repo, sessionRepo, _, _ := newAuthServiceHarness(t)
	sessionRepo.On("Create", mock.Anything, mock.AnythingOfType("*domain.UserSession")).
		Run(func(args mock.Arguments) {
			session := args.Get(1).(*domain.UserSession)
			assert.Equal(t, uuid.Version(7), session.ID.Version())
		}).
		Return(nil)
	svc.googleTokenValidator = func(ctx context.Context, token string, audience string) (*idtoken.Payload, error) {
		return &idtoken.Payload{Claims: map[string]interface{}{
			"email":          "existing@example.com",
			"name":           "Existing",
			"email_verified": true,
		}}, nil
	}

	existing := &domain.User{ID: uuid.New(), Email: "existing@example.com", Role: domain.RoleProducer, EmailVerified: true}
	repo.On("GetByEmail", mock.Anything, "existing@example.com").Return(existing, nil).Once()

	token, err := svc.GoogleLogin(context.Background(), "google-client", GoogleLoginRequest{Token: "token"})
	assert.NoError(t, err)
	assert.NotEmpty(t, token)
}

func TestGoogleLogin_RequiresConfiguredClientID(t *testing.T) {
	svc, _, _, _, _ := newAuthServiceHarness(t)

	_, err := svc.GoogleLogin(context.Background(), "   ", GoogleLoginRequest{Token: "token"})
	assert.ErrorIs(t, err, ErrGoogleClientIDNotConfigured)
}

func TestGoogleLogin_RequiresTokenAndVerifiedEmail(t *testing.T) {
	t.Run("blank token", func(t *testing.T) {
		svc, _, _, _, _ := newAuthServiceHarness(t)

		_, err := svc.GoogleLogin(context.Background(), "google-client", GoogleLoginRequest{Token: " "})
		assert.EqualError(t, err, "google token is required")
		assert.ErrorIs(t, err, ErrGoogleAuthFailed)
	})

	t.Run("unverified email", func(t *testing.T) {
		svc, _, _, _, _ := newAuthServiceHarness(t)
		svc.googleTokenValidator = func(ctx context.Context, token string, audience string) (*idtoken.Payload, error) {
			return &idtoken.Payload{Claims: map[string]interface{}{
				"email":          "user@example.com",
				"name":           "Tester",
				"email_verified": false,
			}}, nil
		}

		_, err := svc.GoogleLogin(context.Background(), "google-client", GoogleLoginRequest{Token: "token"})
		assert.EqualError(t, err, "google email is not verified")
		assert.ErrorIs(t, err, ErrGoogleAuthFailed)
	})
}
