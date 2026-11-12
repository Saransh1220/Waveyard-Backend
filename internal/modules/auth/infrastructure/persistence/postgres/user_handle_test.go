package postgres_test

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/infrastructure/persistence/postgres"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPublicHandleResolvesHandleThenLegacyName(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewUserRepository(db)
	id := uuid.New()
	byHandle := `SELECT \* FROM users WHERE lower\(handle\) = lower\(\$1\) AND status = 'active' LIMIT 1`
	byName := `SELECT \* FROM users WHERE lower\(name\) = lower\(\$1\) AND status = 'active' LIMIT 2`

	// A handle match wins and the legacy name is never consulted.
	mock.ExpectQuery(byHandle).WithArgs("blaze").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(id, "saransh"))
	user, err := repo.GetByHandle(context.Background(), "blaze")
	require.NoError(t, err)
	require.Equal(t, id, user.ID)

	// No handle match: fall back to exactly one legacy login name.
	for _, count := range []int{1, 0, 2} {
		mock.ExpectQuery(byHandle).WithArgs("saransh").WillReturnRows(sqlmock.NewRows([]string{"id", "name"}))
		rows := sqlmock.NewRows([]string{"id", "name"})
		for i := 0; i < count; i++ {
			rows.AddRow(id, "saransh")
		}
		mock.ExpectQuery(byName).WithArgs("saransh").WillReturnRows(rows)
		user, err := repo.GetByHandle(context.Background(), "saransh")
		if count == 1 {
			require.NoError(t, err)
			require.Equal(t, id, user.ID)
		} else {
			require.ErrorIs(t, err, domain.ErrUserNotFound)
			require.Nil(t, user)
		}
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPgUserRepository_UpdateHandle(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewUserRepository(db)
	ctx := context.Background()
	id := uuid.New()
	update := `UPDATE users SET handle = \$1, updated_at = NOW\(\) WHERE id = \$2`

	mock.ExpectExec(update).WithArgs("blaze", id).WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.UpdateHandle(ctx, id, "blaze"))

	mock.ExpectExec(update).WithArgs("blaze", id).WillReturnError(&pq.Error{Code: "23505"})
	require.ErrorIs(t, repo.UpdateHandle(ctx, id, "blaze"), domain.ErrHandleTaken)

	mock.ExpectExec(update).WithArgs("blaze", id).WillReturnError(errors.New("db down"))
	require.EqualError(t, repo.UpdateHandle(ctx, id, "blaze"), "db down")

	mock.ExpectExec(update).WithArgs("blaze", id).WillReturnResult(sqlmock.NewResult(0, 0))
	require.ErrorIs(t, repo.UpdateHandle(ctx, id, "blaze"), domain.ErrUserNotFound)

	mock.ExpectExec(update).WithArgs("blaze", id).WillReturnResult(sqlmock.NewErrorResult(errors.New("rows")))
	require.EqualError(t, repo.UpdateHandle(ctx, id, "blaze"), "rows")

	require.NoError(t, mock.ExpectationsWereMet())
}
