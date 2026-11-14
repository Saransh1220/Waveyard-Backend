package config

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/saransh1220/blueprint-audio/internal/shared/infrastructure/database"
)

// Config holds all configuration for the application
type Config struct {
	Server      ServerConfig
	Database    database.PostgresConfig
	Migration   MigrationConfig
	Bootstrap   BootstrapConfig
	Redis       database.RedisConfig
	JWT         JWTConfig
	Razorpay    RazorpayConfig
	Dodo        DodoPaymentsConfig
	Currency    CurrencyConfig
	FX          FXConfig
	FileStorage FileStorageConfig
	Google      GoogleConfig
	Email       EmailConfig
	Worker      WorkerConfig
	AppBaseURL  string
}

// WorkerConfig holds media processor worker configuration
type WorkerConfig struct {
	Enabled       bool
	ID            string
	PollInterval  time.Duration
	LeaseDuration time.Duration
}

// GoogleConfig holds Google OAuth configuration
type GoogleConfig struct {
	ClientID string
}

type EmailConfig struct {
	ResendAPIKey string
	From         string
	ReplyTo      string
	Enabled      bool
}

// ServerConfig holds server configuration
type ServerConfig struct {
	Port                   string
	AllowedOrigins         string
	Environment            string
	SecureCookies          bool
	APIDocsEnabled         bool
	BrowserSessionsEnabled bool
}

// JWTConfig holds JWT configuration
type JWTConfig struct {
	Secret        string
	Expiry        time.Duration
	RefreshExpiry time.Duration
}

// RazorpayConfig holds Razorpay payment gateway configuration
type RazorpayConfig struct {
	KeyID         string
	KeySecret     string
	WebhookSecret string
}

type DodoPaymentsConfig struct {
	APIKey     string
	ProductID  string
	WebhookKey string
	APIURL     string
	ReturnURL  string
}

type CurrencyConfig struct{}

type FXConfig struct {
	Enabled           bool
	ProviderURL       string
	APIKey            string
	RefreshInterval   time.Duration
	StaleAfter        time.Duration
	MaxAge            time.Duration
	MaxDailyMove      float64
	ReportingCurrency string
}

// FileStorageConfig holds file storage configuration
type FileStorageConfig struct {
	UseS3             bool
	S3Region          string
	S3Endpoint        string
	S3PresignEndpoint string
	S3PublicEndpoint  string
	S3AccessKey       string
	S3SecretKey       string
	S3BucketName      string
	S3UseSSL          bool
	LocalPath         string
}

type MigrationConfig struct {
	AutoRun bool
	Path    string
}

type BootstrapConfig struct {
	SuperAdminEmail string
}

// Load reads configuration from environment variables
func Load() Config {
	loadDotEnvIfPresent(".env")
	environment := strings.ToLower(getEnv("ENV", "development"))

	return Config{
		Server: ServerConfig{
			Port:                   getEnv("PORT", "8080"),
			AllowedOrigins:         getEnv("ALLOWED_ORIGINS", "http://localhost:4200"),
			Environment:            environment,
			SecureCookies:          environment == "production",
			APIDocsEnabled:         getEnv("API_DOCS_ENABLED", "true") == "true",
			BrowserSessionsEnabled: getEnv("AUTH_BROWSER_SESSIONS_ENABLED", "true") == "true",
		},
		Database: database.PostgresConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnv("DB_PORT", "5432"),
			User:     getEnv("DB_USER", "postgres"),
			Password: getEnv("DB_PASSWORD", ""),
			DBName:   getEnv("DB_NAME", "blueprint"),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
		},
		Migration: MigrationConfig{
			AutoRun: getEnv("AUTO_MIGRATE", "false") == "true",
			Path:    getEnv("MIGRATIONS_PATH", "db/migrations"),
		},
		Bootstrap: BootstrapConfig{
			SuperAdminEmail: getEnv("SUPER_ADMIN_EMAIL", ""),
		},
		Redis: database.RedisConfig{
			Enabled:  getEnv("REDIS_ENABLED", "true") == "true",
			Host:     getEnv("REDIS_HOST", "localhost"),
			Port:     getEnv("REDIS_PORT", "6379"),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       0,
		},
		JWT: JWTConfig{
			Secret:        getEnv("JWT_SECRET", "default-dev-secret"),
			Expiry:        parseDuration(getEnv("JWT_EXPIRATION", "24h"), 24*time.Hour),
			RefreshExpiry: parseDuration(getEnv("JWT_REFRESH_EXPIRATION", "720h"), 30*24*time.Hour),
		},
		Razorpay: RazorpayConfig{
			KeyID:         getEnv("RAZORPAY_KEY_ID", ""),
			KeySecret:     getEnv("RAZORPAY_KEY_SECRET", ""),
			WebhookSecret: getEnv("RAZORPAY_WEBHOOK_SECRET", ""),
		},
		Dodo: DodoPaymentsConfig{
			APIKey:     getEnv("DODO_PAYMENTS_API_KEY", ""),
			ProductID:  getEnv("DODO_PAYMENTS_PRODUCT_ID", ""),
			WebhookKey: getEnv("DODO_PAYMENTS_WEBHOOK_KEY", ""),
			APIURL:     getEnv("DODO_PAYMENTS_API_URL", ""),
			ReturnURL:  getEnv("DODO_RETURN_URL", ""),
		},
		Currency: CurrencyConfig{},
		FX: FXConfig{
			Enabled:           getEnv("FX_ENABLED", "false") == "true",
			ProviderURL:       getEnv("FX_PROVIDER_URL", "https://api.frankfurter.dev/v1/latest"),
			APIKey:            getEnv("FX_API_KEY", ""),
			RefreshInterval:   parseDuration(getEnv("FX_REFRESH_INTERVAL", "24h"), 24*time.Hour),
			StaleAfter:        parseDuration(getEnv("FX_STALE_AFTER", "36h"), 36*time.Hour),
			MaxAge:            parseDuration(getEnv("FX_MAX_AGE", "168h"), 168*time.Hour),
			MaxDailyMove:      parseFloat(getEnv("FX_MAX_DAILY_MOVE", "0.20"), 0.20),
			ReportingCurrency: strings.ToUpper(getEnv("FX_REPORTING_CURRENCY", "INR")),
		},
		FileStorage: FileStorageConfig{
			UseS3:             getEnv("USE_S3", "true") == "true",
			S3Region:          getEnv("S3_REGION", "us-east-1"),
			S3Endpoint:        getEnv("S3_ENDPOINT", ""),
			S3PresignEndpoint: getEnv("S3_PRESIGN_ENDPOINT", getEnv("S3_ENDPOINT", "")),
			S3PublicEndpoint:  getEnv("S3_PUBLIC_ENDPOINT", getEnv("S3_ENDPOINT", "")),
			S3AccessKey:       getEnv("S3_ACCESS_KEY", ""),
			S3SecretKey:       getEnv("S3_SECRET_KEY", ""),
			S3BucketName:      getEnv("S3_BUCKET", ""),
			S3UseSSL:          getEnv("S3_USE_SSL", "true") == "true",
			LocalPath:         getEnv("LOCAL_STORAGE_PATH", "./uploads"),
		},
		Google: GoogleConfig{
			ClientID: getEnv("GOOGLE_CLIENT_ID", ""),
		},
		Email: EmailConfig{
			ResendAPIKey: getEnv("RESEND_API_KEY", ""),
			From:         getEnv("EMAIL_FROM", ""),
			ReplyTo:      getEnv("EMAIL_REPLY_TO", ""),
			Enabled:      getEnv("EMAIL_ENABLED", "true") == "true",
		},
		Worker: WorkerConfig{
			Enabled:       getEnv("WORKER_ENABLED", "true") == "true",
			ID:            getEnv("WORKER_ID", ""),
			PollInterval:  parseDuration(getEnv("WORKER_POLL_INTERVAL", "2s"), 2*time.Second),
			LeaseDuration: parseDuration(getEnv("WORKER_LEASE_DURATION", "30m"), 30*time.Minute),
		},
		AppBaseURL: getEnv("APP_BASE_URL", "http://localhost:4200"),
	}
}

// Validate checks critical configuration settings based on environment.
func (c *Config) Validate() error {
	isProd := strings.EqualFold(c.Server.Environment, "production")

	// Validate ReturnURL origin if provided
	if c.Dodo.ReturnURL != "" {
		dodoURL, err := url.Parse(c.Dodo.ReturnURL)
		if err != nil {
			return fmt.Errorf("invalid DODO_RETURN_URL: %w", err)
		}
		appURL, err := url.Parse(c.AppBaseURL)
		if err != nil {
			return fmt.Errorf("invalid APP_BASE_URL: %w", err)
		}
		if dodoURL.Scheme != appURL.Scheme || dodoURL.Host != appURL.Host {
			return fmt.Errorf("DODO_RETURN_URL origin (%s://%s) must match APP_BASE_URL origin (%s://%s)",
				dodoURL.Scheme, dodoURL.Host, appURL.Scheme, appURL.Host)
		}
	}

	if isProd {
		if c.Server.BrowserSessionsEnabled {
			if !c.Server.SecureCookies {
				return errors.New("production environment requires SECURE_COOKIES=true when browser sessions are enabled")
			}
			if c.Server.AllowedOrigins == "*" || strings.Contains(c.Server.AllowedOrigins, "*") {
				return errors.New("production environment cannot use wildcard ALLOWED_ORIGINS when browser sessions are enabled")
			}
			origins := strings.Split(c.Server.AllowedOrigins, ",")
			for _, raw := range origins {
				raw = strings.TrimSpace(raw)
				if raw == "" {
					continue
				}
				parsed, err := url.Parse(raw)
				if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" {
					return fmt.Errorf("production ALLOWED_ORIGINS must be exact https origins, got: %s", raw)
				}
			}
		}
		if c.Razorpay.KeyID == "" || c.Razorpay.KeySecret == "" || c.Razorpay.WebhookSecret == "" {
			return errors.New("production environment requires RAZORPAY_KEY_ID, RAZORPAY_KEY_SECRET, and RAZORPAY_WEBHOOK_SECRET")
		}
		if c.Dodo.APIKey == "" || c.Dodo.ProductID == "" || c.Dodo.WebhookKey == "" || c.Dodo.APIURL == "" || strings.Contains(c.Dodo.APIURL, "test.dodopayments.com") {
			return errors.New("production environment requires DODO_PAYMENTS_API_KEY, DODO_PAYMENTS_PRODUCT_ID, DODO_PAYMENTS_WEBHOOK_KEY, and explicit live DODO_PAYMENTS_API_URL")
		}
	} else {
		if c.Razorpay.KeyID == "" || c.Razorpay.KeySecret == "" || c.Razorpay.WebhookSecret == "" {
			log.Printf("[CONFIG WARNING] Razorpay credentials or webhook secret not fully configured")
		}
		if c.Dodo.APIKey == "" || c.Dodo.ProductID == "" || c.Dodo.WebhookKey == "" {
			log.Printf("[CONFIG WARNING] Dodo Payments credentials not fully configured")
		}
	}

	return nil
}

// getEnv reads an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func loadDotEnvIfPresent(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" || os.Getenv(key) != "" {
			continue
		}
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		_ = os.Setenv(key, value)
	}
	if err := scanner.Err(); err != nil {
		log.Printf("failed to scan env file %s: %v", path, err)
	}
}

// parseDuration parses a duration string or returns a default value
func parseDuration(value string, defaultValue time.Duration) time.Duration {
	if duration, err := time.ParseDuration(value); err == nil {
		return duration
	}
	return defaultValue
}

func parseFloat(value string, defaultValue float64) float64 {
	if val, err := strconv.ParseFloat(value, 64); err == nil && val > 0 {
		return val
	}
	return defaultValue
}
