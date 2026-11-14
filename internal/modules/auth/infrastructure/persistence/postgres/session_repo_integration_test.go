package postgres_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/infrastructure/persistence/postgres"
	"github.com/stretchr/testify/require"
)

// The URL must identify a disposable Postgres instance. All test data is scoped to a new schema.
func TestBrowserSessionsPostgres(t *testing.T) {
	url := os.Getenv("AUTH_TEST_POSTGRES_URL")
	if url == "" {
		t.Skip("set AUTH_TEST_POSTGRES_URL to run against disposable Postgres")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := sqlx.ConnectContext(ctx, "postgres", url)
	require.NoError(t, err)
	defer admin.Close()
	schema := "auth_test_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	_, err = admin.ExecContext(ctx, "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	defer admin.Exec("DROP SCHEMA " + schema + " CASCADE")
	db, err := sqlx.ConnectContext(ctx, "postgres", url+" search_path="+schema)
	require.NoError(t, err)
	defer db.Close()
	db.SetMaxOpenConns(10)
	_, err = db.ExecContext(ctx, "CREATE TABLE users (id uuid PRIMARY KEY, password_hash text NOT NULL, status text NOT NULL DEFAULT 'active', role text NOT NULL DEFAULT 'producer', system_role text NOT NULL DEFAULT 'user', updated_at timestamptz NOT NULL DEFAULT now())")
	require.NoError(t, err)
	for _, name := range []string{"000023_user_sessions", "000024_hash_session_token", "000038_session_rotation_grace", "000051_browser_sessions"} {
		migration, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "..", "db", "migrations", name+".up.sql"))
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, string(migration))
		require.NoError(t, err)
	}
	repo := postgres.NewSessionRepository(db)

	newUser := func(t *testing.T) uuid.UUID {
		t.Helper()
		id := uuid.New()
		_, err := db.ExecContext(ctx, "INSERT INTO users (id, password_hash) VALUES ($1, 'old-hash')", id)
		require.NoError(t, err)
		return id
	}
	newSession := func(user uuid.UUID, token string, family *uuid.UUID) *domain.UserSession {
		return &domain.UserSession{ID: uuid.New(), UserID: user, RefreshToken: token, ExpiresAt: time.Now().Add(time.Hour), BrowserSessionID: family}
	}

	t.Run("logout and legacy upgrade serialize on the account", func(t *testing.T) {
		for range 12 {
			user := newUser(t)
			token := uuid.NewString()
			require.NoError(t, repo.Create(ctx, newSession(user, token, nil)))
			var family *uuid.UUID
			var rotationErr error
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, _, _, _, family, rotationErr = repo.RotateSessionTx(ctx, token, uuid.New(), uuid.NewString(), time.Now().Add(time.Hour), true)
			}()
			revokeErr := repo.RevokeBrowserFamilyByToken(ctx, token)
			<-done
			require.NoError(t, revokeErr)
			if rotationErr == nil {
				require.NotNil(t, family)
				active, err := repo.IsBrowserSessionActive(ctx, *family)
				require.NoError(t, err)
				require.False(t, active)
			} else {
				require.ErrorIs(t, rotationErr, domain.ErrSessionRevoked)
			}
		}
	})

	t.Run("concurrent rotations choose one successor and logout rejects mismatched families", func(t *testing.T) {
		user := newUser(t)
		family, other := uuid.New(), uuid.New()
		token := uuid.NewString()
		require.NoError(t, repo.CreateAuthenticated(ctx, newSession(user, token, &family), nil))
		require.NoError(t, repo.CreateAuthenticated(ctx, newSession(user, uuid.NewString(), &other), nil))
		type outcome struct {
			rotated bool
			family  *uuid.UUID
			err     error
		}
		results := make(chan outcome, 2)
		for range 2 {
			go func() {
				rotated, _, _, _, returnedFamily, err := repo.RotateSessionTx(ctx, token, uuid.New(), uuid.NewString(), time.Now().Add(time.Hour), true)
				results <- outcome{rotated, returnedFamily, err}
			}()
		}
		first, second := <-results, <-results
		for _, result := range []outcome{first, second} {
			require.NoError(t, result.err)
			require.Equal(t, &family, result.family)
		}
		require.NotEqual(t, first.rotated, second.rotated)
		require.ErrorIs(t, repo.RevokeBrowserFamilyByToken(ctx, token, &other), domain.ErrSessionConflict)
		for _, id := range []uuid.UUID{family, other} {
			active, err := repo.IsBrowserSessionActive(ctx, id)
			require.NoError(t, err)
			require.True(t, active)
		}
		require.NoError(t, repo.RevokeBrowserFamilyByToken(ctx, token, &family))
		active, err := repo.IsBrowserSessionActive(ctx, family)
		require.NoError(t, err)
		require.False(t, active)
		_, _, _, _, _, err = repo.RotateSessionTx(ctx, token, uuid.New(), uuid.NewString(), time.Now().Add(time.Hour), true)
		require.ErrorIs(t, err, domain.ErrSessionRevoked)
	})

	t.Run("password update and revocation serialize with old-password login", func(t *testing.T) {
		for range 8 {
			user := newUser(t)
			family := uuid.New()
			old := "old-hash"
			result := make(chan error, 1)
			go func() { result <- repo.CreateAuthenticated(ctx, newSession(user, uuid.NewString(), &family), &old) }()
			resetErr := repo.ResetPasswordAndRevokeSessions(ctx, user, "new-hash")
			issueErr := <-result
			require.NoError(t, resetErr)
			if issueErr != nil {
				require.ErrorIs(t, issueErr, domain.ErrInvalidCredentials)
			}
			require.ErrorIs(t, repo.CreateAuthenticated(ctx, newSession(user, uuid.NewString(), &family), &old), domain.ErrInvalidCredentials)
			active, err := repo.IsBrowserSessionActive(ctx, family)
			require.NoError(t, err)
			require.False(t, active)
			newHash := "new-hash"
			fresh := uuid.New()
			require.NoError(t, repo.CreateAuthenticated(ctx, newSession(user, uuid.NewString(), &fresh), &newHash))
		}
	})
}
