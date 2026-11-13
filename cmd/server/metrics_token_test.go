package main

import (
	"errors"
	"testing"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func fileReturning(content string, err error) func(string) ([]byte, error) {
	return func(string) ([]byte, error) { return []byte(content), err }
}

func TestLoadMetricsToken(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		file        func(string) ([]byte, error)
		wantToken   string
		wantWarning bool
		wantErr     bool
	}{
		{"env var wins", map[string]string{"METRICS_TOKEN": " abc ", "METRICS_TOKEN_FILE": "/x"}, fileReturning("file-token", nil), "abc", false, false},
		{"file fallback", map[string]string{"METRICS_TOKEN_FILE": "/x"}, fileReturning("file-token\n", nil), "file-token", false, false},
		{"file unreadable", map[string]string{"METRICS_TOKEN_FILE": "/x"}, fileReturning("", errors.New("nope")), "", false, true},
		{"file empty", map[string]string{"METRICS_TOKEN_FILE": "/x"}, fileReturning("  \n", nil), "", false, true},
		{"unset in development warns", map[string]string{"ENV": "development"}, fileReturning("", nil), "", true, false},
		{"unset with no ENV warns", map[string]string{}, fileReturning("", nil), "", true, false},
		{"unset in production fails", map[string]string{"ENV": "production"}, fileReturning("", nil), "", false, true},
		{"set in production ok", map[string]string{"ENV": "production", "METRICS_TOKEN": "abc"}, fileReturning("", nil), "abc", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			token, warning, err := loadMetricsToken(envMap(tc.env), tc.file)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if token != tc.wantToken {
				t.Errorf("token = %q, want %q", token, tc.wantToken)
			}
			if (warning != "") != tc.wantWarning {
				t.Errorf("warning = %q, wantWarning %v", warning, tc.wantWarning)
			}
		})
	}
}
