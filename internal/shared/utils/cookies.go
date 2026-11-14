package utils

import (
	"net/http"
	"time"
)

const (
	AccessCookieNameSecure   = "__Host-wy_access"
	AccessCookieNameInsecure = "wy_access"
	RefreshCookieName        = "refresh_token"
	HeaderSessionMarker      = "X-Waveyard-Session"
	SessionMarkerCookie      = "cookie"
	HeaderExpectedViewer     = "X-Waveyard-Expected-Viewer"
)

// AccessCookieName returns the standard cookie name for the access token based on secure flag.
func AccessCookieName(secure bool) string {
	if secure {
		return AccessCookieNameSecure
	}
	return AccessCookieNameInsecure
}

// BuildAuthCookie creates an http.Cookie for browser auth with HttpOnly, Path=/, explicit SameSite=Lax.
func BuildAuthCookie(name, value string, expires time.Time, maxAge int, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		Expires:  expires,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// BuildDeleteAuthCookie creates a tombstone http.Cookie for browser auth.
func BuildDeleteAuthCookie(name string, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// BuildCookie creates an http.Cookie configuring SameSite and Secure consistently.
// If secure is true, SameSite is None; otherwise Strict.
func BuildCookie(name, value, path string, expires time.Time, maxAge int, secure bool, httpOnly bool) *http.Cookie {
	sameSite := http.SameSiteStrictMode
	if secure {
		sameSite = http.SameSiteNoneMode
	}

	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		Expires:  expires,
		MaxAge:   maxAge,
		HttpOnly: httpOnly,
		Secure:   secure,
		SameSite: sameSite,
	}
}

// SetCookie configures and sets an HTTP cookie on the response writer using BuildCookie.
func SetCookie(w http.ResponseWriter, name, value, path string, expires time.Time, maxAge int, secure bool, httpOnly bool) {
	http.SetCookie(w, BuildCookie(name, value, path, expires, maxAge, secure, httpOnly))
}
