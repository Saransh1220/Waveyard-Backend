package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebSocketTickets_AccountLookupAndCapacity(t *testing.T) {
	user := uuid.New()
	var lookupErr error
	state := AccountState{Status: domain.UserStatusActive, SystemRole: domain.SystemRoleUser}
	auth := NewAuthMiddleware("test-secret").WithAccountLookup(func(ctx context.Context, id uuid.UUID) (AccountState, error) {
		return state, lookupErr
	}, time.Minute)

	tickets := NewWebSocketTickets(auth)
	now := time.Now()
	tickets.now = func() time.Time { return now }

	issueFor := func(uid uuid.UUID) string {
		req := httptest.NewRequest(http.MethodPost, "/ws/ticket", nil)
		req = req.WithContext(context.WithValue(req.Context(), ContextKeyUserId, uid))
		rec := httptest.NewRecorder()
		tickets.Issue(rec, req)
		var body struct {
			Ticket string `json:"ticket"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return body.Ticket
	}

	endpoint := tickets.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	t.Run("suspended user is rejected 403", func(t *testing.T) {
		uSuspended := uuid.New()
		state.Status = domain.UserStatusSuspended
		ticket := issueFor(uSuspended)
		rec := httptest.NewRecorder()
		endpoint.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ws?ticket="+ticket, nil))
		require.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("account lookup failure returns 503", func(t *testing.T) {
		uErr := uuid.New()
		lookupErr = errors.New("db error")
		ticket := issueFor(uErr)
		rec := httptest.NewRecorder()
		endpoint.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ws?ticket="+ticket, nil))
		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})

	t.Run("ticket capacity reached returns 503", func(t *testing.T) {
		tickets.mu.Lock()
		for i := 0; i < 10000; i++ {
			tickets.tickets[uuid.New().String()] = socketTicket{userID: user, expires: now.Add(time.Hour)}
		}
		tickets.mu.Unlock()

		req := httptest.NewRequest(http.MethodPost, "/ws/ticket", nil)
		req = req.WithContext(context.WithValue(req.Context(), ContextKeyUserId, user))
		rec := httptest.NewRecorder()
		tickets.Issue(rec, req)
		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})
}

func TestRoleStore_Branches(t *testing.T) {
	t.Run("isKnownPermission returns false for unknown", func(t *testing.T) {
		assert.False(t, isKnownPermission(Permission("unknown:perm")))
		assert.True(t, isKnownPermission(PermAdminOrdersRefund))
	})

	t.Run("Reload with unknown permission logs warning and succeeds", func(t *testing.T) {
		s := NewRoleStore(func(context.Context) ([]RoleDef, error) {
			return []RoleDef{
				{ID: 1, Name: "custom", Kind: RoleKindSystem, Permissions: []Permission{Permission("unregistered:perm")}},
			}, nil
		})
		require.NoError(t, s.Reload(context.Background()))
		assert.True(t, s.HasRole("custom", RoleKindSystem))
		assert.False(t, s.HasRole("custom", RoleKindBusiness))
		assert.False(t, s.HasRole("nonexistent", RoleKindSystem))
	})

	t.Run("NewPgRoleLoader with sqlmock", func(t *testing.T) {
		sqlDB, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer sqlDB.Close()
		db := sqlx.NewDb(sqlDB, "sqlmock")

		loader := NewPgRoleLoader(db)
		mock.ExpectQuery(`SELECT id, name, kind, description, permissions FROM roles`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "kind", "description", "permissions"}).
				AddRow(1, "producer", "business", "Producer role", pq.StringArray{"spec:upload"}))

		defs, err := loader(context.Background())
		require.NoError(t, err)
		require.Len(t, defs, 1)
		assert.Equal(t, "producer", defs[0].Name)
	})
}
