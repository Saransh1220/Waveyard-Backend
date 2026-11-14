package middleware

import (
	"net/http"
)

// CORSMiddleware handles CORS preflight and headers.
// It uses normalized origin matching and sets Vary: Origin.
func CORSMiddleware(next http.Handler, allowedOrigins string) http.Handler {
	allowed := ParseAllowedOrigins(allowedOrigins)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		origin := r.Header.Get("Origin")

		validOrigin := false
		if origin != "" {
			if _, ok := allowed["*"]; ok {
				validOrigin = true
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else if IsOriginAllowed(origin, allowed) {
				validOrigin = true
				norm := NormalizeOrigin(origin)
				if norm != "" {
					w.Header().Set("Access-Control-Allow-Origin", norm)
				} else {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				}
			}
		}

		if validOrigin {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key, X-Skip-Loading, X-Waveyard-Session, X-Waveyard-Expected-Viewer")
			if w.Header().Get("Access-Control-Allow-Origin") != "*" {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			w.Header().Set("Access-Control-Max-Age", "86400")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
