package main

import (
	"context"
	"log"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/saransh1220/blueprint-audio/internal/gateway"
	gatewayMiddleware "github.com/saransh1220/blueprint-audio/internal/gateway/middleware"
	"github.com/saransh1220/blueprint-audio/internal/gateway/openapi"
	"github.com/saransh1220/blueprint-audio/internal/modules/admin"
	"github.com/saransh1220/blueprint-audio/internal/modules/analytics"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth"
	"github.com/saransh1220/blueprint-audio/internal/modules/catalog"
	catalogApplication "github.com/saransh1220/blueprint-audio/internal/modules/catalog/application"
	catalogPersistence "github.com/saransh1220/blueprint-audio/internal/modules/catalog/infrastructure/persistence/postgres"
	"github.com/saransh1220/blueprint-audio/internal/modules/filestorage"
	"github.com/saransh1220/blueprint-audio/internal/modules/fx"
	"github.com/saransh1220/blueprint-audio/internal/modules/legal"
	"github.com/saransh1220/blueprint-audio/internal/modules/notification"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment"
	paymentApplication "github.com/saransh1220/blueprint-audio/internal/modules/payment/application"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/infrastructure/dodo"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/infrastructure/razorpay"
	"github.com/saransh1220/blueprint-audio/internal/modules/user"
	"github.com/saransh1220/blueprint-audio/internal/shared/infrastructure/config"
	"github.com/saransh1220/blueprint-audio/internal/shared/infrastructure/database"
	sharedemail "github.com/saransh1220/blueprint-audio/internal/shared/infrastructure/email"
	"github.com/saransh1220/blueprint-audio/pkg/migration"
)

func main() {
	// 1. Load Configuration
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("Configuration validation failed: %v", err)
	}

	if cfg.Migration.AutoRun {
		logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
		if err := migration.AutoMigrate(postgresURL(cfg.Database), cfg.Migration.Path, logger); err != nil {
			log.Fatalf("Failed to run database migrations: %v", err)
		}
	}

	// 2. Database Connection
	db, err := database.NewPostgresDB(cfg.Database)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	var redisClient *redis.Client
	if cfg.Redis.Enabled {
		// 3. Redis Connection
		redisClient = redis.NewClient(&redis.Options{
			Addr:     cfg.Redis.Host + ":" + cfg.Redis.Port,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})
		if err := redisClient.Ping(ctx).Err(); err != nil {
			log.Printf("Warning: Failed to connect to Redis; cache disabled: %v", err)
			redisClient.Close()
			redisClient = nil
		} else {
			defer redisClient.Close()
		}
	} else {
		log.Printf("Redis disabled; running without cache")
	}

	// 4. Initialize Modules
	if cfg.Email.Enabled && (strings.TrimSpace(cfg.Email.ResendAPIKey) == "" || strings.TrimSpace(cfg.Email.From) == "") {
		log.Fatal("Email is enabled but RESEND_API_KEY or EMAIL_FROM is missing")
	}

	emailSender := sharedemail.NewSender(sharedemail.Config{
		APIKey:  cfg.Email.ResendAPIKey,
		From:    cfg.Email.From,
		ReplyTo: cfg.Email.ReplyTo,
		Enabled: cfg.Email.Enabled,
	})

	// Filestorage Module
	fsModule, err := filestorage.NewModule(ctx, cfg.FileStorage)
	if err != nil {
		log.Fatalf("Failed to initialize filestorage module: %v", err)
	}

	// Auth Module
	authModule, err := auth.NewModule(db, cfg.JWT.Secret, cfg.JWT.Expiry, cfg.JWT.RefreshExpiry, fsModule.Service(), cfg.Google.ClientID, cfg.Server.SecureCookies, emailSender, cfg.AppBaseURL)
	if err != nil {
		log.Fatalf("Failed to initialize auth module: %v", err)
	}
	if strings.TrimSpace(cfg.Bootstrap.SuperAdminEmail) != "" {
		if err := authModule.UserRepository().BootstrapSuperAdmin(ctx, cfg.Bootstrap.SuperAdminEmail); err != nil {
			log.Fatalf("Failed to bootstrap super admin: %v", err)
		}
		log.Printf("Super admin bootstrap checked for configured email")
	}

	// Roles & permissions (database-driven RBAC)
	// Roles and their permissions are data (the roles table). Load them now,
	// refuse to start without them, and refresh periodically so a permission
	// granted by migration applies without a restart.
	roleStore := gatewayMiddleware.NewRoleStore(gatewayMiddleware.NewPgRoleLoader(db))
	if err := roleStore.Reload(ctx); err != nil {
		log.Fatalf("Failed to load roles: %v", err)
	}
	roleStore.Start(ctx, 30*time.Second)

	// User Module
	userModule := user.NewModule(authModule.UserRepository(), fsModule.Service()).WithSecureCookie(cfg.Server.SecureCookies)
	adminModule := admin.NewModule(db, authModule.UserRepository(), roleStore)

	// Notification Module
	notificationModule := notification.NewModule(db)

	// Catalog Module Prerequisites
	// We need to instantiate the SpecRepository explicitly to share it between Catalog and Analytics
	specRepo := catalogPersistence.NewSpecRepository(db)

	// Analytics Module (Likely needs SpecRepo)
	analyticsModule := analytics.NewModule(db, specRepo, fsModule.Service())

	// Catalog Module
	catalogModule := catalog.NewModule(db, specRepo, fsModule.Service(), analyticsModule.AnalyticsService, notificationModule.Service(), redisClient)

	// Payment Module
	paymentModule := payment.NewModule(db, catalogModule.SpecFinder(), authModule.UserFinder(), fsModule.Service(), emailSender, cfg.AppBaseURL, paymentGatewayConfig(cfg))

	// FX Module
	fxModule := fx.NewModule(db, cfg.FX, adminModule.AuditWriter())
	paymentModule.SetSnapshotter(fxModule.Snapshotter())
	fxModule.StartRefresher(ctx)

	// Legal Module
	legalModule := legal.NewModule(db)

	// 5. Middleware
	// Re-check account status and system role against the database (cached
	// for a few seconds) so suspensions and demotions apply immediately
	// instead of when the access token expires.
	userRepo := authModule.UserRepository()

	authMiddleware := gatewayMiddleware.NewAuthMiddleware(cfg.JWT.Secret).WithRoleStore(roleStore).WithAccountLookup(
		func(ctx context.Context, id uuid.UUID) (gatewayMiddleware.AccountState, error) {
			u, err := userRepo.GetByID(ctx, id)
			if err != nil {
				return gatewayMiddleware.AccountState{}, err
			}
			return gatewayMiddleware.AccountState{Status: u.Status, Role: u.Role, SystemRole: u.SystemRole}, nil
		},
		10*time.Second,
	)

	// Favorites strict server (OpenAPI-generated contract implementation)
	favoritesServer := openapi.NewFavoritesServer(analyticsModule.AnalyticsService)

	buyerCurrencyMiddleware := gatewayMiddleware.NewBuyerCurrencyMiddleware(
		func(ctx context.Context, id uuid.UUID) (*string, error) {
			return userRepo.GetPreferredCurrency(ctx, id)
		},
		true,
	)

	// 6. Setup Routes
	mux := gateway.SetupRoutes(gateway.RouterConfig{
		AuthHandler:             authModule.HTTPHandler(),
		AuthMiddleware:          authMiddleware,
		BuyerCurrencyMiddleware: buyerCurrencyMiddleware,
		SpecHandler:             catalogModule.HTTPHandler(),
		SpecUploadHandler:       catalogModule.UploadHTTPHandler(),
		UserHandler:             userModule.HTTPHandler(),
		PaymentHandler:          paymentModule.HTTPHandler(),
		CurrencyGatewayNames:    paymentModule.CurrencyGatewayNames(),
		AnalyticsHandler:        analyticsModule.AnalyticsHandler,
		NotificationHandler:     notificationModule.HTTPHandler(),
		AdminHandler:            adminModule.HTTPHandler(),
		FXHandler:               fxModule.Handler(),
		LegalHandler:            legalModule.HTTPHandler(),
		FavoritesServer:         favoritesServer,
		DisableAPIDocs:          !cfg.Server.APIDocsEnabled,
	})

	// 7. Apply Middleware
	handler := gatewayMiddleware.CORSMiddleware(mux, cfg.Server.AllowedOrigins)
	handler = gatewayMiddleware.PrometheusMiddleware(handler)
	handler = gatewayMiddleware.RequestLoggerMiddleware(handler)
	handler = gatewayMiddleware.TraceMiddleware(handler)

	// 8. Start Embedded Worker (if enabled)
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()
	go paymentApplication.StartWebhookWorker(workerCtx, paymentModule.Service(), paymentApplication.WebhookWorkerConfig{})

	if cfg.Worker.Enabled {
		uploadRepo := catalogPersistence.NewSpecUploadRepository(db)
		processor := catalogApplication.NewSpecUploadProcessor(uploadRepo, fsModule.Service(), notificationModule.Service())
		go catalogApplication.StartUploadWorker(workerCtx, processor, cfg.Worker)
	}

	// 9. Start Server
	srv := gateway.NewServer(cfg.Server.Port, handler)
	if err := srv.Start(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func paymentGatewayConfig(cfg config.Config) payment.GatewayConfig {
	return payment.GatewayConfig{
		Razorpay: razorpay.Config{
			KeyID:         cfg.Razorpay.KeyID,
			KeySecret:     cfg.Razorpay.KeySecret,
			WebhookSecret: cfg.Razorpay.WebhookSecret,
		},
		Dodo: dodo.Config{
			APIKey:     cfg.Dodo.APIKey,
			ProductID:  cfg.Dodo.ProductID,
			WebhookKey: cfg.Dodo.WebhookKey,
			APIURL:     cfg.Dodo.APIURL,
			ReturnURL:  cfg.Dodo.ReturnURL,
		},
	}
}

func postgresURL(cfg database.PostgresConfig) string {
	u := &url.URL{
		Scheme: "postgresql",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   net.JoinHostPort(cfg.Host, cfg.Port),
		Path:   "/" + cfg.DBName,
	}

	q := u.Query()
	q.Set("sslmode", cfg.SSLMode)
	u.RawQuery = q.Encode()

	return u.String()
}
