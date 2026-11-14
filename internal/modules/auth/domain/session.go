package domain

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrSessionNotFound     = errors.New("session not found")
	ErrSessionExpired      = errors.New("session expired")
	ErrSessionRevoked      = errors.New("session has been revoked")
	ErrSessionConflict     = errors.New("conflicting session credentials")
	ErrSessionModeMismatch = errors.New("session mode mismatch")
	ErrAccountSuspended    = errors.New("account suspended")
)

// BrowserSession represents a browser session family
type BrowserSession struct {
	ID        uuid.UUID  `json:"id" db:"id"`
	UserID    uuid.UUID  `json:"user_id" db:"user_id"`
	RevokedAt *time.Time `json:"revoked_at" db:"revoked_at"`
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt time.Time  `json:"updated_at" db:"updated_at"`
}

// UserSession represents a user's refresh token session
type UserSession struct {
	ID                 uuid.UUID `json:"id" db:"id"`
	UserID             uuid.UUID `json:"user_id" db:"user_id"`
	RefreshToken       string    `json:"-" db:"-"`
	RefreshTokenDigest string    `json:"-" db:"refresh_token_digest"`
	IsRevoked          bool      `json:"is_revoked" db:"is_revoked"`
	ExpiresAt          time.Time `json:"expires_at" db:"expires_at"`
	CreatedAt          time.Time `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time `json:"updated_at" db:"updated_at"`
	// RotatedAt and ReplacedBy are set only when the session was revoked because
	// it was exchanged for a new one. Logout, password reset and suspension leave
	// them nil, which is what keeps the refresh grace window from reviving them.
	RotatedAt        *time.Time `json:"-" db:"rotated_at"`
	ReplacedBy       *uuid.UUID `json:"-" db:"replaced_by"`
	BrowserSessionID *uuid.UUID `json:"-" db:"browser_session_id"`
}

// SessionRepository defines the contract for session data access
type SessionRepository interface {
	Create(ctx context.Context, session *UserSession) error
	CreateAuthenticated(ctx context.Context, session *UserSession, passwordHash *string) error
	ResetPasswordAndRevokeSessions(ctx context.Context, userID uuid.UUID, passwordHash string) error
	GetByToken(ctx context.Context, token string) (*UserSession, error)
	Revoke(ctx context.Context, token string) error
	RevokeAllForUser(ctx context.Context, userID uuid.UUID) error
	// MarkRotated revokes the session behind token and records its successor. It
	// reports false when the session was already revoked, so exactly one of
	// several concurrent refreshes wins.
	MarkRotated(ctx context.Context, token string, successorID uuid.UUID) (bool, error)
	// IsActive reports whether the session exists, is not revoked and has not expired.
	IsActive(ctx context.Context, id uuid.UUID) (bool, error)

	// Browser session family operations
	CreateBrowserSession(ctx context.Context, session *BrowserSession) error
	GetBrowserSession(ctx context.Context, id uuid.UUID) (*BrowserSession, error)
	IsBrowserSessionActive(ctx context.Context, id uuid.UUID) (bool, error)
	RevokeBrowserSession(ctx context.Context, id uuid.UUID) error
	RevokeBrowserFamilyByToken(ctx context.Context, token string, expectedFamily ...*uuid.UUID) error

	// Transactional rotation and upgrade
	// RotateSessionTx performs atomic rotation or grace handling with consistent lock order.
	// Returns (rotated, userID, role, systemRole, familyID, err).
	RotateSessionTx(ctx context.Context, refreshToken string, newSessionID uuid.UUID, newRefreshTokenDigest string, expiresAt time.Time, isBrowserMode bool) (rotated bool, userID uuid.UUID, role string, systemRole string, familyID *uuid.UUID, err error)
}
