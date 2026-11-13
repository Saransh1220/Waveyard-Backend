package application

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeIdempotencyKey(t *testing.T) {
	t.Run("trims provided key", func(t *testing.T) {
		k, clientProvided := NormalizeIdempotencyKey("  user-key-123  ")
		assert.Equal(t, "user-key-123", k)
		assert.True(t, clientProvided)
	})

	t.Run("generates uuid when empty", func(t *testing.T) {
		k, clientProvided := NormalizeIdempotencyKey("")
		assert.NotEmpty(t, k)
		assert.False(t, clientProvided)
		_, err := uuid.Parse(k)
		assert.NoError(t, err)
	})

	t.Run("generates uuid when whitespace only", func(t *testing.T) {
		k, clientProvided := NormalizeIdempotencyKey("   \t  ")
		assert.NotEmpty(t, k)
		assert.False(t, clientProvided)
	})
}

func TestComputeCanonicalFingerprint(t *testing.T) {
	uID := uuid.New()
	sID := uuid.New()
	loID := uuid.New()
	priceID := uuid.New()
	rev := int64(3)
	amount := 1500
	curr := "USD"

	base := CreateOrderInput{
		UserID:              uID,
		SpecID:              sID,
		LicenseOptionID:     loID,
		Currency:            "usd",
		ExpectedPriceID:     &priceID,
		ExpectedRevision:    &rev,
		ExpectedAmountMinor: &amount,
		ExpectedCurrency:    &curr,
	}

	h1 := ComputeCanonicalFingerprint(base)
	require.NotEmpty(t, h1)

	t.Run("identical input produces identical hash", func(t *testing.T) {
		h2 := ComputeCanonicalFingerprint(base)
		assert.Equal(t, h1, h2)
	})

	t.Run("currency casing and whitespace normalized", func(t *testing.T) {
		variant := base
		variant.Currency = "  USD  "
		cUpper := "  usd "
		variant.ExpectedCurrency = &cUpper
		assert.Equal(t, h1, ComputeCanonicalFingerprint(variant))
	})

	t.Run("different amount changes hash", func(t *testing.T) {
		otherAmount := 2000
		variant := base
		variant.ExpectedAmountMinor = &otherAmount
		assert.NotEqual(t, h1, ComputeCanonicalFingerprint(variant))
	})

	t.Run("different price ID changes hash", func(t *testing.T) {
		otherPriceID := uuid.New()
		variant := base
		variant.ExpectedPriceID = &otherPriceID
		assert.NotEqual(t, h1, ComputeCanonicalFingerprint(variant))
	})

	t.Run("missing optional fields produce different hash from present fields", func(t *testing.T) {
		variant := base
		variant.ExpectedRevision = nil
		assert.NotEqual(t, h1, ComputeCanonicalFingerprint(variant))
	})
}
