package jwt

import (
	"crypto/rand"
	"encoding/base64"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
)

const (
	PurposeBearer  = ""
	PurposeBrowser = "browser"
)

type CustomClaims struct {
	UserID     uuid.UUID  `json:"user_id"`
	Role       string     `json:"role"`
	SystemRole string     `json:"system_role"`
	Purpose    string     `json:"prp,omitempty"`
	SessionID  *uuid.UUID `json:"sid,omitempty"`
	jwt.RegisteredClaims
}

// GenerateToken creates a JWT token string signed with the provided secret.
// The token includes custom claims for the user's UUID and role, and standard
// registered claims for expiration, issued at, and not before times.
// The duration parameter specifies how long the token is valid.
// Returns the signed JWT token string or an error if signing fails.
func GenerateToken(secret string, duration time.Duration, userID uuid.UUID, role string, systemRole ...string) (string, error) {
	roleClaim := string(domain.SystemRoleUser)
	if len(systemRole) > 0 && systemRole[0] != "" {
		roleClaim = systemRole[0]
	}
	claims := CustomClaims{
		UserID:     userID,
		Role:       role,
		SystemRole: roleClaim,
		Purpose:    PurposeBearer,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(duration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// GenerateBrowserToken creates a browser session JWT token string containing the session family ID (sid) and browser purpose (prp).
func GenerateBrowserToken(secret string, duration time.Duration, userID uuid.UUID, sessionID uuid.UUID, role string, systemRole ...string) (string, error) {
	roleClaim := string(domain.SystemRoleUser)
	if len(systemRole) > 0 && systemRole[0] != "" {
		roleClaim = systemRole[0]
	}
	claims := CustomClaims{
		UserID:     userID,
		Role:       role,
		SystemRole: roleClaim,
		Purpose:    PurposeBrowser,
		SessionID:  &sessionID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(duration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ValidateToken validates a JWT token string using the provided secret key.
// It parses the token with custom claims and verifies the HMAC signing method.
// Returns the validated CustomClaims if the token is valid, or an error if validation fails.
// Errors can include invalid signature, malformed token, or parsing errors.
func ValidateToken(tokenStr string, secret string) (*CustomClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &CustomClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrTokenSignatureInvalid
		}
		return []byte(secret), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*CustomClaims); ok && token.Valid {
		return claims, nil
	}
	return nil, jwt.ErrTokenMalformed
}

// GenerateRefreshToken creates a secure random string to be used as a refresh token
func GenerateRefreshToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
