package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/infrastructure/persistence/postgres"
	"github.com/stretchr/testify/require"
)

func TestPgSessionRepositoryMissingAndErrorBranches(t *testing.T) {
	db, mockDB, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewSessionRepository(db)
	ctx := context.Background()

	mockDB.ExpectQuery("SELECT .+ FROM user_sessions").WithArgs(sqlmock.AnyArg()).WillReturnError(sql.ErrNoRows)
	session, err := repo.GetByToken(ctx, "missing")
	require.NoError(t, err)
	require.Nil(t, session)

	mockDB.ExpectQuery("SELECT .+ FROM user_sessions").WithArgs(sqlmock.AnyArg()).WillReturnError(errors.New("read failed"))
	session, err = repo.GetByToken(ctx, "error")
	require.EqualError(t, err, "read failed")
	require.Nil(t, session)

	userID := uuid.New()
	mockDB.ExpectBegin()
	mockDB.ExpectQuery("SELECT id FROM users").WithArgs(userID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(userID))
	mockDB.ExpectExec("UPDATE browser_sessions SET revoked_at = \\$1").
		WithArgs(sqlmock.AnyArg(), userID).WillReturnResult(sqlmock.NewResult(0, 1))
	mockDB.ExpectExec("UPDATE user_sessions SET is_revoked = true").
		WithArgs(sqlmock.AnyArg(), userID).WillReturnResult(sqlmock.NewResult(0, 2))
	mockDB.ExpectCommit()
	require.NoError(t, repo.RevokeAllForUser(ctx, userID))

	mockDB.ExpectBegin()
	mockDB.ExpectQuery("SELECT id FROM users").WithArgs(userID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(userID))
	mockDB.ExpectExec("UPDATE browser_sessions SET revoked_at = \\$1").
		WithArgs(sqlmock.AnyArg(), userID).WillReturnError(errors.New("write failed"))
	mockDB.ExpectRollback()
	require.EqualError(t, repo.RevokeAllForUser(ctx, userID), "write failed")
	require.NoError(t, mockDB.ExpectationsWereMet())
}

func TestPgSessionRepositoryMarkRotatedAndIsActive(t *testing.T) {
	db, mockDB, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewSessionRepository(db)
	ctx := context.Background()
	successor := uuid.New()

	mockDB.ExpectExec(`UPDATE user_sessions SET is_revoked = true, rotated_at = \$1, replaced_by = \$2, updated_at = \$3\s+WHERE refresh_token_digest = \$4 AND is_revoked = false`).
		WithArgs(sqlmock.AnyArg(), successor, sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	rotated, err := repo.MarkRotated(ctx, "old", successor)
	require.NoError(t, err)
	require.True(t, rotated)

	mockDB.ExpectExec("UPDATE user_sessions SET is_revoked = true, rotated_at").
		WithArgs(sqlmock.AnyArg(), successor, sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 0))
	rotated, err = repo.MarkRotated(ctx, "old", successor)
	require.NoError(t, err)
	require.False(t, rotated, "already-revoked session must not be rotated twice")

	mockDB.ExpectExec("UPDATE user_sessions SET is_revoked = true, rotated_at").
		WithArgs(sqlmock.AnyArg(), successor, sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnError(errors.New("write failed"))
	_, err = repo.MarkRotated(ctx, "old", successor)
	require.EqualError(t, err, "write failed")

	mockDB.ExpectQuery("SELECT EXISTS").WithArgs(successor, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	active, err := repo.IsActive(ctx, successor)
	require.NoError(t, err)
	require.True(t, active)

	mockDB.ExpectQuery("SELECT EXISTS").WithArgs(successor, sqlmock.AnyArg()).WillReturnError(errors.New("read failed"))
	_, err = repo.IsActive(ctx, successor)
	require.EqualError(t, err, "read failed")
	require.NoError(t, mockDB.ExpectationsWereMet())
}
