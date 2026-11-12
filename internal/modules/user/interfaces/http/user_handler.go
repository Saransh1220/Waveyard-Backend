package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	authDomain "github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	"image"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/disintegration/imaging"
	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/gateway/middleware"
	"github.com/saransh1220/blueprint-audio/internal/modules/user/application"
	"github.com/saransh1220/blueprint-audio/internal/shared/money"
	"github.com/saransh1220/blueprint-audio/internal/shared/utils"
	_ "golang.org/x/image/webp"
)

// UserService defines the interface for user operations
type UserService interface {
	UpdateProfile(ctx context.Context, userID uuid.UUID, req application.UpdateProfileRequest) error
	GetPublicProfile(ctx context.Context, userID uuid.UUID) (*application.PublicUserResponse, error)
	GetPreferredCurrency(ctx context.Context, userID uuid.UUID) (*string, error)
	SetPreferredCurrency(ctx context.Context, userID uuid.UUID, currency *string) error
}

// FileService defines the interface for file operations
type FileService interface {
	Upload(ctx context.Context, file multipart.File, header *multipart.FileHeader, folder string) (string, string, error)
	UploadWithKey(ctx context.Context, file io.Reader, key string, contentType string) (string, error)
	GetPresignedURL(ctx context.Context, key string, expiration time.Duration) (string, error)
	GetKeyFromUrl(fileUrl string) (string, error)
	Delete(ctx context.Context, key string) error
}

type UserHandler struct {
	service      UserService
	fileService  FileService
	secureCookie bool
}

func NewUserHandler(service UserService, fileService FileService) *UserHandler {
	return &UserHandler{
		service:     service,
		fileService: fileService,
	}
}

func (h *UserHandler) WithSecureCookie(secure bool) *UserHandler {
	h.secureCookie = secure
	return h
}

// UpdateProfile handles PATCH /users/profile - updates authenticated user's profile
func (h *UserHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(middleware.ContextKeyUserId).(uuid.UUID)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req application.UpdateProfileRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if err := h.service.UpdateProfile(r.Context(), userID, req); err != nil {
		if errors.Is(err, authDomain.ErrHandleTaken) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Return updated profile
	profile, err := h.service.GetPublicProfile(r.Context(), userID)
	if err != nil {
		http.Error(w, "failed to load profile", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profile)
}

// GetPublicProfile handles GET /users/:id/public - gets a user's public profile
func (h *UserHandler) GetPublicProfile(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.PathValue("id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}

	profile, err := h.service.GetPublicProfile(r.Context(), userID)
	if err != nil {
		http.Error(w, "failed to fetch profile", http.StatusInternalServerError)
		return
	}

	// Sanitize profile to generate presigned URLs for avatar
	h.sanitizeUserProfile(profile)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profile)
}

// UploadAvatar handles POST /users/profile/avatar - uploads a new avatar image
func (h *UserHandler) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	h.uploadProfileImage(w, r, "avatar", "avatars", false)
}

// UploadBanner handles POST /users/profile/banner.
func (h *UserHandler) UploadBanner(w http.ResponseWriter, r *http.Request) {
	h.uploadProfileImage(w, r, "banner", "banners", true)
}

func (h *UserHandler) uploadProfileImage(w http.ResponseWriter, r *http.Request, formField, folder string, landscape bool) {
	userID, ok := r.Context().Value(middleware.ContextKeyUserId).(uuid.UUID)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Limit request size to 10MB
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)

	// Parse multipart form (max 10MB for avatar)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "file too large", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile(formField)
	if err != nil {
		http.Error(w, formField+" file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()
	data, err := readAndValidateImage(file, landscape)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Get current user profile to check for existing avatar
	currentUser, err := h.service.GetPublicProfile(r.Context(), userID)
	if err != nil {
		http.Error(w, "failed to get user", http.StatusInternalServerError)
		return
	}

	// Delete old avatar if it exists
	var oldURL *string
	if landscape {
		oldURL = currentUser.BannerURL
	} else {
		oldURL = currentUser.AvatarURL
	}
	imageID, err := uuid.NewV7()
	if err != nil {
		http.Error(w, "failed to generate image id", http.StatusInternalServerError)
		return
	}
	imageURL, err := h.fileService.UploadWithKey(r.Context(), bytes.NewReader(data), fmt.Sprintf("%s/%s.jpg", folder, imageID), "image/jpeg")
	if err != nil {
		http.Error(w, "failed to upload image", http.StatusInternalServerError)
		return
	}

	// Update user profile with new avatar URL
	req := application.UpdateProfileRequest{}
	if landscape {
		req.BannerURL = &imageURL
	} else {
		req.AvatarURL = &imageURL
	}
	if err := h.service.UpdateProfile(r.Context(), userID, req); err != nil {
		// Rollback: delete the newly uploaded file
		if newKey, keyErr := h.fileService.GetKeyFromUrl(imageURL); keyErr == nil {
			_ = h.fileService.Delete(r.Context(), newKey)
		}
		http.Error(w, "failed to update profile image", http.StatusInternalServerError)
		return
	}
	// Delete the previous image only after the new URL is safely persisted.
	if oldURL != nil && *oldURL != "" {
		if oldKey, keyErr := h.fileService.GetKeyFromUrl(*oldURL); keyErr == nil {
			_ = h.fileService.Delete(r.Context(), oldKey)
		}
	}

	// Return updated profile
	profile, err := h.service.GetPublicProfile(r.Context(), userID)
	if err != nil {
		http.Error(w, "failed to load profile", http.StatusInternalServerError)
		return
	}

	// Sanitize to generate presigned URLs
	h.sanitizeUserProfile(profile)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profile)
}

func readAndValidateImage(file multipart.File, landscape bool) ([]byte, error) {
	data := make([]byte, 0)
	buffer := new(bytes.Buffer)
	if _, err := buffer.ReadFrom(file); err != nil {
		return nil, fmt.Errorf("failed to read image")
	}
	data = buffer.Bytes()
	if len(data) == 0 || len(data) > 5<<20 {
		return nil, fmt.Errorf("image must be at most 5MB")
	}
	src, format, err := image.Decode(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png" && format != "webp") {
		return nil, fmt.Errorf("image must be a valid JPEG, PNG, or WebP")
	}
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width < 1 || height < 1 || (landscape && width <= height) || (!landscape && width != height) {
		if landscape {
			return nil, fmt.Errorf("banner image must be landscape")
		}
		return nil, fmt.Errorf("avatar image must be square")
	}
	if landscape {
		src = imaging.Fill(src, 1500, 500, imaging.Center, imaging.Lanczos)
	} else {
		src = imaging.Fit(src, 512, 512, imaging.Lanczos)
	}
	output := new(bytes.Buffer)
	if err := imaging.Encode(output, src, imaging.JPEG, imaging.JPEGQuality(85)); err != nil {
		return nil, fmt.Errorf("failed to normalize image")
	}
	return output.Bytes(), nil
}

// sanitizeUserProfile generates presigned URLs for avatar images
func (h *UserHandler) sanitizeUserProfile(profile *application.PublicUserResponse) {
	for _, field := range []*string{profile.AvatarURL, profile.BannerURL} {
		if field == nil || *field == "" {
			continue
		}
		key, err := h.fileService.GetKeyFromUrl(*field)
		if err != nil {
			continue
		}
		if presignedURL, err := h.fileService.GetPresignedURL(context.Background(), key, time.Hour); err == nil && presignedURL != "" {
			*field = presignedURL
		}
	}
}

func (h *UserHandler) GetPublicProfileByHandle(w http.ResponseWriter, r *http.Request) {
	finder, ok := h.service.(interface {
		GetPublicProfileByHandle(context.Context, string) (*application.PublicUserResponse, error)
	})
	if !ok {
		http.Error(w, "profile not found", http.StatusNotFound)
		return
	}
	profile, err := finder.GetPublicProfileByHandle(r.Context(), r.PathValue("handle"))
	if errors.Is(err, authDomain.ErrUserNotFound) {
		http.Error(w, "profile not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "failed to fetch profile", http.StatusInternalServerError)
		return
	}
	h.sanitizeUserProfile(profile)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profile)
}

// GetCurrencyPreference handles GET /preferences/currency.
// Reads authenticated profile preference, cookie preference, or country code.
// If authenticated with a preference, refreshes the wy_currency cookie.
func (h *UserHandler) GetCurrencyPreference(w http.ResponseWriter, r *http.Request) {
	var profileCurrency *string
	if userID, ok := r.Context().Value(middleware.ContextKeyUserId).(uuid.UUID); ok {
		if pref, err := h.service.GetPreferredCurrency(r.Context(), userID); err == nil {
			profileCurrency = pref
		}
	}

	var cookieCurrency string
	if c, err := r.Cookie("wy_currency"); err == nil {
		cookieCurrency = c.Value
	}

	country := strings.TrimSpace(r.Header.Get("CF-IPCountry"))
	if country == "" {
		country = strings.TrimSpace(r.Header.Get("X-Country-Code"))
	}

	profileVal := ""
	if profileCurrency != nil {
		profileVal = *profileCurrency
	}

	res := money.ResolveDisplayCurrency(money.ResolveInput{
		ProfileCurrency: profileVal,
		CookieCurrency:  cookieCurrency,
		CountryCode:     country,
	})

	if profileCurrency != nil && *profileCurrency != "" {
		utils.SetCookie(w, "wy_currency", *profileCurrency, "/", time.Now().Add(365*24*time.Hour), 365*24*3600, h.secureCookie, true)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(application.CurrencyPreferenceResponse{
		Preference: profileCurrency,
		Effective:  res.Currency,
		Source:     string(res.Source),
	})
}

// SetCurrencyPreference handles PUT /preferences/currency.
// Body: { "currency": "INR" | "USD" | null }
// If currency is null/empty, clears profile and cookie (Automatic mode).
// If authenticated, persists to database profile.
// Updates wy_currency cookie and returns updated preference state.
func (h *UserHandler) SetCurrencyPreference(w http.ResponseWriter, r *http.Request) {
	var req application.SetCurrencyPreferenceRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, `{"error": "invalid request body"}`, http.StatusBadRequest)
		return
	}

	var preferredCurrency *string
	if req.Currency != nil && strings.TrimSpace(*req.Currency) != "" {
		def, ok := money.LookupCurrency(*req.Currency)
		if !ok {
			http.Error(w, `{"error": "unsupported currency"}`, http.StatusBadRequest)
			return
		}
		code := def.Code
		preferredCurrency = &code
	}

	userID, isAuthenticated := r.Context().Value(middleware.ContextKeyUserId).(uuid.UUID)
	if isAuthenticated {
		if err := h.service.SetPreferredCurrency(r.Context(), userID, preferredCurrency); err != nil {
			http.Error(w, `{"error": "failed to save preference"}`, http.StatusInternalServerError)
			return
		}
	}

	country := strings.TrimSpace(r.Header.Get("CF-IPCountry"))
	if country == "" {
		country = strings.TrimSpace(r.Header.Get("X-Country-Code"))
	}

	if preferredCurrency != nil {
		utils.SetCookie(w, "wy_currency", *preferredCurrency, "/", time.Now().Add(365*24*time.Hour), 365*24*3600, h.secureCookie, true)
		res := money.ResolveDisplayCurrency(money.ResolveInput{
			ProfileCurrency: *preferredCurrency,
			CookieCurrency:  *preferredCurrency,
			CountryCode:     country,
		})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(application.CurrencyPreferenceResponse{
			Preference: preferredCurrency,
			Effective:  res.Currency,
			Source:     string(res.Source),
		})
	} else {
		utils.SetCookie(w, "wy_currency", "", "/", time.Unix(0, 0), -1, h.secureCookie, true)
		res := money.ResolveDisplayCurrency(money.ResolveInput{
			CountryCode: country,
		})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(application.CurrencyPreferenceResponse{
			Preference: nil,
			Effective:  res.Currency,
			Source:     string(res.Source),
		})
	}
}
