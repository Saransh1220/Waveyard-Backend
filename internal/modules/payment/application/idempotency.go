package application

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// ComputeCanonicalFingerprint produces a versioned canonical SHA-256 hash
// over the normalized checkout intent fields.
func ComputeCanonicalFingerprint(input CreateOrderInput) string {
	var parts []string
	parts = append(parts, "v1")
	parts = append(parts, strings.ToLower(input.SpecID.String()))
	parts = append(parts, strings.ToLower(input.LicenseOptionID.String()))
	parts = append(parts, strings.ToUpper(strings.TrimSpace(input.Currency)))

	if input.ExpectedPriceID != nil {
		parts = append(parts, strings.ToLower(input.ExpectedPriceID.String()))
	} else {
		parts = append(parts, "none")
	}

	if input.ExpectedRevision != nil {
		parts = append(parts, fmt.Sprintf("%d", *input.ExpectedRevision))
	} else {
		parts = append(parts, "none")
	}

	if input.ExpectedAmountMinor != nil {
		parts = append(parts, fmt.Sprintf("%d", *input.ExpectedAmountMinor))
	} else {
		parts = append(parts, "none")
	}

	if input.ExpectedCurrency != nil {
		parts = append(parts, strings.ToUpper(strings.TrimSpace(*input.ExpectedCurrency)))
	} else {
		parts = append(parts, "none")
	}

	canonicalString := strings.Join(parts, "|")
	hash := sha256.Sum256([]byte(canonicalString))
	return hex.EncodeToString(hash[:])
}

// NormalizeIdempotencyKey trims and validates an idempotency key string.
// If the key is empty, it returns a generated UUID key.
func NormalizeIdempotencyKey(rawKey string) (string, bool) {
	trimmed := strings.TrimSpace(rawKey)
	if trimmed == "" {
		id, err := uuid.NewV7()
		if err != nil {
			id = uuid.New()
		}
		return id.String(), false
	}
	// Limit to 128 characters
	if len(trimmed) > 128 {
		trimmed = trimmed[:128]
	}
	return trimmed, true
}
