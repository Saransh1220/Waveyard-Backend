package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_Defaults(t *testing.T) {
	// Clear environment
	os.Clearenv()

	// Set required vars
	os.Setenv("DB_HOST", "localhost")
	os.Setenv("DB_PORT", "5432")
	os.Setenv("DB_USER", "user")
	os.Setenv("DB_PASSWORD", "pass")
	os.Setenv("DB_NAME", "test")

	cfg := Load()

	// Verify defaults
	assert.Equal(t, "8080", cfg.Server.Port)
	assert.Equal(t, "http://localhost:4200", cfg.Server.AllowedOrigins)
	assert.Equal(t, "development", cfg.Server.Environment)
	assert.False(t, cfg.Server.SecureCookies)
	assert.True(t, cfg.Server.BrowserSessionsEnabled)
	assert.Equal(t, "default-dev-secret", cfg.JWT.Secret)
	assert.Equal(t, 24*time.Hour, cfg.JWT.Expiry)
	assert.Equal(t, "localhost", cfg.Database.Host)
	assert.Equal(t, "5432", cfg.Database.Port)
	assert.False(t, cfg.Migration.AutoRun)
	assert.Equal(t, "db/migrations", cfg.Migration.Path)
	assert.True(t, cfg.Redis.Enabled)
	assert.Empty(t, cfg.FileStorage.S3PresignEndpoint)
}

func TestConfigHelpers(t *testing.T) {
	t.Setenv("CONFIG_HELPER_VALUE", "")
	assert.Equal(t, "fallback", getEnv("CONFIG_HELPER_VALUE", "fallback"))
	assert.Equal(t, time.Minute, parseDuration("not-a-duration", time.Minute))
	originalFromFile, hadFromFile := os.LookupEnv("FROM_FILE")
	_ = os.Unsetenv("FROM_FILE")
	t.Cleanup(func() {
		if hadFromFile {
			_ = os.Setenv("FROM_FILE", originalFromFile)
		} else {
			_ = os.Unsetenv("FROM_FILE")
		}
	})
	workspace, err := filepath.Abs(".")
	require.NoError(t, err)
	t.Setenv("TEMP", workspace)
	path := filepath.Join(t.TempDir(), "config-helper.test.env")
	require.NoError(t, os.WriteFile(path, []byte("# comment\nFROM_FILE = 'value'\nBROKEN\n"), 0o600))
	loadDotEnvIfPresent(path)
	assert.Equal(t, "value", os.Getenv("FROM_FILE"))
}

func TestLoad_CustomValues(t *testing.T) {
	os.Clearenv()

	// Set custom values
	os.Setenv("PORT", "9000")
	os.Setenv("ALLOWED_ORIGINS", "https://example.com")
	os.Setenv("ENV", "production")
	os.Setenv("JWT_SECRET", "my-secret")
	os.Setenv("JWT_EXPIRATION", "2h")
	os.Setenv("DB_HOST", "db-server")
	os.Setenv("DB_PORT", "15432")
	os.Setenv("DB_USER", "admin")
	os.Setenv("DB_PASSWORD", "secret")
	os.Setenv("DB_NAME", "production")
	os.Setenv("DB_SSLMODE", "require")
	os.Setenv("AUTO_MIGRATE", "true")
	os.Setenv("MIGRATIONS_PATH", "/app/migrations")
	os.Setenv("REDIS_ENABLED", "false")
	os.Setenv("REDIS_HOST", "redis-server")
	os.Setenv("REDIS_PORT", "6380")
	os.Setenv("S3_ENDPOINT", "https://internal.storage.example")
	os.Setenv("S3_PRESIGN_ENDPOINT", "https://signed.storage.example")
	os.Setenv("S3_PUBLIC_ENDPOINT", "https://cdn.example")

	cfg := Load()

	// Verify custom values
	assert.Equal(t, "9000", cfg.Server.Port)
	assert.Equal(t, "https://example.com", cfg.Server.AllowedOrigins)
	assert.Equal(t, "production", cfg.Server.Environment)
	assert.True(t, cfg.Server.SecureCookies)
	assert.Equal(t, "my-secret", cfg.JWT.Secret)
	assert.Equal(t, 2*time.Hour, cfg.JWT.Expiry)
	assert.Equal(t, "db-server", cfg.Database.Host)
	assert.Equal(t, "15432", cfg.Database.Port)
	assert.Equal(t, "admin", cfg.Database.User)
	assert.Equal(t, "secret", cfg.Database.Password)
	assert.Equal(t, "production", cfg.Database.DBName)
	assert.Equal(t, "require", cfg.Database.SSLMode)
	assert.True(t, cfg.Migration.AutoRun)
	assert.Equal(t, "/app/migrations", cfg.Migration.Path)
	assert.False(t, cfg.Redis.Enabled)
	assert.Equal(t, "redis-server", cfg.Redis.Host)
	assert.Equal(t, "6380", cfg.Redis.Port)
	assert.Equal(t, "https://internal.storage.example", cfg.FileStorage.S3Endpoint)
	assert.Equal(t, "https://signed.storage.example", cfg.FileStorage.S3PresignEndpoint)
	assert.Equal(t, "https://cdn.example", cfg.FileStorage.S3PublicEndpoint)
}

func TestLoad_PresignEndpointDefaultsToStorageEndpoint(t *testing.T) {
	os.Clearenv()
	os.Setenv("S3_ENDPOINT", "https://storage.example")

	cfg := Load()

	assert.Equal(t, "https://storage.example", cfg.FileStorage.S3PresignEndpoint)
}

func TestLoad_DodoAndFXConfig(t *testing.T) {
	os.Clearenv()
	os.Setenv("DODO_PAYMENTS_API_KEY", "dodo_key")
	os.Setenv("DODO_PAYMENTS_PRODUCT_ID", "pdt_123")
	os.Setenv("DODO_PAYMENTS_WEBHOOK_KEY", "whsec_123")
	os.Setenv("DODO_PAYMENTS_API_URL", "https://test.dodopayments.com")
	os.Setenv("FX_ENABLED", "true")
	os.Setenv("FX_PROVIDER_URL", "https://api.frankfurter.dev/v1/latest")
	os.Setenv("FX_API_KEY", "fx-key")
	os.Setenv("FX_REFRESH_INTERVAL", "12h")
	os.Setenv("FX_STALE_AFTER", "48h")
	os.Setenv("FX_MAX_AGE", "72h")
	os.Setenv("FX_MAX_DAILY_MOVE", "0.15")
	os.Setenv("FX_REPORTING_CURRENCY", "usd")

	cfg := Load()

	assert.Equal(t, "dodo_key", cfg.Dodo.APIKey)
	assert.Equal(t, "pdt_123", cfg.Dodo.ProductID)
	assert.Equal(t, "whsec_123", cfg.Dodo.WebhookKey)
	assert.Equal(t, "https://test.dodopayments.com", cfg.Dodo.APIURL)
	assert.True(t, cfg.FX.Enabled)
	assert.Equal(t, "https://api.frankfurter.dev/v1/latest", cfg.FX.ProviderURL)
	assert.Equal(t, "fx-key", cfg.FX.APIKey)
	assert.Equal(t, 12*time.Hour, cfg.FX.RefreshInterval)
	assert.Equal(t, 48*time.Hour, cfg.FX.StaleAfter)
	assert.Equal(t, 72*time.Hour, cfg.FX.MaxAge)
	assert.Equal(t, 0.15, cfg.FX.MaxDailyMove)
	assert.Equal(t, "USD", cfg.FX.ReportingCurrency)
}

func TestLoad_JWTExpirationParsing(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected time.Duration
	}{
		{"hours", "48h", 48 * time.Hour},
		{"minutes", "30m", 30 * time.Minute},
		{"mixed", "1h30m", 90 * time.Minute},
		{"invalid_uses_default", "invalid", 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Clearenv()
			os.Setenv("DB_HOST", "localhost")
			os.Setenv("DB_PORT", "5432")
			os.Setenv("DB_USER", "user")
			os.Setenv("DB_PASSWORD", "pass")
			os.Setenv("DB_NAME", "test")
			os.Setenv("JWT_EXPIRATION", tt.value)

			cfg := Load()
			assert.Equal(t, tt.expected, cfg.JWT.Expiry)
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	t.Run("development mode does not fail on missing keys", func(t *testing.T) {
		cfg := &Config{
			Server:     ServerConfig{Environment: "development"},
			AppBaseURL: "http://localhost:4200",
			Dodo:       DodoPaymentsConfig{ReturnURL: "http://localhost:4200/payment/return"},
		}
		assert.NoError(t, cfg.Validate())
	})

	t.Run("production mode requires razorpay credentials", func(t *testing.T) {
		cfg := &Config{
			Server:     ServerConfig{Environment: "production"},
			AppBaseURL: "https://example.com",
			Razorpay:   RazorpayConfig{KeyID: "", KeySecret: "sec", WebhookSecret: "whsec"},
			Dodo:       DodoPaymentsConfig{APIKey: "key", ProductID: "pdt", WebhookKey: "whsec", APIURL: "https://live.dodopayments.com", ReturnURL: "https://example.com/payment/return"},
		}
		assert.ErrorContains(t, cfg.Validate(), "RAZORPAY_KEY_ID")

		cfg.Razorpay.KeyID = "rzp_live_123"
		cfg.Razorpay.WebhookSecret = ""
		assert.ErrorContains(t, cfg.Validate(), "RAZORPAY_WEBHOOK_SECRET")
	})

	t.Run("production mode requires dodo credentials and live url", func(t *testing.T) {
		cfg := &Config{
			Server:     ServerConfig{Environment: "production"},
			AppBaseURL: "https://example.com",
			Razorpay:   RazorpayConfig{KeyID: "key", KeySecret: "sec", WebhookSecret: "whsec"},
			Dodo:       DodoPaymentsConfig{APIKey: "", ProductID: "pdt", WebhookKey: "whsec", APIURL: "https://live.dodopayments.com", ReturnURL: "https://example.com/payment/return"},
		}
		assert.ErrorContains(t, cfg.Validate(), "DODO_PAYMENTS_API_KEY")

		cfg.Dodo.APIKey = "dodo_key"
		cfg.Dodo.APIURL = "https://test.dodopayments.com"
		assert.ErrorContains(t, cfg.Validate(), "DODO_PAYMENTS_API_URL")
	})

	t.Run("return url origin mismatch fails validation", func(t *testing.T) {
		cfg := &Config{
			Server:     ServerConfig{Environment: "production"},
			AppBaseURL: "https://app.example.com",
			Razorpay:   RazorpayConfig{KeyID: "key", KeySecret: "sec", WebhookSecret: "whsec"},
			Dodo:       DodoPaymentsConfig{APIKey: "key", ProductID: "pdt", WebhookKey: "whsec", APIURL: "https://live.dodopayments.com", ReturnURL: "https://evil.example.com/payment/return"},
		}
		assert.ErrorContains(t, cfg.Validate(), "must match APP_BASE_URL origin")
	})

	t.Run("production mode rejects insecure cookies when browser sessions enabled", func(t *testing.T) {
		cfg := &Config{
			JWT: JWTConfig{Secret: "test-configured-secret"},
			Server: ServerConfig{
				Environment:            "production",
				BrowserSessionsEnabled: true,
				SecureCookies:          false,
				AllowedOrigins:         "https://waveyard.studio",
			},
			AppBaseURL: "https://waveyard.studio",
			Razorpay:   RazorpayConfig{KeyID: "rzp_live_123", KeySecret: "rzp_sec_456", WebhookSecret: "whsec_rzp_789"},
			Dodo:       DodoPaymentsConfig{APIKey: "dodo_key_123", ProductID: "pdt_456", WebhookKey: "whsec_dodo_789", APIURL: "https://live.dodopayments.com", ReturnURL: "https://waveyard.studio/payment/return"},
		}
		assert.ErrorContains(t, cfg.Validate(), "SECURE_COOKIES=true")
	})

	t.Run("production mode rejects wildcard origins when browser sessions enabled", func(t *testing.T) {
		cfg := &Config{
			JWT: JWTConfig{Secret: "test-configured-secret"},
			Server: ServerConfig{
				Environment:            "production",
				BrowserSessionsEnabled: true,
				SecureCookies:          true,
				AllowedOrigins:         "*",
			},
			AppBaseURL: "https://waveyard.studio",
			Razorpay:   RazorpayConfig{KeyID: "rzp_live_123", KeySecret: "rzp_sec_456", WebhookSecret: "whsec_rzp_789"},
			Dodo:       DodoPaymentsConfig{APIKey: "dodo_key_123", ProductID: "pdt_456", WebhookKey: "whsec_dodo_789", APIURL: "https://live.dodopayments.com", ReturnURL: "https://waveyard.studio/payment/return"},
		}
		assert.ErrorContains(t, cfg.Validate(), "cannot use wildcard ALLOWED_ORIGINS")
	})

	t.Run("production mode rejects non-https origins", func(t *testing.T) {
		cfg := &Config{
			JWT: JWTConfig{Secret: "test-configured-secret"},
			Server: ServerConfig{
				Environment:            "production",
				BrowserSessionsEnabled: true,
				SecureCookies:          true,
				AllowedOrigins:         "http://evil.com",
			},
			AppBaseURL: "https://waveyard.studio",
			Razorpay:   RazorpayConfig{KeyID: "rzp_live_123", KeySecret: "rzp_sec_456", WebhookSecret: "whsec_rzp_789"},
			Dodo:       DodoPaymentsConfig{APIKey: "dodo_key_123", ProductID: "pdt_456", WebhookKey: "whsec_dodo_789", APIURL: "https://live.dodopayments.com", ReturnURL: "https://waveyard.studio/payment/return"},
		}
		assert.ErrorContains(t, cfg.Validate(), "must be exact https origins")
	})

	t.Run("valid production config passes", func(t *testing.T) {
		cfg := &Config{
			JWT: JWTConfig{Secret: "test-configured-secret"},
			Server: ServerConfig{
				Environment:            "production",
				BrowserSessionsEnabled: true,
				SecureCookies:          true,
				AllowedOrigins:         "https://app.example.com",
			},
			AppBaseURL: "https://app.example.com",
			Razorpay:   RazorpayConfig{KeyID: "rzp_live_123", KeySecret: "rzp_sec_456", WebhookSecret: "whsec_rzp_789"},
			Dodo:       DodoPaymentsConfig{APIKey: "dodo_key_123", ProductID: "pdt_456", WebhookKey: "whsec_dodo_789", APIURL: "https://live.dodopayments.com", ReturnURL: "https://app.example.com/payment/return"},
		}
		assert.NoError(t, cfg.Validate())
	})
}
