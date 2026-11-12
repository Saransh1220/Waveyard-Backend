package domain

import (
	"errors"
	"regexp"
	"strings"
)

var (
	ErrInvalidHandle = errors.New("handle must be 3-30 characters: lowercase letters, numbers, hyphens or underscores")
	ErrHandleTaken   = errors.New("that handle is already taken")
)

var handlePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,28}[a-z0-9]$`)

var reservedHandles = map[string]struct{}{
	"admin": {}, "support": {}, "waveyard": {}, "staff": {}, "help": {}, "api": {}, "root": {},
}

// NormalizeHandle trims, strips a leading "@" and lower-cases a requested handle.
func NormalizeHandle(raw string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(raw), "@"))
}

// ValidateHandle checks a normalized handle against the public format rules.
func ValidateHandle(handle string) error {
	if !handlePattern.MatchString(handle) {
		return ErrInvalidHandle
	}
	if _, reserved := reservedHandles[handle]; reserved {
		return ErrInvalidHandle
	}
	return nil
}
