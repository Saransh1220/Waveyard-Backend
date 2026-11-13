package main

import (
	"fmt"
	"strings"
)

// loadMetricsToken resolves the static bearer token that lets a scraper read
// /metrics. Order: METRICS_TOKEN, then the file named by METRICS_TOKEN_FILE.
//
// A missing token is not fatal: the bearer path is simply disabled and
// /metrics stays admin-JWT only. In production the token is required, so a
// deploy cannot silently run with monitoring locked out.
func loadMetricsToken(getenv func(string) string, readFile func(string) ([]byte, error)) (token string, warning string, err error) {
	if v := strings.TrimSpace(getenv("METRICS_TOKEN")); v != "" {
		return v, "", nil
	}

	if path := strings.TrimSpace(getenv("METRICS_TOKEN_FILE")); path != "" {
		raw, readErr := readFile(path)
		if readErr != nil {
			return "", "", fmt.Errorf("read METRICS_TOKEN_FILE %q: %w", path, readErr)
		}
		if v := strings.TrimSpace(string(raw)); v != "" {
			return v, "", nil
		}
		return "", "", fmt.Errorf("METRICS_TOKEN_FILE %q is empty", path)
	}

	if strings.EqualFold(strings.TrimSpace(getenv("ENV")), "production") {
		return "", "", fmt.Errorf("METRICS_TOKEN (or METRICS_TOKEN_FILE) is required when ENV=production")
	}
	return "", "METRICS_TOKEN not set: /metrics only accepts admin JWTs, Prometheus scraping will get 401", nil
}
