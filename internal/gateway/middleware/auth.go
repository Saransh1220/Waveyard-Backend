package middleware

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	"github.com/saransh1220/blueprint-audio/internal/shared/utils"
)

type contextKey string

const (
	ContextKeyUserId     contextKey = "user_id"
	ContextKeyRole       contextKey = "role"
	ContextKeySystemRole contextKey = "system_role"
)

// AccountState is the live, database-backed view of an account. The JWT only
// proves who the caller was when the token was issued; this is what they are
// allowed to do right now.
type AccountState struct {
	Status     domain.UserStatus
	Role       domain.UserRole
	SystemRole domain.SystemRole
}

// AccountLookup loads the current AccountState for a user.
type AccountLookup func(ctx context.Context, userID uuid.UUID) (AccountState, error)

// BrowserSessionLookup verifies whether a browser session family is currently active.
type BrowserSessionLookup func(ctx context.Context, sessionID uuid.UUID) (bool, error)

type cachedAccount struct {
	state   AccountState
	expires time.Time
}

type AuthMiddleWare struct {
	jwtSecret     string
	roles         *RoleStore
	sessionLookup BrowserSessionLookup

	lookup   AccountLookup
	cacheTTL time.Duration
	mu       sync.Mutex
	cache    map[uuid.UUID]cachedAccount
}

const maxAccountCacheEntries = 10000

// NewAuthMiddleware creates and returns a new instance of AuthMiddleWare.
func NewAuthMiddleware(jwtSecret string) *AuthMiddleWare {
	return &AuthMiddleWare{jwtSecret: jwtSecret}
}

// WithBrowserSessionLookup configures database verification for browser session families.
func (m *AuthMiddleWare) WithBrowserSessionLookup(lookup BrowserSessionLookup) *AuthMiddleWare {
	m.sessionLookup = lookup
	return m
}

// WithAccountLookup makes the middleware re-check the account on every
// request (cached for ttl). Suspended accounts are rejected and the system
// role in the context comes from the database rather than the token, so a
// suspension or demotion takes effect within ttl instead of when the access
// token expires. Without a lookup the middleware trusts the token claims.
func (m *AuthMiddleWare) WithAccountLookup(lookup AccountLookup, ttl time.Duration) *AuthMiddleWare {
	m.lookup = lookup
	m.cacheTTL = ttl
	m.cache = make(map[uuid.UUID]cachedAccount)
	return m
}

func (m *AuthMiddleWare) accountState(ctx context.Context, userID uuid.UUID) (AccountState, error) {
	if m.cacheTTL > 0 {
		m.mu.Lock()
		c, ok := m.cache[userID]
		m.mu.Unlock()
		if ok && time.Now().Before(c.expires) {
			return c.state, nil
		}
	}
	state, err := m.lookup(ctx, userID)
	if err != nil {
		return AccountState{}, err
	}
	if m.cacheTTL > 0 {
		m.mu.Lock()
		if len(m.cache) >= maxAccountCacheEntries {
			now := time.Now()
			for id, c := range m.cache {
				if now.After(c.expires) {
					delete(m.cache, id)
				}
			}
			if len(m.cache) >= maxAccountCacheEntries {
				m.cache = make(map[uuid.UUID]cachedAccount)
			}
		}
		m.cache[userID] = cachedAccount{state: state, expires: time.Now().Add(m.cacheTTL)}
		m.mu.Unlock()
	}
	return state, nil
}

type authFailure struct {
	status int
	body   string
}

func isUnmarkedDownload(r *http.Request) bool {
	return (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
		strings.HasPrefix(r.URL.Path, "/licenses/") && strings.HasSuffix(r.URL.Path, "/downloads") &&
		strings.Count(r.URL.Path, "/") == 3
}

// authenticate validates the token and returns a request context carrying the
// caller's identity. Supports browser cookie session mode and legacy Bearer mode.
func (m *AuthMiddleWare) authenticate(r *http.Request) (context.Context, *authFailure) {
	sessionMarkers := r.Header.Values(utils.HeaderSessionMarker)
	if len(sessionMarkers) > 1 {
		return nil, &authFailure{http.StatusBadRequest, `{"error": "duplicate session marker header"}`}
	}

	isBrowser := false
	if len(sessionMarkers) == 1 {
		val := strings.TrimSpace(sessionMarkers[0])
		if val != utils.SessionMarkerCookie {
			return nil, &authFailure{http.StatusBadRequest, `{"error": "unknown session mode"}`}
		}
		isBrowser = true
	}

	tokenStr := ""
	authorization := r.Header.Values("Authorization")
	if len(authorization) > 1 || (isBrowser && len(authorization) != 0) {
		return nil, &authFailure{http.StatusBadRequest, `{"error":"invalid credential combination"}`}
	}
	if !isBrowser && len(authorization) == 1 {
		scheme, value, ok := strings.Cut(authorization[0], " ")
		if ok && scheme == "Bearer" {
			tokenStr = value
		}
	} else if isBrowser || isUnmarkedDownload(r) {
		isBrowser = true
		found := false
		for _, cookie := range r.Cookies() {
			if cookie.Name != utils.AccessCookieNameSecure && cookie.Name != utils.AccessCookieNameInsecure {
				continue
			}
			if found {
				return nil, &authFailure{http.StatusBadRequest, `{"error":"duplicate access cookies"}`}
			}
			found = true
			tokenStr = cookie.Value
		}
	}
	if tokenStr == "" {
		return nil, &authFailure{http.StatusUnauthorized, `{"error":"missing or invalid authorization"}`}
	}

	claims, err := utils.ValidateToken(tokenStr, m.jwtSecret)
	if err != nil {
		return nil, &authFailure{http.StatusUnauthorized, `{"error": "invalid or expired token"}`}
	}

	if isBrowser {
		if claims.Purpose != utils.TokenPurposeBrowser {
			return nil, &authFailure{http.StatusUnauthorized, `{"error": "invalid token purpose"}`}
		}
		if claims.SessionID == nil {
			return nil, &authFailure{http.StatusUnauthorized, `{"error": "missing session family id"}`}
		}
		if m.sessionLookup == nil {
			return nil, &authFailure{http.StatusServiceUnavailable, `{"error":"unable to verify session"}`}
		}
		active, err := m.sessionLookup(r.Context(), *claims.SessionID)
		if err != nil {
			return nil, &authFailure{http.StatusServiceUnavailable, `{"error":"unable to verify session"}`}
		}
		if !active {
			return nil, &authFailure{http.StatusUnauthorized, `{"error":"session revoked"}`}
		}

	} else {
		if claims.Purpose == utils.TokenPurposeBrowser {
			return nil, &authFailure{http.StatusUnauthorized, `{"error": "invalid token purpose"}`}
		}
	}

	role := claims.Role
	systemRole := claims.SystemRole
	if m.lookup != nil {
		state, err := m.accountState(r.Context(), claims.UserID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, &authFailure{http.StatusUnauthorized, `{"error": "invalid or expired token"}`}
			}
			return nil, &authFailure{http.StatusServiceUnavailable, `{"error": "unable to verify account"}`}
		}
		if state.Status == domain.UserStatusSuspended {
			return nil, &authFailure{http.StatusForbidden, `{"error": "account suspended", "code":"ACCOUNT_SUSPENDED"}`}
		}
		role = string(state.Role)
		systemRole = string(state.SystemRole)
	}

	expectedViewer := strings.TrimSpace(r.Header.Get(utils.HeaderExpectedViewer))
	if expectedViewer != "" {
		if expectedViewer == "guest" || expectedViewer != claims.UserID.String() {
			return nil, &authFailure{http.StatusConflict, `{"error": "session changed", "code": "SESSION_CHANGED"}`}
		}
	}

	ctx := context.WithValue(r.Context(), ContextKeyUserId, claims.UserID)
	ctx = context.WithValue(ctx, ContextKeyRole, role)
	ctx = context.WithValue(ctx, ContextKeySystemRole, systemRole)
	return ctx, nil
}

// RequireAuth enforces authentication on HTTP requests.
func (m *AuthMiddleWare) RequireAuth(next http.Handler) http.Handler {
	return m.require(next)
}

func (m *AuthMiddleWare) require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, fail := m.authenticate(r)
		if fail != nil {
			http.Error(w, fail.body, fail.status)
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// FlexibleAuth attempts authentication, preserving guest behavior when no valid token is provided,
// while enforcing viewer expectations and failing on infrastructure errors.
func (m *AuthMiddleWare) FlexibleAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedViewer := strings.TrimSpace(r.Header.Get(utils.HeaderExpectedViewer))

		hasAuthHeader := r.Header.Get("Authorization") != ""
		hasMarker := r.Header.Get(utils.HeaderSessionMarker) != ""
		hasCookie := false
		for _, c := range r.Cookies() {
			if (c.Name == utils.AccessCookieNameSecure || c.Name == utils.AccessCookieNameInsecure) && c.Value != "" {
				hasCookie = true
				break
			}
		}

		if !hasAuthHeader && !hasMarker && !hasCookie {
			if expectedViewer != "" && expectedViewer != "guest" {
				http.Error(w, `{"error": "session changed", "code": "SESSION_CHANGED"}`, http.StatusConflict)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		ctx, fail := m.authenticate(r)
		if fail != nil {
			if fail.status == http.StatusServiceUnavailable || fail.status == http.StatusConflict || fail.status == http.StatusBadRequest {
				http.Error(w, fail.body, fail.status)
				return
			}
			if expectedViewer != "" && expectedViewer != "guest" {
				http.Error(w, fail.body, fail.status)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
