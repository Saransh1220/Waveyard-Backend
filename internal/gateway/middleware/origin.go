package middleware

import (
	"net/url"
	"strings"
)

// NormalizeOrigin normalizes an origin string into scheme://host (no trailing slash, lowercased scheme/host).
// It returns empty string if the origin is invalid, malformed, "null", or contains path/query/fragment components.
func NormalizeOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	if (u.Path != "" && u.Path != "/") || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" || strings.Contains(raw, "#") || u.User != nil {
		return ""
	}
	host := strings.ToLower(u.Host)
	return scheme + "://" + host
}

// ParseAllowedOrigins parses a comma-separated list of allowed origins and returns a deduplicated map of normalized origins.
func ParseAllowedOrigins(allowedOrigins string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, part := range strings.Split(allowedOrigins, ",") {
		part = strings.TrimSpace(part)
		if part == "*" {
			result["*"] = struct{}{}
			continue
		}
		if norm := NormalizeOrigin(part); norm != "" {
			result[norm] = struct{}{}
		}
	}
	return result
}

// IsOriginAllowed checks whether an origin string is in the allowed origins set.
// If allowWildcard is false, "*" in allowed is ignored (used for credentialed/cookie CSRF guard).
func IsOriginAllowed(origin string, allowed map[string]struct{}, allowWildcard ...bool) bool {
	if origin == "" {
		return false
	}
	wildcardOk := len(allowWildcard) > 0 && allowWildcard[0]
	if wildcardOk {
		if _, ok := allowed["*"]; ok {
			return true
		}
	}
	norm := NormalizeOrigin(origin)
	if norm == "" {
		return false
	}
	_, ok := allowed[norm]
	return ok
}
