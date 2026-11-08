package gateway

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/saransh1220/blueprint-audio/internal/gateway/apidocs"
	"github.com/saransh1220/blueprint-audio/internal/gateway/middleware"
	"github.com/saransh1220/blueprint-audio/internal/gateway/openapi"
	admin_http "github.com/saransh1220/blueprint-audio/internal/modules/admin/interfaces/http"
	analytics_http "github.com/saransh1220/blueprint-audio/internal/modules/analytics/interfaces/http"
	auth_http "github.com/saransh1220/blueprint-audio/internal/modules/auth/interfaces/http"
	catalog_http "github.com/saransh1220/blueprint-audio/internal/modules/catalog/interfaces/http"
	fx_http "github.com/saransh1220/blueprint-audio/internal/modules/fx/interfaces/http"
	legal_http "github.com/saransh1220/blueprint-audio/internal/modules/legal/interfaces/http"
	notification_http "github.com/saransh1220/blueprint-audio/internal/modules/notification/interfaces/http"
	payment_http "github.com/saransh1220/blueprint-audio/internal/modules/payment/interfaces/http"
	user_http "github.com/saransh1220/blueprint-audio/internal/modules/user/interfaces/http"
)

// RouterConfig holds all the handlers and middleware needed for routing
type RouterConfig struct {
	AuthHandler             *auth_http.AuthHandler
	AuthMiddleware          *middleware.AuthMiddleWare
	BuyerCurrencyMiddleware *middleware.BuyerCurrencyMiddleware
	SpecHandler             *catalog_http.SpecHandler
	SpecUploadHandler       *catalog_http.SpecUploadHandler
	UserHandler             *user_http.UserHandler
	PaymentHandler          *payment_http.PaymentHandler
	AnalyticsHandler        *analytics_http.AnalyticsHandler
	NotificationHandler     *notification_http.NotificationHandler
	AdminHandler            *admin_http.AdminHandler
	LegalHandler            *legal_http.LegalHandler
	FXHandler               *fx_http.FXHandler
	// FavoritesServer implements the oapi-codegen StrictServerInterface for /me/favorites.
	FavoritesServer      *openapi.FavoritesServer
	DisableAPIDocs       bool
	CurrencyGatewayNames map[string]string
}

// SetupRoutes creates and configures all application routes
func SetupRoutes(config RouterConfig) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /config/currencies", currencyConfiguration(config.CurrencyGatewayNames))

	buyerCurrency := func(h http.Handler) http.Handler {
		if config.BuyerCurrencyMiddleware != nil {
			return config.BuyerCurrencyMiddleware.Middleware(h)
		}
		return h
	}

	// Health Check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Prometheus Metrics Endpoint
	mux.Handle("GET /metrics", config.AuthMiddleware.Authorize(middleware.PermAdminAnalyticsRead)(promhttp.Handler()))

	if !config.DisableAPIDocs {
		apidocs.Register(mux)
	}

	// --- OpenAPI-generated routes (strict server) ---
	// GET /me/favorites — requires authentication.
	// RequireAuth rejects unauthenticated requests before the handler is reached.
	// FavoritesServer additionally validates the userID from context as a second
	// line of defence (defence-in-depth).
	if config.FavoritesServer != nil {
		// The generated Handler wraps ServerInterface in an http.Handler.
		// We use HandlerWithOptions so we can supply the auth middleware and our
		// existing mux as the base router — the generator registers its own routes
		// directly onto the mux.
		authMiddlewareFunc := openapi.MiddlewareFunc(config.AuthMiddleware.RequireAuth)
		var favoritesMiddlewares []openapi.MiddlewareFunc
		favoritesMiddlewares = append(favoritesMiddlewares, authMiddlewareFunc)
		if config.BuyerCurrencyMiddleware != nil {
			favoritesMiddlewares = append(favoritesMiddlewares, openapi.MiddlewareFunc(config.BuyerCurrencyMiddleware.Middleware))
		}
		openapi.HandlerWithOptions(
			openapi.NewStrictHandler(config.FavoritesServer, nil),
			openapi.StdHTTPServerOptions{
				BaseRouter:  mux,
				Middlewares: favoritesMiddlewares,
			},
		)
	}

	// Auth Routes
	loginLimiter := middleware.RateLimitMiddleware(20, 15*time.Minute)
	registerLimiter := middleware.RateLimitMiddleware(5, 15*time.Minute)
	refreshLimiter := middleware.RateLimitMiddleware(60, time.Minute)
	playLimiter := middleware.RateLimitMiddleware(120, time.Minute)
	uploadLimiter := middleware.RateLimitMiddleware(60, time.Minute)
	tickets := middleware.NewWebSocketTickets(config.AuthMiddleware)
	emailActionLimiter := middleware.RateLimitMiddleware(3, 15*time.Minute)

	mux.HandleFunc("POST /register", registerLimiter(config.AuthHandler.Register))
	mux.HandleFunc("POST /login", loginLimiter(config.AuthHandler.Login))
	mux.HandleFunc("POST /auth/google", loginLimiter(config.AuthHandler.GoogleLogin))
	mux.HandleFunc("POST /auth/refresh", refreshLimiter(config.AuthHandler.Refresh))
	mux.HandleFunc("POST /auth/logout", config.AuthHandler.Logout)
	mux.HandleFunc("POST /auth/verify-email", emailActionLimiter(config.AuthHandler.VerifyEmail))
	mux.HandleFunc("POST /auth/resend-verification", emailActionLimiter(config.AuthHandler.ResendVerification))
	mux.HandleFunc("POST /auth/forgot-password", emailActionLimiter(config.AuthHandler.ForgotPassword))
	mux.HandleFunc("POST /auth/reset-password", emailActionLimiter(config.AuthHandler.ResetPassword))
	mux.Handle("GET /me", config.AuthMiddleware.RequireAuth(http.HandlerFunc(config.AuthHandler.Me)))

	// authz authenticates, then requires the listed permissions. Which roles hold
	// a permission is data in the roles table (see middleware/permissions.go).
	authz := config.AuthMiddleware.Authorize

	// Catalog/Spec Routes
	mux.Handle("GET /catalog/home", config.AuthMiddleware.FlexibleAuth(buyerCurrency(http.HandlerFunc(config.SpecHandler.Home))))
	mux.Handle("GET /specs", config.AuthMiddleware.FlexibleAuth(buyerCurrency(http.HandlerFunc(config.SpecHandler.List))))
	mux.Handle("GET /specs/{id}", config.AuthMiddleware.FlexibleAuth(buyerCurrency(http.HandlerFunc(config.SpecHandler.Get))))
	mux.Handle("POST /specs", config.AuthMiddleware.RequireAuth(http.HandlerFunc(config.SpecHandler.CreateGone)))
	if config.SpecUploadHandler != nil {
		mux.Handle("POST /spec-uploads", authz(middleware.PermSpecUpload)(buyerCurrency(http.HandlerFunc(uploadLimiter(config.SpecUploadHandler.Initiate)))))
		mux.Handle("PUT /spec-uploads/{id}/metadata", authz(middleware.PermSpecUpload)(http.HandlerFunc(uploadLimiter(config.SpecUploadHandler.SaveMetadata))))
		mux.Handle("POST /spec-uploads/{id}/files", authz(middleware.PermSpecUpload)(http.HandlerFunc(uploadLimiter(config.SpecUploadHandler.PrepareFile))))
		mux.Handle("POST /spec-uploads/{id}/files/{assetID}/complete", authz(middleware.PermSpecUpload)(http.HandlerFunc(uploadLimiter(config.SpecUploadHandler.ConfirmFile))))
		mux.Handle("POST /spec-uploads/{id}/complete", authz(middleware.PermSpecUpload)(buyerCurrency(http.HandlerFunc(uploadLimiter(config.SpecUploadHandler.Complete)))))
		mux.Handle("GET /spec-uploads/{id}", authz(middleware.PermSpecUpload)(http.HandlerFunc(uploadLimiter(config.SpecUploadHandler.Status))))
	}
	mux.Handle("PATCH /specs/{id}", config.AuthMiddleware.RequireAuth(http.HandlerFunc(config.SpecHandler.Update)))
	mux.Handle("DELETE /specs/{id}", config.AuthMiddleware.RequireAuth(http.HandlerFunc(config.SpecHandler.Delete)))
	mux.Handle("POST /specs/{id}/download-free", config.AuthMiddleware.RequireAuth(http.HandlerFunc(config.SpecHandler.DownloadFree)))

	// User Routes
	mux.HandleFunc("GET /producers/{handle}", config.UserHandler.GetPublicProfileByHandle)
	mux.Handle("PATCH /users/profile", config.AuthMiddleware.RequireAuth(http.HandlerFunc(config.UserHandler.UpdateProfile)))
	mux.Handle("POST /users/profile/avatar", config.AuthMiddleware.RequireAuth(http.HandlerFunc(config.UserHandler.UploadAvatar)))
	mux.Handle("POST /users/profile/banner", config.AuthMiddleware.RequireAuth(http.HandlerFunc(config.UserHandler.UploadBanner)))
	mux.HandleFunc("GET /users/{id}/public", config.UserHandler.GetPublicProfile)
	mux.Handle("GET /users/{id}/specs", config.AuthMiddleware.FlexibleAuth(buyerCurrency(http.HandlerFunc(config.SpecHandler.GetUserSpecs))))

	// Buyer Currency Preferences
	preferenceLimiter := middleware.RateLimitMiddleware(60, time.Minute)
	mux.Handle("GET /preferences/currency", config.AuthMiddleware.FlexibleAuth(http.HandlerFunc(config.UserHandler.GetCurrencyPreference)))
	mux.Handle("PUT /preferences/currency", config.AuthMiddleware.FlexibleAuth(http.HandlerFunc(preferenceLimiter(config.UserHandler.SetCurrencyPreference))))

	// Payment Routes
	mux.Handle("POST /orders", config.AuthMiddleware.RequireAuth(buyerCurrency(http.HandlerFunc(config.PaymentHandler.CreateOrder))))
	mux.Handle("GET /orders", config.AuthMiddleware.RequireAuth(buyerCurrency(http.HandlerFunc(config.PaymentHandler.GetUserOrders))))
	mux.Handle("GET /orders/{id}", config.AuthMiddleware.RequireAuth(buyerCurrency(http.HandlerFunc(config.PaymentHandler.GetOrder))))
	mux.Handle("GET /orders/{id}/status", config.AuthMiddleware.RequireAuth(buyerCurrency(http.HandlerFunc(config.PaymentHandler.GetOrderStatus))))
	mux.Handle("POST /payments/verify", config.AuthMiddleware.RequireAuth(http.HandlerFunc(config.PaymentHandler.VerifyPayment)))
	mux.HandleFunc("POST /webhooks/{provider}", config.PaymentHandler.ProviderWebhook)
	mux.HandleFunc("POST /webhooks/dodo", config.PaymentHandler.DodoWebhook)
	mux.Handle("GET /licenses", config.AuthMiddleware.RequireAuth(http.HandlerFunc(config.PaymentHandler.GetUserLicenses)))
	mux.Handle("GET /licenses/{id}/downloads", config.AuthMiddleware.RequireAuth(http.HandlerFunc(config.PaymentHandler.GetLicenseDownloads)))
	mux.Handle("GET /orders/producer", authz(middleware.PermOrdersProducer)(http.HandlerFunc(config.PaymentHandler.GetProducerOrders)))

	// Notification Routes
	mux.Handle("GET /notifications", config.AuthMiddleware.RequireAuth(http.HandlerFunc(config.NotificationHandler.ListNotifications)))
	mux.Handle("PATCH /notifications/{id}/read", config.AuthMiddleware.RequireAuth(http.HandlerFunc(config.NotificationHandler.MarkAsRead)))
	mux.Handle("PATCH /notifications/read-all", config.AuthMiddleware.RequireAuth(http.HandlerFunc(config.NotificationHandler.MarkAllAsRead)))
	mux.Handle("GET /notifications/unread-count", config.AuthMiddleware.RequireAuth(http.HandlerFunc(config.NotificationHandler.UnreadCount)))
	mux.Handle("POST /ws/ticket", config.AuthMiddleware.RequireAuth(http.HandlerFunc(refreshLimiter(tickets.Issue))))
	mux.Handle("GET /ws", tickets.Authenticate(http.HandlerFunc(config.NotificationHandler.Subscribe)))

	// Analytics Routes
	mux.HandleFunc("POST /specs/{id}/play", playLimiter(config.AnalyticsHandler.TrackPlay))
	mux.Handle("POST /specs/{id}/favorite", config.AuthMiddleware.RequireAuth(http.HandlerFunc(config.AnalyticsHandler.ToggleFavorite)))
	mux.Handle("GET /specs/{id}/analytics", authz(middleware.PermAnalyticsView)(http.HandlerFunc(config.AnalyticsHandler.GetProducerAnalytics)))
	mux.Handle("GET /analytics/overview", authz(middleware.PermAnalyticsView)(http.HandlerFunc(config.AnalyticsHandler.GetOverview)))
	mux.Handle("GET /analytics/top-specs", authz(middleware.PermAnalyticsView)(http.HandlerFunc(config.AnalyticsHandler.GetTopSpecs)))

	if config.AdminHandler != nil {
		// Admin routes: each requires its own admin:* permission.
		mux.Handle("GET /admin/roles", authz(middleware.PermAdminRolesRead)(http.HandlerFunc(config.AdminHandler.ListRoles)))
		mux.Handle("GET /admin/users", authz(middleware.PermAdminUsersRead)(http.HandlerFunc(config.AdminHandler.ListUsers)))
		mux.Handle("GET /admin/users/{id}", authz(middleware.PermAdminUsersRead)(http.HandlerFunc(config.AdminHandler.GetUser)))
		mux.Handle("PATCH /admin/users/{id}/system-role", authz(middleware.PermAdminRolesAssign)(http.HandlerFunc(config.AdminHandler.UpdateUserSystemRole)))
		mux.Handle("PATCH /admin/users/{id}/status", authz(middleware.PermAdminUsersWrite)(http.HandlerFunc(config.AdminHandler.UpdateUserStatus)))
		mux.Handle("GET /admin/specs", authz(middleware.PermAdminSpecsRead)(http.HandlerFunc(config.AdminHandler.ListSpecs)))
		mux.Handle("PATCH /admin/specs/{id}", authz(middleware.PermAdminSpecsWrite)(http.HandlerFunc(config.AdminHandler.UpdateSpec)))
		mux.Handle("DELETE /admin/specs/{id}", authz(middleware.PermAdminSpecsWrite)(http.HandlerFunc(config.AdminHandler.DeleteSpec)))
		mux.Handle("GET /admin/orders", authz(middleware.PermAdminOrdersRead)(http.HandlerFunc(config.AdminHandler.ListOrders)))
		if config.PaymentHandler != nil {
			mux.Handle("POST /admin/orders/{id}/refunds", authz(middleware.PermAdminOrdersRefund)(http.HandlerFunc(config.PaymentHandler.CreateRefund)))
			mux.Handle("GET /admin/orders/{id}/refunds", authz(middleware.PermAdminOrdersRead)(http.HandlerFunc(config.PaymentHandler.GetRefunds)))
			mux.Handle("GET /admin/payment-webhooks", authz(middleware.PermAdminAuditRead)(http.HandlerFunc(config.PaymentHandler.ListWebhookEvents)))
			mux.Handle("POST /admin/payment-webhooks/{id}/requeue", authz(middleware.PermAdminAuditRead)(http.HandlerFunc(config.PaymentHandler.RequeueWebhookEvent)))
		}
		mux.Handle("GET /admin/licenses", authz(middleware.PermAdminLicensesRead)(http.HandlerFunc(config.AdminHandler.ListLicenses)))
		mux.Handle("GET /admin/analytics/overview", authz(middleware.PermAdminAnalyticsRead)(http.HandlerFunc(config.AdminHandler.AnalyticsOverview)))
		mux.Handle("GET /admin/audit-log", authz(middleware.PermAdminAuditRead)(http.HandlerFunc(config.AdminHandler.ListAuditLog)))
	}

	// Legal Routes
	if config.FXHandler != nil {
		mux.Handle("GET /admin/fx-rates", authz(middleware.PermAdminFXRead)(http.HandlerFunc(config.FXHandler.AdminListRates)))
		mux.Handle("POST /admin/fx-rates/refresh", authz(middleware.PermAdminFXWrite)(http.HandlerFunc(config.FXHandler.AdminRefreshRates)))
		mux.Handle("GET /fx/rates/latest", config.AuthMiddleware.RequireAuth(http.HandlerFunc(refreshLimiter(config.FXHandler.GetLatestRate))))
		mux.Handle("POST /fx/suggest-price", authz(middleware.PermSpecUpload)(http.HandlerFunc(refreshLimiter(config.FXHandler.SuggestPrice))))
	}

	if config.LegalHandler != nil {
		mux.HandleFunc("GET /api/v1/legal/documents/{key}", config.LegalHandler.GetDocument)
		mux.HandleFunc("GET /api/v1/legal/documents/{key}/versions", config.LegalHandler.GetDocumentVersions)
		mux.HandleFunc("GET /api/v1/legal/documents/{key}/versions/{version}", config.LegalHandler.GetDocumentVersion)
		if config.AuthMiddleware != nil {
			mux.Handle("GET /api/v1/legal/requirements", config.AuthMiddleware.RequireAuth(http.HandlerFunc(config.LegalHandler.GetRequirements)))
			mux.Handle("POST /api/v1/legal/acceptances", config.AuthMiddleware.RequireAuth(http.HandlerFunc(config.LegalHandler.RecordAcceptance)))
			mux.Handle("POST /api/v1/admin/legal/documents/{key}/drafts", authz(middleware.PermAdminSpecsWrite)(http.HandlerFunc(config.LegalHandler.CreateDraft)))
			mux.Handle("POST /api/v1/admin/legal/documents/{key}/publish", authz(middleware.PermAdminSpecsWrite)(http.HandlerFunc(config.LegalHandler.PublishVersion)))
			mux.Handle("GET /api/v1/admin/legal/audits", authz(middleware.PermAdminAuditRead)(http.HandlerFunc(config.LegalHandler.GetAudits)))
		}
	}

	return mux
}
