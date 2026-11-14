package auth

import (
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/application"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/infrastructure/persistence/postgres"
	auth_http "github.com/saransh1220/blueprint-audio/internal/modules/auth/interfaces/http"
	fileApp "github.com/saransh1220/blueprint-audio/internal/modules/filestorage/application"
	sharedemail "github.com/saransh1220/blueprint-audio/internal/shared/infrastructure/email"
)

// Module represents the Auth module
type Module struct {
	service     *application.AuthService
	userRepo    *postgres.PgUserRepository
	sessionRepo *postgres.PgSessionRepository
	tokenRepo   *postgres.PgEmailActionTokenRepository
	handler     *auth_http.AuthHandler
}

// NewModule creates and initializes the Auth module
func NewModule(db *sqlx.DB, jwtSecret string, jwtExpiry time.Duration, jwtRefreshExpiry time.Duration, fileService *fileApp.FileService, googleClientID string, secureCookie bool, emailSender sharedemail.Sender, appBaseURL string) (*Module, error) {
	userRepo := postgres.NewUserRepository(db)
	sessionRepo := postgres.NewSessionRepository(db)
	tokenRepo := postgres.NewEmailActionTokenRepository(db)
	service := application.NewAuthService(userRepo, sessionRepo, tokenRepo, emailSender, appBaseURL, jwtSecret, jwtExpiry, jwtRefreshExpiry)
	handler := auth_http.NewAuthHandler(service, fileService, googleClientID, jwtRefreshExpiry, secureCookie, jwtExpiry)

	return &Module{
		service:     service,
		userRepo:    userRepo,
		sessionRepo: sessionRepo,
		tokenRepo:   tokenRepo,
		handler:     handler,
	}, nil
}

// Service returns the auth service for use by the gateway layer
func (m *Module) Service() *application.AuthService {
	return m.service
}

// UserFinder returns the user finder interface for use by other modules
func (m *Module) UserFinder() domain.UserFinder {
	return m.userRepo
}

// UserRepository returns the user repository (for handlers still using old interfaces)
func (m *Module) UserRepository() *postgres.PgUserRepository {
	return m.userRepo
}

// SessionRepository returns the session repository
func (m *Module) SessionRepository() domain.SessionRepository {
	return m.sessionRepo
}

// HTTPHandler returns the HTTP handler for the auth module
func (m *Module) HTTPHandler() *auth_http.AuthHandler {
	return m.handler
}
