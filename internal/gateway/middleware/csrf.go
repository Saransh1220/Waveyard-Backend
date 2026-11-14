package middleware

import (
	"net/http"
	"strings"

	"github.com/saransh1220/blueprint-audio/internal/shared/utils"
)

// CSRFGuardMiddleware enforces Origin verification for browser mutations.
// Unsafe requests carrying X-Waveyard-Session: cookie must present an exact trusted Origin.
// Unmarked unsafe requests carrying browser credentials are rejected.
// Server-to-server webhook callbacks without browser credentials are exempt.
func CSRFGuardMiddleware(next http.Handler, allowedOrigins string) http.Handler {
	allowed := ParseAllowedOrigins(allowedOrigins)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Authenticated and viewer-dependent responses must bypass shared caches.
		if r.Header.Get(utils.HeaderSessionMarker) != "" || r.Header.Get("Authorization") != "" || hasBrowserAccessCookies(r) {
			w.Header().Set("Cache-Control", "private, no-store")
		}
		// Safe methods and CORS preflight do not require CSRF mutation checks.
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		// Webhook exemption: server-to-server callbacks (e.g. /webhooks/*)
		// MUST NOT carry browser auth credentials.
		if strings.HasPrefix(r.URL.Path, "/webhooks/") {
			if hasAnyAuthCookies(r) || r.Header.Get(utils.HeaderSessionMarker) != "" {
				http.Error(w, `{"error":"forbidden: webhooks must not carry browser credentials","code":"CSRF_REJECTED"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		sessionMarkerValues := r.Header.Values(utils.HeaderSessionMarker)
		if len(sessionMarkerValues) > 1 || (len(sessionMarkerValues) == 1 && strings.TrimSpace(sessionMarkerValues[0]) != utils.SessionMarkerCookie) {
			http.Error(w, `{"error":"invalid session marker header"}`, http.StatusBadRequest)
			return
		}

		isMarked := len(sessionMarkerValues) == 1 && strings.TrimSpace(sessionMarkerValues[0]) == utils.SessionMarkerCookie

		if isMarked {
			// Require exact trusted Origin
			origins := r.Header.Values("Origin")
			if len(origins) != 1 {
				http.Error(w, `{"error":"forbidden: missing or invalid origin header","code":"CSRF_REJECTED"}`, http.StatusForbidden)
				return
			}
			origin := strings.TrimSpace(origins[0])
			if origin == "" || origin == "null" {
				http.Error(w, `{"error":"forbidden: null or empty origin","code":"CSRF_REJECTED"}`, http.StatusForbidden)
				return
			}
			// Exact match required (no wildcard for browser mutations)
			if !IsOriginAllowed(origin, allowed, false) {
				http.Error(w, `{"error":"forbidden: origin not allowed","code":"CSRF_REJECTED"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		// Unmarked unsafe request:
		// 1. Reject if carrying browser access cookies (unmarked form submission with credentials)
		if hasBrowserAccessCookies(r) {
			http.Error(w, `{"error":"forbidden: unmarked mutation with browser credentials","code":"CSRF_REJECTED"}`, http.StatusForbidden)
			return
		}

		// 2. Legacy browser caller compatibility: for browser mutations with an Origin header,
		// enforce that the Origin is trusted.
		origins := r.Header.Values("Origin")
		if len(origins) > 0 || (hasAnyAuthCookies(r) && (r.URL.Path == "/auth/refresh" || r.URL.Path == "/auth/logout")) {
			if len(origins) != 1 || !IsOriginAllowed(strings.TrimSpace(origins[0]), allowed, false) {
				http.Error(w, `{"error":"forbidden: origin not allowed","code":"CSRF_REJECTED"}`, http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func hasBrowserAccessCookies(r *http.Request) bool {
	for _, c := range r.Cookies() {
		if (c.Name == utils.AccessCookieNameSecure || c.Name == utils.AccessCookieNameInsecure) && c.Value != "" {
			return true
		}
	}
	return false
}

func hasAnyAuthCookies(r *http.Request) bool {
	for _, c := range r.Cookies() {
		if (c.Name == utils.AccessCookieNameSecure || c.Name == utils.AccessCookieNameInsecure || c.Name == utils.RefreshCookieName) && c.Value != "" {
			return true
		}
	}
	return false
}
