package main

import (
	"reflect"

	admin_http "github.com/saransh1220/blueprint-audio/internal/modules/admin/interfaces/http"
	analytics_domain "github.com/saransh1220/blueprint-audio/internal/modules/analytics/domain"
	auth_app "github.com/saransh1220/blueprint-audio/internal/modules/auth/application"
	auth_domain "github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	catalog_dto "github.com/saransh1220/blueprint-audio/internal/modules/catalog/interfaces/http"
	fx_http "github.com/saransh1220/blueprint-audio/internal/modules/fx/interfaces/http"
	notification_domain "github.com/saransh1220/blueprint-audio/internal/modules/notification/domain"
	payment_app "github.com/saransh1220/blueprint-audio/internal/modules/payment/application"
	payment_domain "github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
	user_app "github.com/saransh1220/blueprint-audio/internal/modules/user/application"
)

// Common response schemas
type SuccessMessageResponse struct {
	Message string `json:"message"`
}

type TokenResponse struct {
	Token string `json:"token"`
}

type UnreadCountResponse struct {
	UnreadCount int `json:"unread_count"`
}

type ToggleFavoriteResponse struct {
	IsFavorited bool `json:"is_favorited"`
}

type VerifyPaymentRequest struct {
	OrderID           string `json:"order_id"`
	RazorpayPaymentID string `json:"razorpay_payment_id"`
	RazorpaySignature string `json:"razorpay_signature"`
}

type VerifyPaymentResponse struct {
	Success bool                   `json:"success"`
	License payment_domain.License `json:"license"`
	Message string                 `json:"message"`
}

type CreateOrderRequest struct {
	SpecID          string `json:"spec_id"`
	LicenseOptionID string `json:"license_option_id"`
}

type DownloadFreeResponse struct {
	DownloadURL string `json:"download_url"`
	ExpiresIn   int    `json:"expires_in"`
}

type AdminUpdateSystemRoleRequest struct {
	SystemRole string `json:"system_role"`
}

type AdminUpdateStatusRequest struct {
	Status string `json:"status"`
}

type AdminUpdateSpecRequest struct {
	Title            *string  `json:"title,omitempty"`
	Category         *string  `json:"category,omitempty"`
	BPM              *int     `json:"bpm,omitempty"`
	Key              *string  `json:"key,omitempty"`
	BasePrice        *float64 `json:"base_price,omitempty"`
	ProcessingStatus *string  `json:"processing_status,omitempty"`
	IsDeleted        *bool    `json:"is_deleted,omitempty"`
}

type AdminCreateRefundRequest struct {
	Reason string `json:"reason"`
	Notes  string `json:"notes,omitempty"`
}

// GetEndpointsRegistry returns all route definitions mapped to real Go structs
func GetEndpointsRegistry() []EndpointDef {
	return []EndpointDef{
		// Operations
		{
			Method:      "GET",
			Path:        "/health",
			Tags:        []string{"Operations"},
			OperationID: "healthCheck",
			Summary:     "Check API health",
			Description: "Returns OK if the service is running",
			Auth:        "none",
			SuccessCode: 200,
		},
		{
			Method:      "GET",
			Path:        "/metrics",
			Tags:        []string{"Operations"},
			OperationID: "prometheusMetrics",
			Summary:     "Prometheus metrics",
			Description: "Exposes runtime, process, and HTTP metrics for Prometheus scraping",
			Auth:        "none",
			SuccessCode: 200,
		},

		// Auth
		{
			Method:       "POST",
			Path:         "/register",
			Tags:         []string{"Authentication"},
			OperationID:  "registerUser",
			Summary:      "Register a new user",
			Description:  "Creates a new artist or producer account and dispatches an email verification code",
			Auth:         "none",
			RequestBody:  auth_app.RegisterRequest{},
			SuccessCode:  201,
			ResponseBody: auth_domain.User{},
		},
		{
			Method:       "POST",
			Path:         "/login",
			Tags:         []string{"Authentication"},
			OperationID:  "loginUser",
			Summary:      "Log in with email and password",
			Description:  "Authenticates the user. In browser mode (X-Waveyard-Session: cookie), sets HttpOnly access and refresh cookies and returns 204 No Content. For legacy callers, sets an HTTP-only refresh cookie and returns a JSON access token.",
			Auth:         "none",
			RequestBody:  auth_app.LoginRequest{},
			SuccessCode:  200,
			ResponseBody: TokenResponse{},
		},
		{
			Method:       "POST",
			Path:         "/auth/google",
			Tags:         []string{"Authentication"},
			OperationID:  "googleLogin",
			Summary:      "Log in with Google ID token",
			Description:  "Authenticates or creates a user account via a verified Google ID token. In browser mode (X-Waveyard-Session: cookie), sets HttpOnly access and refresh cookies and returns 204 No Content. For legacy callers, sets an HTTP-only refresh cookie and returns a JSON access token.",
			Auth:         "none",
			RequestBody:  auth_app.GoogleLoginRequest{},
			SuccessCode:  200,
			ResponseBody: TokenResponse{},
		},
		{
			Method:       "POST",
			Path:         "/auth/refresh",
			Tags:         []string{"Authentication"},
			OperationID:  "refreshToken",
			Summary:      "Refresh access token",
			Description:  "Rotates the refresh session sent via HTTP-only cookie. In browser mode (X-Waveyard-Session: cookie), sets fresh HttpOnly access and refresh cookies and returns 204 No Content. For legacy callers, returns a JSON access token.",
			Auth:         "cookie",
			SuccessCode:  200,
			ResponseBody: TokenResponse{},
		},
		{
			Method:       "POST",
			Path:         "/auth/logout",
			Tags:         []string{"Authentication"},
			OperationID:  "logoutUser",
			Summary:      "Log out",
			Description:  "Revokes the current browser/refresh session and clears authentication cookies. Returns 204 No Content in browser mode (X-Waveyard-Session: cookie) or 200 OK for legacy callers.",
			Auth:         "cookie",
			SuccessCode:  200,
			ResponseBody: SuccessMessageResponse{},
		},
		{
			Method:       "POST",
			Path:         "/auth/verify-email",
			Tags:         []string{"Authentication"},
			OperationID:  "verifyEmail",
			Summary:      "Verify email address",
			Description:  "Validates a verification code received by email",
			Auth:         "none",
			RequestBody:  auth_app.VerifyEmailRequest{},
			SuccessCode:  200,
			ResponseBody: SuccessMessageResponse{},
		},
		{
			Method:       "POST",
			Path:         "/auth/resend-verification",
			Tags:         []string{"Authentication"},
			OperationID:  "resendVerificationEmail",
			Summary:      "Resend verification email",
			Description:  "Sends a new email verification code if the account is unverified",
			Auth:         "none",
			RequestBody:  auth_app.ResendVerificationRequest{},
			SuccessCode:  200,
			ResponseBody: SuccessMessageResponse{},
		},
		{
			Method:       "POST",
			Path:         "/auth/forgot-password",
			Tags:         []string{"Authentication"},
			OperationID:  "forgotPassword",
			Summary:      "Request password reset",
			Description:  "Generates and emails a password reset token",
			Auth:         "none",
			RequestBody:  auth_app.ForgotPasswordRequest{},
			SuccessCode:  200,
			ResponseBody: SuccessMessageResponse{},
		},
		{
			Method:       "POST",
			Path:         "/auth/reset-password",
			Tags:         []string{"Authentication"},
			OperationID:  "resetPassword",
			Summary:      "Reset password",
			Description:  "Resets the password using the code received via email",
			Auth:         "none",
			RequestBody:  auth_app.ResetPasswordRequest{},
			SuccessCode:  200,
			ResponseBody: SuccessMessageResponse{},
		},
		{
			Method:       "GET",
			Path:         "/me",
			Tags:         []string{"Authentication"},
			OperationID:  "getCurrentUser",
			Summary:      "Get current user profile",
			Description:  "Returns authenticated user account details",
			Auth:         "bearer",
			SuccessCode:  200,
			ResponseBody: auth_domain.User{},
		},

		// Catalog
		{
			Method:       "GET",
			Path:         "/catalog/home",
			Tags:         []string{"Catalog"},
			OperationID:  "getCatalogHome",
			Summary:      "Get catalog homepage data",
			Description:  "Returns curated and ranked catalog sections along with catalog-wide counts",
			Auth:         "flexible",
			SuccessCode:  200,
			ResponseBody: catalog_dto.HomepageResponse{},
		},
		{
			Method:      "GET",
			Path:        "/specs",
			Tags:        []string{"Catalog"},
			OperationID: "listSpecs",
			Summary:     "List catalog specs",
			Description: "Search and filter published specs",
			Auth:        "flexible",
			QueryParams: []ParamDef{
				{Name: "search", Type: "string", Description: "Search query across titles, tags, and producers"},
				{Name: "genre", Type: "string", Description: "Filter by genre slug"},
				{Name: "category", Type: "string", Description: "Filter by category: beat or soundkit"},
				{Name: "min_bpm", Type: "integer", Description: "Minimum tempo"},
				{Name: "max_bpm", Type: "integer", Description: "Maximum tempo"},
				{Name: "key", Type: "string", Description: "Musical key"},
				{Name: "sort", Type: "string", Description: "Sorting strategy"},
				{Name: "page", Type: "integer", Description: "Page number (1-based)"},
				{Name: "limit", Type: "integer", Description: "Items per page (max 100)"},
			},
			SuccessCode:  200,
			ResponseBody: []catalog_dto.SpecResponse{},
		},
		{
			Method:      "GET",
			Path:        "/specs/{id}",
			Tags:        []string{"Catalog"},
			OperationID: "getSpecByID",
			Summary:     "Get spec by ID or slug",
			Description: "Fetch single spec details with license options and audio preview URL",
			Auth:        "flexible",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Required: true, Description: "Spec UUID, slug, or short code"},
			},
			SuccessCode:  200,
			ResponseBody: catalog_dto.SpecResponse{},
		},
		{
			Method:      "POST",
			Path:        "/specs",
			Tags:        []string{"Catalog"},
			OperationID: "createSpecLegacy",
			Summary:     "Legacy spec creation (Gone)",
			Description: "Legacy multipart upload endpoint replaced by the /spec-uploads pipeline",
			Auth:        "bearer",
			SuccessCode: 410,
		},
		{
			Method:      "PATCH",
			Path:        "/specs/{id}",
			Tags:        []string{"Catalog"},
			OperationID: "updateSpec",
			Summary:     "Update spec details or cover image",
			Description: "Updates mutable metadata and optionally replaces the cover image",
			Auth:        "bearer",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "Spec UUID"},
			},
			ContentType:  "multipart/form-data",
			SuccessCode:  200,
			ResponseBody: catalog_dto.SpecResponse{},
		},
		{
			Method:      "DELETE",
			Path:        "/specs/{id}",
			Tags:        []string{"Catalog"},
			OperationID: "deleteSpec",
			Summary:     "Soft-delete spec",
			Description: "Soft-deletes a producer's own spec",
			Auth:        "bearer",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "Spec UUID"},
			},
			SuccessCode:  200,
			ResponseBody: SuccessMessageResponse{},
		},
		{
			Method:      "POST",
			Path:        "/specs/{id}/download-free",
			Tags:        []string{"Catalog"},
			OperationID: "downloadFreeSpec",
			Summary:     "Download free MP3 preview",
			Description: "Returns a presigned download URL for free-enabled specs",
			Auth:        "bearer",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "Spec UUID"},
			},
			SuccessCode:  200,
			ResponseBody: DownloadFreeResponse{},
		},

		// Uploads
		{
			Method:       "POST",
			Path:         "/spec-uploads",
			Tags:         []string{"Uploads"},
			OperationID:  "initiateSpecUpload",
			Summary:      "Initiate multi-step upload session",
			Description:  "Allocates a durable upload session and draft spec record",
			Auth:         "bearer",
			SuccessCode:  201,
			ResponseBody: catalog_dto.CreateSpecUploadResponse{},
		},
		{
			Method:      "PUT",
			Path:        "/spec-uploads/{id}/metadata",
			Tags:        []string{"Uploads"},
			OperationID: "saveSpecUploadMetadata",
			Summary:     "Save draft spec metadata",
			Description: "Stores title, genres, license pricing, BPM, and musical key",
			Auth:        "bearer",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "Upload session UUID"},
			},
			RequestBody:  catalog_dto.CreateSpecMetadataRequest{},
			SuccessCode:  200,
			ResponseBody: SuccessMessageResponse{},
		},
		{
			Method:      "POST",
			Path:        "/spec-uploads/{id}/files",
			Tags:        []string{"Uploads"},
			OperationID: "prepareSpecUploadFile",
			Summary:     "Request presigned S3 upload URL",
			Description: "Generates a presigned PUT URL for cover art, audio preview, WAV, or stems archive",
			Auth:        "bearer",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "Upload session UUID"},
			},
			RequestBody:  catalog_dto.CreateUploadFileRequest{},
			SuccessCode:  201,
			ResponseBody: catalog_dto.PresignedUploadResponse{},
		},
		{
			Method:      "POST",
			Path:        "/spec-uploads/{id}/files/{assetID}/complete",
			Tags:        []string{"Uploads"},
			OperationID: "confirmSpecUploadFile",
			Summary:     "Confirm uploaded file part",
			Description: "Notifies the backend that an S3 direct upload completed",
			Auth:        "bearer",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "Upload session UUID"},
				{Name: "assetID", Type: "string", Format: "uuid", Required: true, Description: "Asset UUID"},
			},
			SuccessCode:  200,
			ResponseBody: catalog_dto.ConfirmedUploadFileResponse{},
		},
		{
			Method:      "POST",
			Path:        "/spec-uploads/{id}/complete",
			Tags:        []string{"Uploads"},
			OperationID: "completeSpecUpload",
			Summary:     "Enqueue upload session for background processing",
			Description: "Enqueues media processing (waveform generation, audio conversion)",
			Auth:        "bearer",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "Upload session UUID"},
			},
			SuccessCode:  202,
			ResponseBody: catalog_dto.SpecUploadStatusResponse{},
		},
		{
			Method:      "GET",
			Path:        "/spec-uploads/{id}",
			Tags:        []string{"Uploads"},
			OperationID: "getSpecUploadStatus",
			Summary:     "Poll upload and processing status",
			Description: "Returns status of background media processing",
			Auth:        "bearer",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "Upload session UUID"},
			},
			SuccessCode:  200,
			ResponseBody: catalog_dto.SpecUploadStatusResponse{},
		},

		// Preferences
		{
			Method:       "GET",
			Path:         "/preferences/currency",
			Tags:         []string{"Preferences"},
			OperationID:  "getCurrencyPreference",
			Summary:      "Get buyer currency preference",
			Description:  "Returns the active display currency, saved user preference, and resolution source",
			Auth:         "flexible",
			SuccessCode:  200,
			ResponseBody: user_app.CurrencyPreferenceResponse{},
		},
		{
			Method:       "PUT",
			Path:         "/preferences/currency",
			Tags:         []string{"Preferences"},
			OperationID:  "setCurrencyPreference",
			Summary:      "Set buyer currency preference",
			Description:  "Sets preferred display currency (or resets to Automatic with null/empty)",
			Auth:         "flexible",
			RequestBody:  user_app.SetCurrencyPreferenceRequest{},
			SuccessCode:  200,
			ResponseBody: user_app.CurrencyPreferenceResponse{},
		},

		// Users
		{
			Method:       "PATCH",
			Path:         "/users/profile",
			Tags:         []string{"Users"},
			OperationID:  "updateUserProfile",
			Summary:      "Update current user profile",
			Description:  "Updates bio, display name, social links, and store currency",
			Auth:         "bearer",
			RequestBody:  user_app.UpdateProfileRequest{},
			SuccessCode:  200,
			ResponseBody: user_app.PublicUserResponse{},
		},
		{
			Method:      "POST",
			Path:        "/users/profile/avatar",
			Tags:        []string{"Users"},
			OperationID: "uploadUserAvatar",
			Summary:     "Upload user avatar image",
			Description: "Uploads and crops avatar image",
			Auth:        "bearer",
			ContentType: "multipart/form-data",
			SuccessCode: 200,
		},
		{
			Method:      "POST",
			Path:        "/users/profile/banner",
			Tags:        []string{"Users"},
			OperationID: "uploadUserBanner",
			Summary:     "Upload user profile banner",
			Description: "Uploads profile header banner image",
			Auth:        "bearer",
			ContentType: "multipart/form-data",
			SuccessCode: 200,
		},
		{
			Method:      "GET",
			Path:        "/users/{id}/public",
			Tags:        []string{"Users"},
			OperationID: "getPublicUserProfile",
			Summary:     "Get public user profile",
			Description: "Fetches public creator details by user ID",
			Auth:        "none",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "User UUID"},
			},
			SuccessCode:  200,
			ResponseBody: user_app.PublicUserResponse{},
		},
		{
			Method:      "GET",
			Path:        "/users/{id}/specs",
			Tags:        []string{"Users"},
			OperationID: "getUserSpecs",
			Summary:     "List producer's published specs",
			Description: "Fetches public specs released by a producer",
			Auth:        "flexible",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "Producer user UUID"},
			},
			QueryParams: []ParamDef{
				{Name: "page", Type: "integer", Description: "Page number (1-based)"},
				{Name: "limit", Type: "integer", Description: "Items per page"},
			},
			SuccessCode:  200,
			ResponseBody: []catalog_dto.SpecResponse{},
		},

		// Payments
		{
			Method:       "POST",
			Path:         "/orders",
			Tags:         []string{"Payments"},
			OperationID:  "createOrder",
			Summary:      "Create order for beat license",
			Description:  "Initiates checkout for a specific license option",
			Auth:         "bearer",
			RequestBody:  CreateOrderRequest{},
			SuccessCode:  200,
			ResponseBody: payment_domain.Order{},
			HeaderParams: []ParamDef{
				{Name: "Idempotency-Key", Type: "string", Description: "Opaque checkout intent key, up to 128 characters"},
			},
			Responses: map[string]Response{
				"202": {
					Description: "Checkout is in flight. Retry with the same key.",
					Headers:     map[string]Header{"Retry-After": {Schema: &Property{Type: "integer"}}},
				},
				"409": {Description: "Idempotency key conflict, changed price, or unavailable offer"},
			},
		},
		{
			Method:      "GET",
			Path:        "/orders",
			Tags:        []string{"Payments"},
			OperationID: "getUserOrders",
			Summary:     "List user's buyer orders",
			Description: "Lists historical orders placed by the authenticated buyer",
			Auth:        "bearer",
			QueryParams: []ParamDef{
				{Name: "page", Type: "integer", Description: "Page number (1-based)"},
				{Name: "limit", Type: "integer", Description: "Items per page"},
			},
			SuccessCode:  200,
			ResponseBody: []payment_domain.Order{},
		},
		{
			Method:      "GET",
			Path:        "/orders/{id}",
			Tags:        []string{"Payments"},
			OperationID: "getOrder",
			Summary:     "Get order details",
			Description: "Fetches order status, total amount, and checkout identifiers",
			Auth:        "bearer",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "Order UUID"},
			},
			SuccessCode:  200,
			ResponseBody: payment_domain.Order{},
		},
		{
			Method:      "GET",
			Path:        "/orders/{id}/status",
			Tags:        []string{"Payments"},
			OperationID: "getOrderStatus",
			Summary:     "Get order polling status",
			Description: "Pollable endpoint returning provider-neutral status, expiry, retry interval, and license when paid",
			Auth:        "bearer",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "Order UUID"},
			},
			SuccessCode:  200,
			ResponseBody: payment_app.OrderStatusResponse{},
		},
		{
			Method:       "POST",
			Path:         "/payments/verify",
			Tags:         []string{"Payments"},
			OperationID:  "verifyPayment",
			Summary:      "Verify payment signature",
			Description:  "Validates gateway signature and issues license record",
			Auth:         "bearer",
			RequestBody:  VerifyPaymentRequest{},
			SuccessCode:  200,
			ResponseBody: VerifyPaymentResponse{},
		},
		{
			Method:      "POST",
			Path:        "/webhooks/{provider}",
			Tags:        []string{"Payments"},
			OperationID: "providerWebhook",
			Summary:     "Process payment provider webhook",
			Description: "Provider-neutral webhook callback handler for Razorpay and Dodo payments",
			Auth:        "none",
			PathParams: []ParamDef{
				{Name: "provider", Type: "string", Required: true, Description: "Payment provider name (razorpay, dodo)"},
			},
			SuccessCode: 200,
		},
		{
			Method:      "POST",
			Path:        "/webhooks/dodo",
			Tags:        []string{"Payments"},
			OperationID: "dodoWebhook",
			Summary:     "Process Dodo payment webhook",
			Description: "Webhook callback handler for Dodo payments",
			Auth:        "none",
			SuccessCode: 200,
		},
		{
			Method:      "GET",
			Path:        "/licenses",
			Tags:        []string{"Payments"},
			OperationID: "getUserLicenses",
			Summary:     "List purchased licenses",
			Description: "Returns all active beat licenses owned by the caller",
			Auth:        "bearer",
			QueryParams: []ParamDef{
				{Name: "page", Type: "integer", Description: "Page number (1-based)"},
				{Name: "limit", Type: "integer", Description: "Items per page"},
				{Name: "type", Type: "string", Description: "Filter by license type"},
				{Name: "search", Type: "string", Description: "Search by title"},
			},
			SuccessCode:  200,
			ResponseBody: []payment_domain.License{},
		},
		{
			Method:      "GET",
			Path:        "/licenses/{id}/downloads",
			Tags:        []string{"Payments"},
			OperationID: "getLicenseDownloads",
			Summary:     "Get presigned asset download links",
			Description: "Returns temporary download URLs for stems, WAV, and MP3 files",
			Auth:        "bearer",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "License UUID"},
			},
			SuccessCode:  200,
			ResponseBody: payment_app.LicenseDownloadsResponse{},
		},
		{
			Method:      "GET",
			Path:        "/orders/producer",
			Tags:        []string{"Payments"},
			OperationID: "getProducerOrders",
			Summary:     "List sales orders for producer",
			Description: "Returns paginated orders of specs produced by the caller",
			Auth:        "bearer",
			QueryParams: []ParamDef{
				{Name: "page", Type: "integer", Description: "Page number (1-based)"},
				{Name: "limit", Type: "integer", Description: "Items per page"},
			},
			SuccessCode:  200,
			ResponseBody: payment_app.ProducerOrderResponse{},
		},

		// Notifications
		{
			Method:      "GET",
			Path:        "/notifications",
			Tags:        []string{"Notifications"},
			OperationID: "listNotifications",
			Summary:     "List caller notifications",
			Description: "Fetches user notifications sorted by recent activity",
			Auth:        "bearer",
			QueryParams: []ParamDef{
				{Name: "page", Type: "integer", Description: "Page number (1-based)"},
				{Name: "limit", Type: "integer", Description: "Items per page"},
			},
			SuccessCode:  200,
			ResponseBody: []notification_domain.Notification{},
		},
		{
			Method:      "PATCH",
			Path:        "/notifications/{id}/read",
			Tags:        []string{"Notifications"},
			OperationID: "markNotificationRead",
			Summary:     "Mark notification as read",
			Description: "Flags single notification as read",
			Auth:        "bearer",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "Notification UUID"},
			},
			SuccessCode:  200,
			ResponseBody: SuccessMessageResponse{},
		},
		{
			Method:       "PATCH",
			Path:         "/notifications/read-all",
			Tags:         []string{"Notifications"},
			OperationID:  "markAllNotificationsRead",
			Summary:      "Mark all notifications as read",
			Description:  "Clears unread flag on all notifications for the caller",
			Auth:         "bearer",
			SuccessCode:  200,
			ResponseBody: SuccessMessageResponse{},
		},
		{
			Method:       "GET",
			Path:         "/notifications/unread-count",
			Tags:         []string{"Notifications"},
			OperationID:  "getUnreadNotificationCount",
			Summary:      "Get unread notification count",
			Description:  "Quick count of unread items for badge counters",
			Auth:         "bearer",
			SuccessCode:  200,
			ResponseBody: UnreadCountResponse{},
		},
		{
			Method:      "GET",
			Path:        "/ws",
			Tags:        []string{"Notifications"},
			OperationID: "subscribeRealtimeNotifications",
			Summary:     "Realtime WebSocket notification feed",
			Description: "Upgrades HTTP connection to WebSocket for live event streaming",
			Auth:        "bearer",
			SuccessCode: 101,
		},

		// Analytics
		{
			Method:      "POST",
			Path:        "/specs/{id}/play",
			Tags:        []string{"Analytics"},
			OperationID: "trackSpecPlay",
			Summary:     "Record spec audio playback",
			Description: "Increments public and producer play metrics",
			Auth:        "none",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "Spec UUID"},
			},
			SuccessCode: 200,
		},
		{
			Method:      "POST",
			Path:        "/specs/{id}/favorite",
			Tags:        []string{"Analytics"},
			OperationID: "toggleSpecFavorite",
			Summary:     "Toggle favorite bookmark",
			Description: "Adds or removes spec from authenticated user's favorites",
			Auth:        "bearer",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "Spec UUID"},
			},
			SuccessCode:  200,
			ResponseBody: ToggleFavoriteResponse{},
		},
		{
			Method:      "GET",
			Path:        "/specs/{id}/analytics",
			Tags:        []string{"Analytics"},
			OperationID: "getProducerSpecAnalytics",
			Summary:     "Get detailed analytics for single spec",
			Description: "Returns producer-exclusive playback, favorite, and purchase metrics",
			Auth:        "bearer",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "Spec UUID"},
			},
			SuccessCode:  200,
			ResponseBody: analytics_domain.ProducerAnalytics{},
		},
		{
			Method:      "GET",
			Path:        "/analytics/overview",
			Tags:        []string{"Analytics"},
			OperationID: "getProducerAnalyticsOverview",
			Summary:     "Get producer aggregate metrics overview",
			Description: "Aggregates revenue, plays, conversion rates, and time-series charts",
			Auth:        "bearer",
			QueryParams: []ParamDef{
				{Name: "days", Type: "integer", Description: "Lookback window in days (e.g. 7, 30, 90)"},
				{Name: "sortBy", Type: "string", Description: "Sorting field for ranked breakdowns"},
			},
			SuccessCode:  200,
			ResponseBody: analytics_domain.AnalyticsOverviewResponse{},
		},
		{
			Method:      "GET",
			Path:        "/analytics/top-specs",
			Tags:        []string{"Analytics"},
			OperationID: "getProducerTopSpecs",
			Summary:     "Get top performing specs for producer",
			Description: "Ranked list of best-performing tracks",
			Auth:        "bearer",
			QueryParams: []ParamDef{
				{Name: "limit", Type: "integer", Description: "Max items to return"},
				{Name: "sortBy", Type: "string", Description: "Sort by plays, revenue, or favorites"},
			},
			SuccessCode:  200,
			ResponseBody: []analytics_domain.TopSpecStat{},
		},

		// Strict server endpoint
		{
			Method:      "GET",
			Path:        "/me/favorites",
			Tags:        []string{"Users"},
			OperationID: "listMyFavorites",
			Summary:     "List caller favorite specs",
			Description: "Returns a paginated list of specs favorited by the authenticated user",
			Auth:        "bearer",
			QueryParams: []ParamDef{
				{Name: "limit", Type: "integer", Description: "Number of favorites to retrieve"},
				{Name: "cursor", Type: "string", Description: "Opaque pagination cursor"},
			},
			SuccessCode: 200,
		},

		// Admin routes
		{
			Method:      "GET",
			Path:        "/admin/users",
			Tags:        []string{"Admin"},
			OperationID: "adminListUsers",
			Summary:     "Admin list users",
			Description: "Super-admin search and list accounts with role and status filters",
			Auth:        "bearer",
			QueryParams: []ParamDef{
				{Name: "page", Type: "integer", Description: "Page number, starting at 1"},
				{Name: "limit", Type: "integer", Description: "Page size chosen by the client (default 50, capped at 100)"},
				{Name: "q", Type: "string", Description: "Search text"},
			},
			SuccessCode: 200,
		},
		{
			Method:      "GET",
			Path:        "/admin/users/{id}",
			Tags:        []string{"Admin"},
			OperationID: "adminGetUser",
			Summary:     "Admin get user details",
			Description: "Super-admin inspect single account with system details",
			Auth:        "bearer",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "User UUID"},
			},
			SuccessCode: 200,
		},
		{
			Method:      "PATCH",
			Path:        "/admin/users/{id}/system-role",
			Tags:        []string{"Admin"},
			OperationID: "adminUpdateUserSystemRole",
			Summary:     "Admin update user system role",
			Description: "Assigns a system-kind role from the roles table (for example super_admin or user)",
			Auth:        "bearer",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "User UUID"},
			},
			RequestBody: AdminUpdateSystemRoleRequest{},
			SuccessCode: 200,
		},
		{
			Method:      "PATCH",
			Path:        "/admin/users/{id}/status",
			Tags:        []string{"Admin"},
			OperationID: "adminUpdateUserStatus",
			Summary:     "Admin update user account status",
			Description: "Suspends or activates user account",
			Auth:        "bearer",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "User UUID"},
			},
			RequestBody: AdminUpdateStatusRequest{},
			SuccessCode: 200,
		},
		{
			Method:      "GET",
			Path:        "/admin/specs",
			Tags:        []string{"Admin"},
			OperationID: "adminListSpecs",
			Summary:     "Admin list specs",
			Description: "Super-admin search specs including deleted items",
			Auth:        "bearer",
			QueryParams: []ParamDef{
				{Name: "page", Type: "integer", Description: "Page number, starting at 1"},
				{Name: "limit", Type: "integer", Description: "Page size chosen by the client (default 50, capped at 100)"},
				{Name: "q", Type: "string", Description: "Search text"},
			},
			SuccessCode: 200,
		},
		{
			Method:      "PATCH",
			Path:        "/admin/specs/{id}",
			Tags:        []string{"Admin"},
			OperationID: "adminUpdateSpec",
			Summary:     "Admin update spec",
			Description: "Super-admin override spec properties",
			Auth:        "bearer",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "Spec UUID"},
			},
			RequestBody: AdminUpdateSpecRequest{},
			SuccessCode: 200,
		},
		{
			Method:      "DELETE",
			Path:        "/admin/specs/{id}",
			Tags:        []string{"Admin"},
			OperationID: "adminDeleteSpec",
			Summary:     "Admin delete spec",
			Description: "Super-admin hard or soft delete of spec",
			Auth:        "bearer",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "Spec UUID"},
			},
			SuccessCode: 200,
		},
		{
			Method:      "GET",
			Path:        "/admin/orders",
			Tags:        []string{"Admin"},
			OperationID: "adminListOrders",
			Summary:     "Admin list orders",
			Description: "Super-admin search orders across all buyers and producers",
			Auth:        "bearer",
			QueryParams: []ParamDef{
				{Name: "page", Type: "integer", Description: "Page number, starting at 1"},
				{Name: "limit", Type: "integer", Description: "Page size chosen by the client (default 50, capped at 100)"},
				{Name: "q", Type: "string", Description: "Search text"},
			},
			SuccessCode: 200,
		},
		{
			Method:      "POST",
			Path:        "/admin/orders/{id}/reconcile-checkout",
			Tags:        []string{"Admin"},
			OperationID: "adminReconcileCheckout",
			QueryParams: []ParamDef{{Name: "provider_reference", Type: "string", Description: "Verified Razorpay order ID or Dodo payment ID from the provider dashboard"}},
			Summary:     "Reconcile an existing checkout using provider reads",
			Auth:        "bearer",
			PathParams:  []ParamDef{{Name: "id", Type: "string", Format: "uuid", Required: true}},
			SuccessCode: 204,
			Responses: map[string]Response{
				"202": {Description: "Checkout outcome is still unresolved"},
				"404": {Description: "Order not found"},
			},
		},
		{
			Method:      "POST",
			Path:        "/admin/orders/{id}/refunds",
			Tags:        []string{"Admin"},
			OperationID: "adminCreateOrderRefund",
			Summary:     "Admin issue full refund for order",
			Description: "Initiates a provider refund and revokes licenses upon success",
			Auth:        "bearer",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "Order UUID"},
			},
			RequestBody:  AdminCreateRefundRequest{},
			SuccessCode:  201,
			ResponseBody: payment_app.OrderRefundResponse{},
			HeaderParams: []ParamDef{
				{Name: "Idempotency-Key", Type: "string", Description: "Opaque refund intent key, up to 128 characters"},
			},
			Responses: map[string]Response{
				"409": {Description: "Refund operation already exists"},
			},
		},
		{
			Method:      "GET",
			Path:        "/admin/orders/{id}/refunds",
			Tags:        []string{"Admin"},
			OperationID: "adminGetOrderRefunds",
			Summary:     "Admin list order refunds",
			Description: "Lists all refund records for a given order",
			Auth:        "bearer",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "Order UUID"},
			},
			SuccessCode:  200,
			ResponseBody: []payment_domain.OrderRefund{},
		},
		{
			Method:      "GET",
			Path:        "/admin/payment-webhooks",
			Tags:        []string{"Admin"},
			OperationID: "adminListPaymentWebhooks",
			Summary:     "Admin list payment webhook inbox events",
			Description: "Lists webhook inbox events for audit and operations",
			Auth:        "bearer",
			QueryParams: []ParamDef{
				{Name: "page", Type: "integer", Description: "Page number, starting at 1"},
				{Name: "limit", Type: "integer", Description: "Page size chosen by the client"},
				{Name: "provider", Type: "string", Description: "Provider filter (razorpay, dodo)"},
				{Name: "status", Type: "string", Description: "Processing status (pending, processing, completed, failed, dead_letter)"},
			},
			SuccessCode:  200,
			ResponseBody: []payment_domain.PaymentWebhookEvent{},
		},
		{
			Method:      "POST",
			Path:        "/admin/payment-webhooks/{id}/requeue",
			Tags:        []string{"Admin"},
			OperationID: "adminRequeuePaymentWebhook",
			Summary:     "Admin requeue payment webhook event",
			Description: "Requeues a failed or dead-lettered webhook event for processing",
			Auth:        "bearer",
			PathParams: []ParamDef{
				{Name: "id", Type: "string", Format: "uuid", Required: true, Description: "Webhook Event UUID"},
			},
			SuccessCode: 200,
		},
		{
			Method:      "GET",
			Path:        "/admin/licenses",
			Tags:        []string{"Admin"},
			OperationID: "adminListLicenses",
			Summary:     "Admin list licenses",
			Description: "Super-admin search issued licenses",
			Auth:        "bearer",
			QueryParams: []ParamDef{
				{Name: "page", Type: "integer", Description: "Page number, starting at 1"},
				{Name: "limit", Type: "integer", Description: "Page size chosen by the client (default 50, capped at 100)"},
				{Name: "q", Type: "string", Description: "Search text"},
			},
			SuccessCode: 200,
		},
		{
			Method:      "GET",
			Path:        "/admin/analytics/overview",
			Tags:        []string{"Admin"},
			OperationID: "adminAnalyticsOverview",
			Summary:     "Admin platform analytics overview",
			Description: "Super-admin platform-wide metrics",
			Auth:        "bearer",
			SuccessCode: 200,
		},
		{
			Method:      "GET",
			Path:        "/admin/audit-log",
			Tags:        []string{"Admin"},
			OperationID: "adminListAuditLog",
			Summary:     "Admin audit log",
			Description: "Super-admin audit log of changes made by administrators",
			Auth:        "bearer",
			QueryParams: []ParamDef{
				{Name: "page", Type: "integer", Description: "Page number, starting at 1"},
				{Name: "limit", Type: "integer", Description: "Page size chosen by the client (default 50, capped at 100)"},
				{Name: "q", Type: "string", Description: "Search text"},
			},
			SuccessCode: 200,
		},
		{
			Method:      "GET",
			Path:        "/admin/roles",
			Tags:        []string{"Admin"},
			OperationID: "adminListRoles",
			Summary:     "Admin list roles",
			Description: "Lists every role (business and system) with the permissions it grants",
			Auth:        "bearer",
			SuccessCode: 200,
		},
		{
			Method:      "GET",
			Path:        "/admin/fx-rates",
			Tags:        []string{"Admin", "FX"},
			OperationID: "adminListFXRates",
			Summary:     "List FX rate history",
			Description: "Super-admin paginated history of exchange rates with latest rate per pair",
			Auth:        "bearer",
			QueryParams: []ParamDef{
				{Name: "page", Type: "integer", Description: "Page number, starting at 1"},
				{Name: "limit", Type: "integer", Description: "Page size chosen by the client (default 50, capped at 100)"},
				{Name: "base", Type: "string", Description: "Filter by base currency code (e.g. USD)"},
				{Name: "quote", Type: "string", Description: "Filter by quote currency code (e.g. INR)"},
				{Name: "status", Type: "string", Description: "Filter by status: accepted or quarantined"},
			},
			SuccessCode: 200,
		},
		{
			Method:      "POST",
			Path:        "/admin/fx-rates/refresh",
			Tags:        []string{"Admin", "FX"},
			OperationID: "adminRefreshFXRates",
			Summary:     "Trigger manual FX rate refresh",
			Description: "Triggers a rate refresh from the upstream provider with cooldown and audit logging",
			Auth:        "bearer",
			SuccessCode: 200,
		},
		{
			Method:      "GET",
			Path:        "/fx/rates/latest",
			Tags:        []string{"FX"},
			OperationID: "getLatestFXRate",
			Summary:     "Get latest FX rate",
			Description: "Returns the latest accepted exchange rate for a given currency pair",
			Auth:        "bearer",
			QueryParams: []ParamDef{
				{Name: "base", Type: "string", Required: true, Description: "Base currency code (e.g. USD)"},
				{Name: "quote", Type: "string", Required: true, Description: "Quote currency code (e.g. INR)"},
			},
			SuccessCode: 200,
		},
		{
			Method:       "POST",
			Path:         "/fx/suggest-price",
			Tags:         []string{"FX"},
			OperationID:  "suggestSecondaryPrice",
			Summary:      "Suggest secondary currency price",
			Description:  "Calculates an auto-filled secondary currency price using exchange rates and target rounding rules",
			Auth:         "bearer",
			RequestBody:  fx_http.SuggestPriceRequest{},
			SuccessCode:  200,
			ResponseBody: fx_http.SuggestPriceResponse{},
		},

		// System & Currency Config
		{
			Method:      "GET",
			Path:        "/config/currencies",
			Tags:        []string{"Configuration"},
			OperationID: "getCurrencyConfiguration",
			Summary:     "Get currency configurations",
			Description: "Returns active supported currencies and minor-unit metadata",
			Auth:        "none",
			SuccessCode: 200,
		},
		{
			Method:      "GET",
			Path:        "/producers/{handle}",
			Tags:        []string{"Catalog"},
			OperationID: "getProducerByHandle",
			Summary:     "Get producer profile by handle",
			Description: "Retrieves public producer profile and catalog metrics",
			Auth:        "none",
			PathParams: []ParamDef{
				{Name: "handle", Type: "string", Required: true, Description: "Producer username/handle"},
			},
			SuccessCode: 200,
		},
		{
			Method:      "POST",
			Path:        "/ws/ticket",
			Tags:        []string{"Notifications"},
			OperationID: "createWebSocketTicket",
			Summary:     "Create single-use WebSocket ticket",
			Description: "Exchanges bearer auth for short-lived ticket",
			Auth:        "bearer",
			SuccessCode: 200,
		},

		// Legal Documents
		{
			Method:      "GET",
			Path:        "/api/v1/legal/documents/{key}",
			Tags:        []string{"Legal"},
			OperationID: "getLegalDocument",
			Summary:     "Get current legal document",
			Description: "Returns effective version and metadata for the specified legal document key",
			Auth:        "none",
			PathParams: []ParamDef{
				{Name: "key", Type: "string", Required: true, Description: "Document key (terms, privacy, licensing)"},
			},
			SuccessCode: 200,
		},
		{
			Method:      "GET",
			Path:        "/api/v1/legal/documents/{key}/versions",
			Tags:        []string{"Legal"},
			OperationID: "listLegalDocumentVersions",
			Summary:     "List published legal document versions",
			Description: "Returns revision history and change summaries for the legal document",
			Auth:        "none",
			PathParams: []ParamDef{
				{Name: "key", Type: "string", Required: true, Description: "Document key"},
			},
			SuccessCode: 200,
		},
		{
			Method:      "GET",
			Path:        "/api/v1/legal/documents/{key}/versions/{version}",
			Tags:        []string{"Legal"},
			OperationID: "getLegalDocumentVersion",
			Summary:     "Get specific historical legal document version",
			Description: "Returns immutable historical text and sections for archival reference",
			Auth:        "none",
			PathParams: []ParamDef{
				{Name: "key", Type: "string", Required: true, Description: "Document key"},
				{Name: "version", Type: "string", Required: true, Description: "Display version (e.g. 1.0.0)"},
			},
			SuccessCode: 200,
		},
		{
			Method:      "GET",
			Path:        "/api/v1/legal/requirements",
			Tags:        []string{"Legal"},
			OperationID: "getLegalRequirements",
			Summary:     "Get user legal requirements",
			Description: "Returns required documents and current user acceptance state",
			Auth:        "bearer",
			SuccessCode: 200,
		},
		{
			Method:      "POST",
			Path:        "/api/v1/legal/acceptances",
			Tags:        []string{"Legal"},
			OperationID: "recordLegalAcceptance",
			Summary:     "Record legal document acceptance",
			Description: "Records affirmative user acceptance against exact document hash",
			Auth:        "bearer",
			SuccessCode: 200,
		},
		{
			Method:      "POST",
			Path:        "/api/v1/admin/legal/documents/{key}/drafts",
			Tags:        []string{"Admin"},
			OperationID: "createLegalDraft",
			Summary:     "Admin create legal draft version",
			Description: "Uploads a new draft revision for review and preview",
			Auth:        "bearer",
			PathParams: []ParamDef{
				{Name: "key", Type: "string", Required: true, Description: "Document key"},
			},
			SuccessCode: 201,
		},
		{
			Method:      "POST",
			Path:        "/api/v1/admin/legal/documents/{key}/publish",
			Tags:        []string{"Admin"},
			OperationID: "publishLegalVersion",
			Summary:     "Admin publish legal document version",
			Description: "Atomically activates a legal version and supersedes previous versions",
			Auth:        "bearer",
			PathParams: []ParamDef{
				{Name: "key", Type: "string", Required: true, Description: "Document key"},
			},
			SuccessCode: 200,
		},
		{
			Method:      "GET",
			Path:        "/api/v1/admin/legal/audits",
			Tags:        []string{"Admin"},
			OperationID: "listLegalPublicationAudits",
			Summary:     "Admin list legal publication audits",
			Description: "Returns publication audit history for legal policies",
			Auth:        "bearer",
			SuccessCode: 200,
		},
	}
}

// RegisterKnownTypes registers explicit types into the reflector
func RegisterKnownTypes(r *Reflector) {
	// Register Admin types if needed
	_ = reflect.TypeOf(admin_http.AdminHandler{})
}
