package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
)

const refreshGraceWindow = 10 * time.Second

type PgSessionRepository struct {
	db *sqlx.DB
}

func NewSessionRepository(db *sqlx.DB) *PgSessionRepository {
	return &PgSessionRepository{db: db}
}

func digestRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (r *PgSessionRepository) Create(ctx context.Context, session *domain.UserSession) error {
	return insertSession(ctx, r.db, session)
}

func insertSession(ctx context.Context, db sqlx.ExtContext, session *domain.UserSession) error {
	query := `INSERT INTO user_sessions (id, user_id, refresh_token_digest, is_revoked, expires_at, created_at, updated_at, browser_session_id)
			  VALUES (:id, :user_id, :refresh_token_digest, :is_revoked, :expires_at, :created_at, :updated_at, :browser_session_id)`

	if session.ID == uuid.Nil {
		sessionID, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("failed to generate uuid: %w", err)
		}
		session.ID = sessionID
	}

	if session.CreatedAt.IsZero() {
		session.CreatedAt = time.Now()
	}
	if session.UpdatedAt.IsZero() {
		session.UpdatedAt = time.Now()
	}

	persistedSession := *session
	persistedSession.RefreshTokenDigest = digestRefreshToken(session.RefreshToken)
	persistedSession.RefreshToken = ""
	session.RefreshTokenDigest = persistedSession.RefreshTokenDigest

	_, err := sqlx.NamedExecContext(ctx, db, query, &persistedSession)
	return err
}

func (r *PgSessionRepository) CreateAuthenticated(ctx context.Context, session *domain.UserSession, passwordHash *string) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var user struct {
		Status       domain.UserStatus `db:"status"`
		PasswordHash sql.NullString    `db:"password_hash"`
	}
	if err := tx.GetContext(ctx, &user, "SELECT status, password_hash FROM users WHERE id = $1 FOR UPDATE", session.UserID); err != nil {
		if err == sql.ErrNoRows {
			return domain.ErrInvalidCredentials
		}
		return err
	}
	if user.Status == domain.UserStatusSuspended {
		return domain.ErrAccountSuspended
	}
	if passwordHash != nil && user.PasswordHash.String != *passwordHash {
		return domain.ErrInvalidCredentials
	}

	if session.BrowserSessionID != nil {
		if _, err := tx.ExecContext(ctx, "INSERT INTO browser_sessions (id, user_id) VALUES ($1, $2)", *session.BrowserSessionID, session.UserID); err != nil {
			return err
		}
	}
	if err := insertSession(ctx, tx, session); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *PgSessionRepository) ResetPasswordAndRevokeSessions(ctx context.Context, userID uuid.UUID, passwordHash string) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, "UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2", passwordHash, userID)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil {
		return err
	} else if rows == 0 {
		return domain.ErrUserNotFound
	}
	if err := revokeUserSessions(ctx, tx, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *PgSessionRepository) GetByToken(ctx context.Context, token string) (*domain.UserSession, error) {
	session := &domain.UserSession{}
	query := `SELECT id, user_id, refresh_token_digest, is_revoked, expires_at, created_at, updated_at, rotated_at, replaced_by, browser_session_id
	          FROM user_sessions WHERE refresh_token_digest = $1`

	err := r.db.GetContext(ctx, session, query, digestRefreshToken(token))
	if err == sql.ErrNoRows {
		return nil, nil // Return nil, nil when not found to easily distinguish from db errors
	}
	if err != nil {
		return nil, err
	}
	return session, nil
}

func (r *PgSessionRepository) Revoke(ctx context.Context, token string) error {
	query := `UPDATE user_sessions SET is_revoked = true, updated_at = $1 WHERE refresh_token_digest = $2`
	_, err := r.db.ExecContext(ctx, query, time.Now(), digestRefreshToken(token))
	return err
}

func (r *PgSessionRepository) MarkRotated(ctx context.Context, token string, successorID uuid.UUID) (bool, error) {
	now := time.Now()
	query := `UPDATE user_sessions SET is_revoked = true, rotated_at = $1, replaced_by = $2, updated_at = $3
			  WHERE refresh_token_digest = $4 AND is_revoked = false`
	res, err := r.db.ExecContext(ctx, query, now, successorID, now, digestRefreshToken(token))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (r *PgSessionRepository) IsActive(ctx context.Context, id uuid.UUID) (bool, error) {
	var active bool
	query := `SELECT EXISTS(SELECT 1 FROM user_sessions WHERE id = $1 AND is_revoked = false AND expires_at > $2)`
	if err := r.db.GetContext(ctx, &active, query, id, time.Now()); err != nil {
		return false, err
	}
	return active, nil
}

func (r *PgSessionRepository) RevokeAllForUser(ctx context.Context, userID uuid.UUID) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockSessionUser(ctx, tx, userID); err != nil {
		return err
	}
	if err := revokeUserSessions(ctx, tx, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func lockSessionUser(ctx context.Context, tx *sqlx.Tx, userID uuid.UUID) error {
	var id uuid.UUID
	return tx.GetContext(ctx, &id, "SELECT id FROM users WHERE id = $1 FOR UPDATE", userID)
}

func revokeUserSessions(ctx context.Context, tx *sqlx.Tx, userID uuid.UUID) error {
	now := time.Now()
	if _, err := tx.ExecContext(ctx, "UPDATE browser_sessions SET revoked_at = $1, updated_at = $1 WHERE user_id = $2 AND revoked_at IS NULL", now, userID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "UPDATE user_sessions SET is_revoked = true, updated_at = $1 WHERE user_id = $2 AND is_revoked = false", now, userID)
	return err
}

func (r *PgSessionRepository) CreateBrowserSession(ctx context.Context, session *domain.BrowserSession) error {
	if session.ID == uuid.Nil {
		sessionID, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("failed to generate uuid: %w", err)
		}
		session.ID = sessionID
	}

	if session.CreatedAt.IsZero() {
		session.CreatedAt = time.Now()
	}
	if session.UpdatedAt.IsZero() {
		session.UpdatedAt = time.Now()
	}

	query := `INSERT INTO browser_sessions (id, user_id, revoked_at, created_at, updated_at)
			  VALUES (:id, :user_id, :revoked_at, :created_at, :updated_at)`
	_, err := r.db.NamedExecContext(ctx, query, session)
	return err
}

func (r *PgSessionRepository) GetBrowserSession(ctx context.Context, id uuid.UUID) (*domain.BrowserSession, error) {
	session := &domain.BrowserSession{}
	query := `SELECT id, user_id, revoked_at, created_at, updated_at FROM browser_sessions WHERE id = $1`
	err := r.db.GetContext(ctx, session, query, id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return session, nil
}

func (r *PgSessionRepository) IsBrowserSessionActive(ctx context.Context, id uuid.UUID) (bool, error) {
	var active bool
	query := `SELECT EXISTS(SELECT 1 FROM browser_sessions WHERE id = $1 AND revoked_at IS NULL)`
	if err := r.db.GetContext(ctx, &active, query, id); err != nil {
		return false, err
	}
	return active, nil
}

func (r *PgSessionRepository) RevokeBrowserSession(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	query := `UPDATE browser_sessions SET revoked_at = $1, updated_at = $1 WHERE id = $2 AND revoked_at IS NULL`
	_, err := r.db.ExecContext(ctx, query, now, id)
	return err
}

func (r *PgSessionRepository) RevokeBrowserFamilyByToken(ctx context.Context, token string, expectedFamily ...*uuid.UUID) error {
	var expected *uuid.UUID
	if len(expectedFamily) > 0 {
		expected = expectedFamily[0]
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var session domain.UserSession
	err = tx.GetContext(ctx, &session, "SELECT id, user_id FROM user_sessions WHERE refresh_token_digest = $1", digestRefreshToken(token))
	if err == sql.ErrNoRows {
		if expected == nil {
			return nil
		}
		return r.RevokeBrowserSession(ctx, *expected)
	}
	if err != nil {
		return err
	}
	if err := lockSessionUser(ctx, tx, session.UserID); err != nil {
		return err
	}

	// Read the family again after waiting for a concurrent refresh or upgrade.
	if err := tx.GetContext(ctx, &session, "SELECT id, user_id, browser_session_id, replaced_by FROM user_sessions WHERE id = $1", session.ID); err != nil {
		return err
	}
	// A legacy predecessor may point at the refresh row upgraded by another tab.
	for session.BrowserSessionID == nil && session.ReplacedBy != nil {
		if err := tx.GetContext(ctx, &session, "SELECT id, user_id, browser_session_id, replaced_by FROM user_sessions WHERE id = $1 AND user_id = $2", *session.ReplacedBy, session.UserID); err != nil {
			return err
		}
	}
	if expected != nil && (session.BrowserSessionID == nil || *expected != *session.BrowserSessionID) {
		return domain.ErrSessionConflict
	}

	now := time.Now()
	if session.BrowserSessionID != nil {
		if _, err := tx.ExecContext(ctx, "UPDATE browser_sessions SET revoked_at = $1, updated_at = $1 WHERE id = $2 AND revoked_at IS NULL", now, *session.BrowserSessionID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE user_sessions SET is_revoked = true, updated_at = $1 WHERE browser_session_id = $2 AND is_revoked = false", now, *session.BrowserSessionID); err != nil {
			return err
		}
	} else if _, err := tx.ExecContext(ctx, "UPDATE user_sessions SET is_revoked = true, updated_at = $1 WHERE id = $2 AND is_revoked = false", now, session.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *PgSessionRepository) RotateSessionTx(
	ctx context.Context,
	refreshToken string,
	newSessionID uuid.UUID,
	newRefreshTokenDigest string,
	expiresAt time.Time,
	isBrowserMode bool,
) (bool, uuid.UUID, string, string, *uuid.UUID, error) {
	digest := digestRefreshToken(refreshToken)

	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, uuid.Nil, "", "", nil, err
	}
	defer tx.Rollback()

	// 1. Probe session to find user and family
	var session domain.UserSession
	querySession := `SELECT id, user_id, refresh_token_digest, is_revoked, expires_at, created_at, updated_at, rotated_at, replaced_by, browser_session_id
	                 FROM user_sessions WHERE refresh_token_digest = $1`
	if err := tx.GetContext(ctx, &session, querySession, digest); err != nil {
		if err == sql.ErrNoRows {
			return false, uuid.Nil, "", "", nil, domain.ErrSessionNotFound
		}
		return false, uuid.Nil, "", "", nil, err
	}

	// 2. Lock User row first (consistent global lock order: User -> BrowserSession -> UserSession)
	var userInfo struct {
		ID         uuid.UUID         `db:"id"`
		Status     domain.UserStatus `db:"status"`
		Role       domain.UserRole   `db:"role"`
		SystemRole domain.SystemRole `db:"system_role"`
	}
	queryUser := `SELECT id, status, role, system_role FROM users WHERE id = $1 FOR UPDATE`
	if err := tx.GetContext(ctx, &userInfo, queryUser, session.UserID); err != nil {
		return false, uuid.Nil, "", "", nil, err
	}
	if userInfo.Status == domain.UserStatusSuspended {
		if err := revokeUserSessions(ctx, tx, session.UserID); err != nil {
			return false, uuid.Nil, "", "", nil, err
		}
		if err := tx.Commit(); err != nil {
			return false, uuid.Nil, "", "", nil, err
		}
		return false, uuid.Nil, "", "", nil, domain.ErrAccountSuspended
	}

	// 3. Lock BrowserSession row if present
	if session.BrowserSessionID != nil {
		var revokedAt *time.Time
		queryBS := `SELECT revoked_at FROM browser_sessions WHERE id = $1 FOR UPDATE`
		if err := tx.GetContext(ctx, &revokedAt, queryBS, *session.BrowserSessionID); err != nil {
			return false, uuid.Nil, "", "", nil, err
		}
		if revokedAt != nil {
			return false, uuid.Nil, "", "", nil, domain.ErrSessionRevoked
		}
	}

	// 4. Lock UserSession row
	if err := tx.GetContext(ctx, &session, `SELECT id, user_id, refresh_token_digest, is_revoked, expires_at, created_at, updated_at, rotated_at, replaced_by, browser_session_id
	                                        FROM user_sessions WHERE id = $1 FOR UPDATE`, session.ID); err != nil {
		return false, uuid.Nil, "", "", nil, err
	}

	now := time.Now()
	if now.After(session.ExpiresAt) {
		return false, uuid.Nil, "", "", nil, domain.ErrSessionExpired
	}

	// Mode check
	if !isBrowserMode {
		// Legacy mode caller
		if session.BrowserSessionID != nil {
			// A cookie-mode session presented to legacy refresh must never produce JSON
			return false, uuid.Nil, "", "", nil, domain.ErrSessionModeMismatch
		}
	} else {
		// Browser mode caller
		if session.BrowserSessionID == nil {
			if session.IsRevoked {
				return false, uuid.Nil, "", "", nil, domain.ErrSessionRevoked
			}
			// Upgrade legacy session to browser family
			famID, err := uuid.NewV7()
			if err != nil {
				return false, uuid.Nil, "", "", nil, err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO browser_sessions (id, user_id, revoked_at, created_at, updated_at) VALUES ($1, $2, NULL, $3, $3)`, famID, session.UserID, now)
			if err != nil {
				return false, uuid.Nil, "", "", nil, err
			}
			session.BrowserSessionID = &famID
			_, err = tx.ExecContext(ctx, `UPDATE user_sessions SET browser_session_id = $1 WHERE id = $2`, famID, session.ID)
			if err != nil {
				return false, uuid.Nil, "", "", nil, err
			}
		}
	}

	// Check if already revoked
	if session.IsRevoked {
		if session.RotatedAt == nil || session.ReplacedBy == nil {
			return false, uuid.Nil, "", "", nil, domain.ErrSessionRevoked
		}
		// Grace window check
		if now.Sub(*session.RotatedAt) > refreshGraceWindow {
			return false, uuid.Nil, "", "", nil, domain.ErrSessionRevoked
		}
		// Check if successor is active
		var successorActive bool
		err := tx.GetContext(ctx, &successorActive, `SELECT EXISTS(SELECT 1 FROM user_sessions WHERE id = $1 AND is_revoked = false AND expires_at > $2 AND browser_session_id IS NOT DISTINCT FROM $3)`, *session.ReplacedBy, now, session.BrowserSessionID)
		if err != nil {
			return false, uuid.Nil, "", "", nil, err
		}
		if !successorActive {
			return false, uuid.Nil, "", "", nil, domain.ErrSessionRevoked
		}
		// Valid grace refresh!
		if err := tx.Commit(); err != nil {
			return false, uuid.Nil, "", "", nil, err
		}
		return false, userInfo.ID, string(userInfo.Role), string(userInfo.SystemRole), session.BrowserSessionID, nil
	}

	// Active session: rotate it!
	queryInsert := `INSERT INTO user_sessions (id, user_id, refresh_token_digest, is_revoked, expires_at, created_at, updated_at, browser_session_id)
	                VALUES ($1, $2, $3, false, $4, $5, $5, $6)`
	if _, err := tx.ExecContext(ctx, queryInsert, newSessionID, session.UserID, newRefreshTokenDigest, expiresAt, now, session.BrowserSessionID); err != nil {
		return false, uuid.Nil, "", "", nil, err
	}

	queryUpdate := `UPDATE user_sessions SET is_revoked = true, rotated_at = $1, replaced_by = $2, updated_at = $3, browser_session_id = $4 WHERE id = $5`
	if _, err := tx.ExecContext(ctx, queryUpdate, now, newSessionID, now.UTC(), session.BrowserSessionID, session.ID); err != nil {
		return false, uuid.Nil, "", "", nil, err
	}

	if err := tx.Commit(); err != nil {
		return false, uuid.Nil, "", "", nil, err
	}

	return true, userInfo.ID, string(userInfo.Role), string(userInfo.SystemRole), session.BrowserSessionID, nil
}
