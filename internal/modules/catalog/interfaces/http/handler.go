package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/disintegration/imaging"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/saransh1220/blueprint-audio/internal/gateway/middleware"
	"github.com/saransh1220/blueprint-audio/internal/modules/catalog/application"
	"github.com/saransh1220/blueprint-audio/internal/modules/catalog/domain"
	"github.com/saransh1220/blueprint-audio/internal/shared/money"
)

type SpecHandler struct {
	service             application.SpecService
	fileService         FileService
	analyticsService    AnalyticsService
	notificationService NotificationService
	redisClient         *redis.Client
}

func NewSpecHandler(service application.SpecService, fileService FileService, analyticsService AnalyticsService, notificationService NotificationService, redisClient *redis.Client) *SpecHandler {
	return &SpecHandler{
		service:             service,
		fileService:         fileService,
		analyticsService:    analyticsService,
		notificationService: notificationService,
		redisClient:         redisClient,
	}
}

func (h *SpecHandler) cacheGet(ctx context.Context, key string) (string, bool) {
	if h.redisClient == nil {
		return "", false
	}

	val, err := h.redisClient.Get(ctx, key).Result()
	return val, err == nil
}

func (h *SpecHandler) cacheSet(ctx context.Context, key string, value []byte, expiration time.Duration) {
	if h.redisClient == nil {
		return
	}
	if err := h.redisClient.Set(ctx, key, value, expiration).Err(); err != nil {
		log.Printf("[CACHE SET FAILED] %s: %v", key, err)
	}
}

func (h *SpecHandler) cacheDel(ctx context.Context, key string) {
	if h.redisClient == nil {
		return
	}
	if err := h.redisClient.Del(ctx, key).Err(); err != nil {
		log.Printf("[CACHE INVALIDATE FAILED] %s: %v", key, err)
	}
}

func (h *SpecHandler) cacheDelSpec(ctx context.Context, specID uuid.UUID) {
	baseKey := "spec:prices-v2:" + specID.String()
	h.cacheDel(ctx, baseKey)
	h.cacheDel(ctx, baseKey+":"+money.CurrencyINR)
	h.cacheDel(ctx, baseKey+":"+money.CurrencyUSD)
}

// CreateGone retires the server-proxied multipart upload. New uploads must use
// the direct-to-object-storage session endpoints.
func (h *SpecHandler) CreateGone(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "multipart spec uploads are no longer supported; use /spec-uploads", http.StatusGone)
}

func (h *SpecHandler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")

	var spec *domain.Spec
	var err error

	id, uuidErr := uuid.Parse(idStr)
	if uuidErr == nil {
		spec, err = h.service.GetSpec(r.Context(), id)
	} else {
		// Not a UUID, try short code (length 8) or slug
		if len(idStr) == 8 {
			spec, err = h.service.GetSpecByShortCode(r.Context(), idStr)
		} else {
			spec, err = h.service.GetSpecBySlug(r.Context(), idStr)
		}
	}

	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if spec == nil {
		http.Error(w, "spec not found", http.StatusNotFound)
		return
	}

	// Cache only the catalog fields. Analytics belongs to the current request.
	displayCurrency := middleware.BuyerCurrencyFromContext(r.Context())
	cacheKey := "spec:prices-v2:" + spec.ID.String() + ":" + displayCurrency
	var response *SpecResponse
	cacheStatus := "MISS"
	if val, ok := h.cacheGet(r.Context(), cacheKey); ok {
		var cached SpecResponse
		if err := json.Unmarshal([]byte(val), &cached); err == nil {
			if cached.ProcessingStatus == string(domain.ProcessingStatusProcessing) {
				h.cacheDelSpec(r.Context(), spec.ID)
			} else {
				// Older cache entries may contain a different viewer's analytics.
				cached.Analytics = nil
				response = &cached
				cacheStatus = "HIT"
			}
		}
	}

	if response == nil {
		h.sanitizeSpec(spec)
		response = ToSpecResponseForCurrency(spec, displayCurrency)
		if spec.ProcessingStatus == domain.ProcessingStatusCompleted {
			if jsonBytes, err := json.Marshal(response); err == nil {
				go h.cacheSet(context.Background(), cacheKey, jsonBytes, 10*time.Minute)
			}
		}
	}

	var userIDPtr *uuid.UUID
	if userID, ok := r.Context().Value(middleware.ContextKeyUserId).(uuid.UUID); ok {
		userIDPtr = &userID
	}

	// Fetch analytics data
	analytics, err := h.analyticsService.GetPublicAnalytics(r.Context(), spec.ID, userIDPtr)
	if err == nil {
		response.Analytics = &SpecAnalytics{
			PlayCount:          analytics.PlayCount,
			FavoriteCount:      analytics.FavoriteCount,
			TotalDownloadCount: analytics.TotalDownloadCount,
			IsFavorited:        analytics.IsFavorited,
		}
	}

	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cache", cacheStatus)
	json.NewEncoder(w).Encode(response)
}

func (h *SpecHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	category := domain.Category(q.Get("category"))

	var genres []string
	if g := q.Get("genres"); g != "" {
		genres = strings.Split(g, ",")
	}
	var tags []string
	if t := q.Get("tags"); t != "" {
		tags = strings.Split(t, ",")
	}

	var moods []string
	if m := strings.TrimSpace(q.Get("moods")); m != "" {
		moods = strings.Split(m, ",")
	}

	var instruments []string
	if i := strings.TrimSpace(q.Get("instruments")); i != "" {
		instruments = strings.Split(i, ",")
	}

	search := q.Get("search")
	key := q.Get("key")
	if key == "All" {
		key = ""
	}

	minBPM, _ := strconv.Atoi(q.Get("min_bpm"))
	maxBPM, _ := strconv.Atoi(q.Get("max_bpm"))

	minDuration, _ := strconv.Atoi(q.Get("min_duration"))
	maxDuration, _ := strconv.Atoi(q.Get("max_duration"))

	minPrice := -1.0
	maxPrice := 0.0
	for _, entry := range []struct {
		key    string
		target *float64
	}{{"min_price", &minPrice}, {"max_price", &maxPrice}} {
		if raw := q.Get(entry.key); raw != "" {
			value, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				http.Error(w, "invalid price filter", http.StatusBadRequest)
				return
			}
			if _, err := money.MajorToMinor(value); err != nil {
				http.Error(w, "invalid price filter", http.StatusBadRequest)
				return
			}
			*entry.target = value
		}
	}
	if maxPrice > 0 && minPrice > maxPrice {
		http.Error(w, "invalid price range", http.StatusBadRequest)
		return
	}

	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}

	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit, _ = strconv.Atoi(q.Get("per_page"))
	}
	if limit <= 0 {
		limit = 20
	}

	filter := domain.SpecFilter{
		Currency:    middleware.BuyerCurrencyFromContext(r.Context()),
		Category:    category,
		Genres:      genres,
		Tags:        tags,
		Moods:       moods,
		Instruments: instruments,
		Search:      search,
		MinBPM:      minBPM,
		MaxBPM:      maxBPM,
		MinPrice:    minPrice,
		MaxPrice:    maxPrice,
		MinDuration: minDuration,
		MaxDuration: maxDuration,
		Key:         key,
		Sort:        q.Get("sort"),
		Page:        page,
		Limit:       limit,
	}

	specs, total, err := h.service.ListSpecs(r.Context(), filter)
	if err != nil {
		log.Printf("[SpecHandler.List] Error: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Get user ID if authenticated (optional)
	var userIDPtr *uuid.UUID
	if userID, ok := r.Context().Value(middleware.ContextKeyUserId).(uuid.UUID); ok {
		userIDPtr = &userID
	}

	responses := make([]SpecResponse, len(specs))

	for i := range specs {
		h.sanitizeSpec(&specs[i])
		responses[i] = *ToSpecResponseForCurrency(&specs[i], middleware.BuyerCurrencyFromContext(r.Context()))

		// Fetch analytics for each spec
		analytics, err := h.analyticsService.GetPublicAnalytics(r.Context(), specs[i].ID, userIDPtr)
		if err == nil {
			responses[i].Analytics = &SpecAnalytics{
				PlayCount:          analytics.PlayCount,
				FavoriteCount:      analytics.FavoriteCount,
				TotalDownloadCount: analytics.TotalDownloadCount,
				IsFavorited:        analytics.IsFavorited,
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": responses,
		"metadata": map[string]interface{}{
			"total":    total,
			"page":     page,
			"per_page": limit,
			"limit":    limit,
			"offset":   (page - 1) * limit,
			"total_pages": func() int {
				if limit <= 0 {
					return 1
				}
				if total == 0 {
					return 1
				}
				return (total + limit - 1) / limit
			}(),
		},
	})
}

func (h *SpecHandler) Home(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit, _ := strconv.Atoi(q.Get("limit"))
	sections := []string{}
	if rawSections := strings.TrimSpace(q.Get("sections")); rawSections != "" {
		for _, section := range strings.Split(rawSections, ",") {
			sections = append(sections, strings.TrimSpace(section))
		}
	}
	log.Printf("[SpecHandler.Home] request limit=%d period=%q sections=%v raw_query=%q", limit, q.Get("period"), sections, r.URL.RawQuery)

	home, err := h.service.GetHome(r.Context(), domain.HomepageParams{
		Limit:    limit,
		Sections: sections,
		Period:   q.Get("period"),
	})
	if err != nil {
		log.Printf("[SpecHandler.Home] Error: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var userIDPtr *uuid.UUID
	if userID, ok := r.Context().Value(middleware.ContextKeyUserId).(uuid.UUID); ok {
		userIDPtr = &userID
	}

	displayCurrency := middleware.BuyerCurrencyFromContext(r.Context())
	response := HomepageResponse{
		GeneratedAt:     home.GeneratedAt,
		CacheTTLSeconds: home.CacheTTLSeconds,
		Stats: HomepageStatsResponse{
			TotalLiveBeats: home.Stats.TotalLiveBeats,
			NewReleases7D:  home.Stats.NewReleases7D,
			TotalProducers: home.Stats.TotalProducers,
		},
		Sections: make(map[string]HomepageSectionResponse, len(home.Sections)),
	}

	for key, section := range home.Sections {
		items := make([]HomepageItemResponse, len(section.Items))
		log.Printf("[SpecHandler.Home] serializing section=%s source=%s period=%s items=%d", key, section.Source, section.Period, len(section.Items))
		var wg sync.WaitGroup
		for i, item := range section.Items {
			wg.Add(1)
			go func(i int, item domain.RankedSpec) {
				defer wg.Done()
				spec := item.Spec
				h.sanitizeSpec(&spec)
				specResponse := *ToSpecResponseForCurrency(&spec, displayCurrency)

				analytics, err := h.analyticsService.GetPublicAnalytics(r.Context(), spec.ID, userIDPtr)
				if err == nil {
					specResponse.Analytics = &SpecAnalytics{PlayCount: analytics.PlayCount, FavoriteCount: analytics.FavoriteCount, TotalDownloadCount: analytics.TotalDownloadCount, IsFavorited: analytics.IsFavorited}
				}
				items[i] = HomepageItemResponse{Rank: item.Rank, Score: item.Score, Movement: item.Movement, Metrics: item.Metrics, Spec: specResponse}
			}(i, item)
		}
		wg.Wait()
		response.Sections[key] = HomepageSectionResponse{
			Title:  section.Title,
			Source: section.Source,
			Period: section.Period,
			Items:  items,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	log.Printf("[SpecHandler.Home] success sections=%d currency=%s", len(response.Sections), displayCurrency)
	json.NewEncoder(w).Encode(response)
}

func (h *SpecHandler) Delete(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	producerID, ok := r.Context().Value(middleware.ContextKeyUserId).(uuid.UUID)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// 1. Get Spec details before deletion (to get file URLs)
	spec, err := h.service.GetSpec(r.Context(), id)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if spec == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// 2. Verify ownership
	if spec.ProducerID != producerID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if spec.ProcessingStatus == domain.ProcessingStatusPending ||
		spec.ProcessingStatus == domain.ProcessingStatusProcessing {
		http.Error(w, domain.ErrSpecProcessing.Error(), http.StatusConflict)
		return
	}

	// 3. Delete from DB
	if err := h.service.DeleteSpec(r.Context(), id, producerID); err != nil {
		if errors.Is(err, money.ErrStalePrice) {
			http.Error(w, "prices changed; reload before saving", http.StatusConflict)
			return
		}
		if err == domain.ErrSpecProcessing {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if err == domain.ErrSpecNotFound {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err == domain.ErrSpecSoftDeleted {
			// Spec was soft-deleted because it has existing purchases.
			// Do NOT delete files from storage.
			log.Printf("Spec %s was soft deleted (purchased). Skipping file deletion.", idStr)

			// Invalidate Cache
			h.cacheDelSpec(context.Background(), id)
			log.Printf("[CACHE INVALIDATE] Deleted Spec ID: %s", idStr)

			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// 4. Delete files from Storage (Async or Sync - here Sync for simplicity)
	ctx := context.Background() // Use background context for cleanup to ensure it runs even if request cancels

	// Helper to delete file by URL
	deleteFile := func(url string) {
		if url == "" {
			return
		}
		key, err := h.fileService.GetKeyFromUrl(url)
		if err != nil {
			// Log error but continue
			return
		}
		_ = h.fileService.Delete(ctx, key)
	}

	deleteFile(spec.ImageUrl)
	deleteFile(spec.PreviewUrl)
	if spec.WavUrl != nil {
		deleteFile(*spec.WavUrl)
	}
	if spec.StemsUrl != nil {
		deleteFile(*spec.StemsUrl)
	}

	// Invalidate Cache
	h.cacheDelSpec(context.Background(), id)
	log.Printf("[CACHE INVALIDATE] Deleted Spec ID: %s", idStr)

	w.WriteHeader(http.StatusNoContent)
}

// sanitizeSpec generates presigned URLs for audio files to enable range request streaming
func (h *SpecHandler) sanitizeSpec(spec *domain.Spec) {
	ctx := context.Background()

	// For audio files, generate presigned URLs (1 hour expiration)
	// This enables range requests and chunked downloading
	expiration := time.Hour * 1

	// Helper to get key from URL and generate presigned URL
	generatePresignedURL := func(url string) (string, error) {
		if url == "" {
			return "", nil
		}
		key, err := h.fileService.GetKeyFromUrl(url)
		if err != nil {
			// If we can't parse the key, return original URL
			return url, nil
		}
		presignedURL, err := h.fileService.GetPresignedDownloadURL(ctx, key, spec.Title, expiration)
		if err != nil {
			return url, err
		}
		return presignedURL, nil
	}

	// Generate presigned URLs for audio files
	if presignedURL, err := generatePresignedURL(spec.PreviewUrl); err == nil && presignedURL != "" {
		spec.PreviewUrl = presignedURL
	}

	if spec.ImageUrl != "" {
		// Use regular presigned URL for image preview.
		imageKey, err := h.fileService.GetKeyFromUrl(spec.ImageUrl)
		if err == nil {
			if presignedURL, err := h.fileService.GetPresignedURL(ctx, imageKey, expiration); err == nil {
				spec.ImageUrl = presignedURL
			}
		}
	}
}

// Update handles PATCH /specs/:id - updates spec metadata and optionally the cover image
func (h *SpecHandler) Update(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	producerID, ok := r.Context().Value(middleware.ContextKeyUserId).(uuid.UUID)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// 1. Fetch existing spec first
	existingSpec, err := h.service.GetSpec(r.Context(), id)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if existingSpec == nil {
		http.Error(w, "spec not found", http.StatusNotFound)
		return
	}
	if existingSpec.ProducerID != producerID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if existingSpec.ProcessingStatus == domain.ProcessingStatusPending ||
		existingSpec.ProcessingStatus == domain.ProcessingStatusProcessing {
		http.Error(w, domain.ErrSpecProcessing.Error(), http.StatusConflict)
		return
	}

	// 2. Parse Multipart Form
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "file too large", http.StatusBadRequest)
		return
	}

	// 3. Extract and Apply Metadata
	metadata := r.FormValue("metadata")
	var updateData domain.Spec
	if err := json.Unmarshal([]byte(metadata), &updateData); err != nil {
		http.Error(w, "invalid metadata json", http.StatusBadRequest)
		return
	}

	// Update fields
	existingSpec.Title = updateData.Title
	existingSpec.BasePrice = updateData.BasePrice
	existingSpec.BPM = updateData.BPM
	existingSpec.Key = updateData.Key
	existingSpec.Tags = updateData.Tags
	existingSpec.Description = updateData.Description
	existingSpec.FreeMp3Enabled = updateData.FreeMp3Enabled
	existingSpec.Licenses = updateData.Licenses

	// 4. Handle Image Replacement
	file, _, err := r.FormFile("image")
	if err != nil && err != http.ErrMissingFile {
		http.Error(w, "invalid file", http.StatusBadRequest)
		return
	}

	var (
		oldImageURL    string
		newUploadedKey string
	)

	if file != nil {
		defer file.Close()

		// Resize and Upload New Image
		src, err := imaging.Decode(file)
		if err != nil {
			http.Error(w, "failed to decode image", http.StatusBadRequest)
			return
		}

		dst := imaging.Fit(src, 500, 500, imaging.Lanczos)
		buf := new(bytes.Buffer)
		if err := imaging.Encode(buf, dst, imaging.JPEG, imaging.JPEGQuality(80)); err != nil {
			http.Error(w, "failed to encode image", http.StatusInternalServerError)
			return
		}

		id, err := uuid.NewV7()
		if err != nil {
			http.Error(w, "failed to generate uuid", http.StatusInternalServerError)
			return
		}
		filename := fmt.Sprintf("%s.jpg", id.String())
		key := fmt.Sprintf("images/%s", filename)

		url, err := h.fileService.UploadWithKey(r.Context(), buf, key, "image/jpeg")
		if err != nil {
			http.Error(w, "failed to upload image: "+err.Error(), http.StatusInternalServerError)
			return
		}

		oldImageURL = existingSpec.ImageUrl
		newUploadedKey = key
		existingSpec.ImageUrl = url
	}

	// 5. Save Updates
	if err := h.service.UpdateSpec(r.Context(), existingSpec, producerID); err != nil {
		if newUploadedKey != "" {
			_ = h.fileService.Delete(context.Background(), newUploadedKey)
		}
		if errors.Is(err, money.ErrStalePrice) {
			http.Error(w, "prices changed; reload before saving", http.StatusConflict)
			return
		}
		if err == domain.ErrSpecProcessing {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Delete old image only after successful database update
	if oldImageURL != "" {
		if oldKey, err := h.fileService.GetKeyFromUrl(oldImageURL); err == nil {
			_ = h.fileService.Delete(context.Background(), oldKey)
		}
	}

	// 6. Return Updated Spec
	h.sanitizeSpec(existingSpec)
	response := ToSpecResponseForCurrency(existingSpec, middleware.BuyerCurrencyFromContext(r.Context()))

	// Invalidate Cache
	h.cacheDelSpec(context.Background(), id)
	log.Printf("[CACHE INVALIDATE] Updated Spec ID: %s", idStr)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (h *SpecHandler) GetUserSpecs(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.PathValue("id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}

	q := r.URL.Query()

	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}

	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit, _ = strconv.Atoi(q.Get("per_page"))
	}
	if limit <= 0 {
		limit = 20
	}

	specs, total, err := h.service.GetUserSpecs(r.Context(), userID, page, limit)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Get current user ID (viewer) if authenticated
	var currentUserIDPtr *uuid.UUID
	if currentUserID, ok := r.Context().Value(middleware.ContextKeyUserId).(uuid.UUID); ok {
		currentUserIDPtr = &currentUserID
	}

	responses := make([]SpecResponse, len(specs))

	for i := range specs {
		h.sanitizeSpec(&specs[i])
		responses[i] = *ToSpecResponseForCurrency(&specs[i], middleware.BuyerCurrencyFromContext(r.Context()))

		// Fetch analytics for each spec
		analytics, err := h.analyticsService.GetPublicAnalytics(r.Context(), specs[i].ID, currentUserIDPtr)
		if err == nil {
			responses[i].Analytics = &SpecAnalytics{
				PlayCount:          analytics.PlayCount,
				FavoriteCount:      analytics.FavoriteCount,
				TotalDownloadCount: analytics.TotalDownloadCount,
				IsFavorited:        analytics.IsFavorited,
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": responses,
		"metadata": map[string]interface{}{
			"total":    total,
			"page":     page,
			"per_page": limit,
			"limit":    limit,
			"offset":   (page - 1) * limit,
			"total_pages": func() int {
				if limit <= 0 {
					return 1
				}
				if total == 0 {
					return 1
				}
				return (total + limit - 1) / limit
			}(),
		},
	})
}

// DownloadFree handles POST /specs/{id}/download-free
func (h *SpecHandler) DownloadFree(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	// 1. Get Spec
	spec, err := h.service.GetSpec(r.Context(), id)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if spec == nil {
		http.Error(w, "spec not found", http.StatusNotFound)
		return
	}

	// 2. Check if Free Download is enabled
	if !spec.FreeMp3Enabled {
		http.Error(w, "free download not enabled for this spec", http.StatusForbidden)
		return
	}

	// 3. Track Download (Async)
	go func() {
		if err := h.analyticsService.TrackFreeDownload(context.Background(), id); err != nil {
			log.Printf("Failed to track free download for spec %s: %v", id, err)
		}
	}()

	// 4. Generate Presigned URL
	// Using PreviewUrl as the free MP3 source
	if spec.PreviewUrl == "" {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	key, err := h.fileService.GetKeyFromUrl(spec.PreviewUrl)
	if err != nil {
		// Log error and return regular URL if key extraction fails
		log.Printf("Failed to extract key from url %s: %v", spec.PreviewUrl, err)
		json.NewEncoder(w).Encode(map[string]string{"url": spec.PreviewUrl})
		return
	}

	// Generate download URL with 1 hour expiration
	// We use the spec title as the filename
	filename := fmt.Sprintf("%s.mp3", spec.Title) // Assuming MP3
	downloadUrl, err := h.fileService.GetPresignedDownloadURL(r.Context(), key, filename, time.Hour)
	if err != nil {
		http.Error(w, "failed to generate download url", http.StatusInternalServerError)
		return
	}

	// 5. Return URL
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": downloadUrl})
}
