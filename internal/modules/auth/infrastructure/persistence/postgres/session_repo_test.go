package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/infrastructure/persistence/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPgSessionRepository_Create_GeneratesUUIDv7(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()

	repo := postgres.NewSessionRepository(db)
	session := &domain.UserSession{
		UserID:       uuid.New(),
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour),
	}

	mock.ExpectExec("INSERT INTO user_sessions").WillReturnResult(sqlmock.NewResult(1, 1))

	err := repo.Create(context.Background(), session)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, session.ID)
	assert.Equal(t, uuid.Version(7), session.ID.Version())
	assert.NotEmpty(t, session.RefreshTokenDigest)
	assert.NotEqual(t, session.RefreshToken, session.RefreshTokenDigest)
}

func TestPgSessionRepository_UsesTokenDigestForLookupAndRevoke(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()

	repo := postgres.NewSessionRepository(db)
	token := "refresh-token"
	sessionID := uuid.New()
	userID := uuid.New()

	rows := sqlmock.NewRows([]string{"id", "user_id", "refresh_token_digest", "is_revoked", "expires_at", "created_at", "updated_at", "rotated_at", "replaced_by", "browser_session_id"}).
		AddRow(sessionID, userID, "digest", false, time.Now().Add(time.Hour), time.Now(), time.Now(), nil, nil, nil)
	mock.ExpectQuery("SELECT .+ FROM user_sessions WHERE refresh_token_digest = \\$1").
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(rows)

	session, err := repo.GetByToken(context.Background(), token)
	require.NoError(t, err)
	require.NotNil(t, session)
	assert.Equal(t, sessionID, session.ID)

	mock.ExpectExec("UPDATE user_sessions SET is_revoked = true, updated_at = \\$1 WHERE refresh_token_digest = \\$2").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Revoke(context.Background(), token))
}
