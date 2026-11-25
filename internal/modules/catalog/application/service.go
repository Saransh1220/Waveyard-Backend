package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/catalog/domain"
	"github.com/saransh1220/blueprint-audio/internal/shared/money"
)

const (
	defaultSpecPage  = 1
	defaultSpecLimit = 20
	maxSpecLimit     = 50
	shortCodeLength  = 8
	currencyINR      = money.CurrencyINR
	currencyUSD      = money.CurrencyUSD

	defaultHomeLimit = 8
	maxHomeLimit     = 20
	homeCacheTTL     = 900
)

type SpecService interface {
	CreateSpec(ctx context.Context, spec *domain.Spec) error
	GetSpec(ctx context.Context, id uuid.UUID) (*domain.Spec, error)
	ListSpecs(ctx context.Context, filter domain.SpecFilter) ([]domain.Spec, int, error)
	UpdateSpec(ctx context.Context, spec *domain.Spec, producerID uuid.UUID) error
	UpdateFilesAndStatus(ctx context.Context, id uuid.UUID, files map[string]*string, status domain.ProcessingStatus) error
	DeleteSpec(ctx context.Context, id uuid.UUID, producerId uuid.UUID) error
	GetUserSpecs(ctx context.Context, producerID uuid.UUID, page, limit int, filters ...domain.ProducerSpecFilter) ([]domain.Spec, int, error)
	GetSpecByShortCode(ctx context.Context, code string) (*domain.Spec, error)
	GetSpecBySlug(ctx context.Context, slug string) (*domain.Spec, error)
	GetHome(ctx context.Context, params domain.HomepageParams) (*domain.HomepageData, error)
}

type specService struct {
	repo domain.SpecRepository
}

func NewSpecService(repo domain.SpecRepository) SpecService {
	return &specService{repo: repo}
}

func (s *specService) CreateSpec(ctx context.Context, spec *domain.Spec) error {
	if err := s.prepareSpecForCreate(ctx, spec); err != nil {
		return err
	}
	return s.repo.Create(ctx, spec)
}

func (s *specService) prepareSpecForCreate(ctx context.Context, spec *domain.Spec) error {
	if err := validateSpec(spec); err != nil {
		return err
	}
	if spec.Category == domain.CategoryBeat {
		if spec.ProcessingStatus != domain.ProcessingStatusProcessing {
			if spec.WavUrl == nil || *spec.WavUrl == "" {
				return errors.New("WAV file is required!")
			}
			if spec.StemsUrl == nil || *spec.StemsUrl == "" {
				return errors.New("stems file is mandatory for beats")
			}
		}
	}

	if spec.Slug == nil || strings.TrimSpace(*spec.Slug) == "" {
		slug, err := s.generateUniqueSlug(ctx, spec.Title)
		if err != nil {
			return err
		}
		spec.Slug = &slug
	}

	if spec.ShortCode == nil || strings.TrimSpace(*spec.ShortCode) == "" {
		shortCode, err := s.generateUniqueShortCode(ctx)
		if err != nil {
			return err
		}
		spec.ShortCode = &shortCode
	}

	if err := prepareLicensePrices(spec, nil); err != nil {
		return err
	}

	return nil
}

func (s *specService) GetSpec(ctx context.Context, id uuid.UUID) (*domain.Spec, error) {
	return s.repo.GetByID(ctx, id)
}

// The `GetSpecByShortCode` function in the `specService` struct is used to retrieve a specific spec by
// its short code from the repository. It takes the context and the short code string as input
// parameters and returns a pointer to the `domain.Spec` struct along with an error. Inside the
// function, it calls the `GetByShortCode` method of the repository (`s.repo`) passing the context and
// the short code to fetch the spec based on the provided short code.
func (s *specService) GetSpecByShortCode(ctx context.Context, code string) (*domain.Spec, error) {
	return s.repo.GetByShortCode(ctx, code)
}

// The `GetSpecBySlug` function in the `specService` struct is used to retrieve a specific spec by its
// slug from the repository. It takes the context and the slug string as input parameters and returns a
// pointer to the `domain.Spec` struct along with an error. Inside the function, it calls the
// `GetBySlug` method of the repository (`s.repo`) passing the context and the slug to fetch the spec
// based on the provided slug.
func (s *specService) GetSpecBySlug(ctx context.Context, slug string) (*domain.Spec, error) {
	return s.repo.GetBySlug(ctx, slug)
}

func (s *specService) ListSpecs(ctx context.Context, filter domain.SpecFilter) ([]domain.Spec, int, error) {
	filter.Page, filter.Limit = normalizePageAndLimit(filter.Page, filter.Limit)
	filter.Offset = (filter.Page - 1) * filter.Limit
	return s.repo.List(ctx, filter)
}

func (s *specService) GetHome(ctx context.Context, params domain.HomepageParams) (*domain.HomepageData, error) {
	limit := normalizeHomeLimit(params.Limit)
	period := normalizeHomePeriod(params.Period)
	sections := normalizeHomeSections(params.Sections)
	log.Printf("[Catalog Home] building homepage limit=%d period=%s sections=%v", limit, period, sections)

	stats, err := s.repo.GetHomepageStats(ctx)
	if err != nil {
		log.Printf("[Catalog Home] stats query failed: %v", err)
		return nil, err
	}
	log.Printf("[Catalog Home] stats total_live_beats=%d new_releases_7d=%d total_producers=%d", stats.TotalLiveBeats, stats.NewReleases7D, stats.TotalProducers)

	data := &domain.HomepageData{
		GeneratedAt:     time.Now().UTC(),
		CacheTTLSeconds: homeCacheTTL,
		Stats:           *stats,
		Sections:        make(map[string]domain.HomepageSection, len(sections)),
	}

	var newestFallback []domain.Spec
	getFallback := func() ([]domain.Spec, error) {
		if newestFallback != nil {
			return newestFallback, nil
		}
		specs, err := s.repo.GetNewestBeats(ctx, limit)
		if err != nil {
			log.Printf("[Catalog Home] newest fallback query failed: %v", err)
			return nil, err
		}
		log.Printf("[Catalog Home] loaded newest fallback specs count=%d", len(specs))
		newestFallback = specs
		return newestFallback, nil
	}

	for _, section := range sections {
		switch section {
		case domain.HomeSectionFeatured:
			specs, err := getFallback()
			if err != nil {
				return nil, err
			}
			log.Printf("[Catalog Home] section=%s source=fallback items=%d", section, len(specs))
			data.Sections[section] = domain.HomepageSection{
				Title:  "Featured beats",
				Source: "fallback",
				Items:  specsToRankedItems(specs),
			}
		case domain.HomeSectionNewReleases:
			specs, err := getFallback()
			if err != nil {
				return nil, err
			}
			log.Printf("[Catalog Home] section=%s source=query items=%d", section, len(specs))
			data.Sections[section] = domain.HomepageSection{
				Title:  "New releases",
				Source: "query",
				Items:  specsToRankedItems(specs),
			}
		case domain.HomeSectionTrending:
			items, err := s.getRankedHomeItems(ctx, domain.HomeSectionTrending, domain.HomePeriod24H, limit)
			if err != nil {
				log.Printf("[Catalog Home] ranked section=%s unavailable, using fallback: %v", section, err)
			}
			if len(items) == 0 {
				specs, err := getFallback()
				if err != nil {
					return nil, err
				}
				items = specsToRankedItems(specs)
				log.Printf("[Catalog Home] section=%s source=fallback items=%d", section, len(items))
			} else {
				log.Printf("[Catalog Home] section=%s source=algorithmic period=%s items=%d", section, domain.HomePeriod24H, len(items))
			}
			data.Sections[section] = domain.HomepageSection{
				Title:  "Trending now",
				Source: "algorithmic",
				Period: domain.HomePeriod24H,
				Items:  items,
			}
		case domain.HomeSectionTopCharts:
			items, err := s.getRankedHomeItems(ctx, domain.HomeSectionTopCharts, period, limit)
			if err != nil {
				log.Printf("[Catalog Home] ranked section=%s unavailable, using fallback: %v", section, err)
			}
			if len(items) == 0 {
				specs, err := getFallback()
				if err != nil {
					return nil, err
				}
				items = specsToRankedItems(specs)
				log.Printf("[Catalog Home] section=%s source=fallback items=%d", section, len(items))
			} else {
				log.Printf("[Catalog Home] section=%s source=algorithmic period=%s items=%d", section, period, len(items))
			}
			data.Sections[section] = domain.HomepageSection{
				Title:  "Top charts",
				Source: "algorithmic",
				Period: period,
				Items:  items,
			}
		}
	}

	log.Printf("[Catalog Home] built homepage sections=%d", len(data.Sections))
	return data, nil
}

func (s *specService) getRankedHomeItems(ctx context.Context, section, period string, limit int) ([]domain.RankedSpec, error) {
	log.Printf("[Catalog Home] loading ranked items section=%s period=%s limit=%d", section, period, limit)
	if err := s.ensureRankingFresh(ctx, section, period); err != nil {
		log.Printf("[Catalog Home] ranking refresh failed section=%s period=%s err=%v", section, period, err)
	}

	rows, err := s.repo.GetRankedSpecs(ctx, section, period, limit)
	if err != nil {
		log.Printf("[Catalog Home] ranked read failed section=%s period=%s err=%v", section, period, err)
		return nil, err
	}
	log.Printf("[Catalog Home] ranked read succeeded section=%s period=%s rows=%d", section, period, len(rows))
	items := make([]domain.RankedSpec, len(rows))
	for i, row := range rows {
		items[i] = domain.RankedSpec{
			Spec:         row.Spec,
			Rank:         row.Rank,
			Score:        &row.Score,
			Movement:     movementForRanks(row.Rank, row.PreviousRank),
			Metrics:      metricsPointer(parseRankingMetrics(row.MetricsJSON), row.MetricsJSON),
			CalculatedAt: row.CalculatedAt,
		}
	}
	return items, nil
}

func (s *specService) ensureRankingFresh(ctx context.Context, section, period string) error {
	freshness, err := s.repo.GetRankingFreshness(ctx, section, period)
	if err != nil {
		log.Printf("[Catalog Home] ranking freshness check failed section=%s period=%s err=%v", section, period, err)
		return err
	}

	maxAge := time.Hour
	if section == domain.HomeSectionTrending {
		maxAge = 15 * time.Minute
	}

	if freshness.Count > 0 && !freshness.CalculatedAt.IsZero() && time.Since(freshness.CalculatedAt) < maxAge {
		log.Printf("[Catalog Home] ranking fresh section=%s period=%s count=%d calculated_at=%s", section, period, freshness.Count, freshness.CalculatedAt.Format(time.RFC3339))
		return nil
	}
	log.Printf("[Catalog Home] ranking stale/missing section=%s period=%s count=%d calculated_at=%s; recalculating", section, period, freshness.Count, freshness.CalculatedAt.Format(time.RFC3339))
	return s.repo.RecalculateBeatRankings(ctx, section, period)
}

func (s *specService) DeleteSpec(ctx context.Context, id uuid.UUID, producerId uuid.UUID) error {
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return domain.ErrSpecNotFound
	}
	if existing.ProducerID != producerId {
		return domain.ErrUnauthorized
	}
	if isProcessingSpec(existing.ProcessingStatus) {
		return domain.ErrSpecProcessing
	}
	return s.repo.Delete(ctx, id, producerId)
}

// UpdateSpec updates a spec's metadata with ownership validation
func (s *specService) UpdateSpec(ctx context.Context, spec *domain.Spec, producerID uuid.UUID) error {
	// Validate ownership
	existing, err := s.repo.GetByID(ctx, spec.ID)
	if err != nil {
		return err
	}
	if existing == nil {
		return errors.New("spec not found")
	}
	if existing.ProducerID != producerID {
		return errors.New("unauthorized: you can only update your own specs")
	}
	if isProcessingSpec(existing.ProcessingStatus) {
		return domain.ErrSpecProcessing
	}

	// Validate updates
	if err := validateSpec(spec); err != nil {
		return err
	}
	if err := prepareLicensePrices(spec, existing); err != nil {
		return err
	}

	// Set producer ID to ensure it doesn't change
	spec.ProducerID = producerID
	spec.ShortCode = existing.ShortCode
	if spec.Slug == nil || strings.TrimSpace(*spec.Slug) == "" {
		spec.Slug = existing.Slug
	}
	return s.repo.Update(ctx, spec)
}

func isProcessingSpec(status domain.ProcessingStatus) bool {
	return status == domain.ProcessingStatusPending ||
		status == domain.ProcessingStatusProcessing
}

// GetUserSpecs retrieves all specs for a specific producer with pagination
func (s *specService) GetUserSpecs(ctx context.Context, producerID uuid.UUID, page, limit int, filters ...domain.ProducerSpecFilter) ([]domain.Spec, int, error) {
	page, limit = normalizePageAndLimit(page, limit)
	offset := (page - 1) * limit
	return s.repo.ListByUserID(ctx, producerID, limit, offset, filters...)
}

func (s *specService) UpdateFilesAndStatus(ctx context.Context, id uuid.UUID, files map[string]*string, status domain.ProcessingStatus) error {
	return s.repo.UpdateFilesAndStatus(ctx, id, files, status)
}

func normalizePageAndLimit(page, limit int) (int, int) {
	if page < 1 {
		page = defaultSpecPage
	}
	if limit <= 0 {
		limit = defaultSpecLimit
	}
	if limit > maxSpecLimit {
		limit = maxSpecLimit
	}
	return page, limit
}

func normalizeHomeLimit(limit int) int {
	if limit <= 0 {
		return defaultHomeLimit
	}
	if limit > maxHomeLimit {
		return maxHomeLimit
	}
	return limit
}

func normalizeHomePeriod(period string) string {
	switch strings.ToLower(strings.TrimSpace(period)) {
	case domain.HomePeriod24H:
		return domain.HomePeriod24H
	case domain.HomePeriod7D:
		return domain.HomePeriod7D
	case domain.HomePeriod30D:
		return domain.HomePeriod30D
	default:
		return domain.HomePeriod30D
	}
}

func normalizeHomeSections(sections []string) []string {
	if len(sections) == 0 {
		return []string{
			domain.HomeSectionFeatured,
			domain.HomeSectionTrending,
			domain.HomeSectionTopCharts,
			domain.HomeSectionNewReleases,
		}
	}

	allowed := map[string]bool{
		domain.HomeSectionFeatured:    true,
		domain.HomeSectionTrending:    true,
		domain.HomeSectionTopCharts:   true,
		domain.HomeSectionNewReleases: true,
	}
	seen := make(map[string]bool, len(sections))
	normalized := make([]string, 0, len(sections))
	for _, section := range sections {
		section = strings.ToLower(strings.TrimSpace(section))
		if allowed[section] && !seen[section] {
			seen[section] = true
			normalized = append(normalized, section)
		}
	}
	if len(normalized) == 0 {
		return normalizeHomeSections(nil)
	}
	return normalized
}

func specsToRankedItems(specs []domain.Spec) []domain.RankedSpec {
	items := make([]domain.RankedSpec, len(specs))
	for i := range specs {
		items[i] = domain.RankedSpec{
			Spec:     specs[i],
			Rank:     i + 1,
			Movement: "-",
		}
	}
	return items
}

func metricsPointer(metrics domain.BeatRankingMetrics, raw json.RawMessage) *domain.BeatRankingMetrics {
	if len(raw) == 0 {
		return nil
	}
	return &metrics
}

func movementForRanks(rank int, previousRank *int) string {
	if previousRank == nil || *previousRank == rank {
		return "-"
	}
	if rank < *previousRank {
		return "up"
	}
	return "down"
}

func parseRankingMetrics(raw []byte) domain.BeatRankingMetrics {
	var metrics domain.BeatRankingMetrics
	if len(raw) == 0 {
		return metrics
	}
	if err := json.Unmarshal(raw, &metrics); err != nil {
		return domain.BeatRankingMetrics{}
	}
	return metrics
}

func normalizeSpecCurrencies(spec *domain.Spec) error {
	currency, err := normalizeCurrency(spec.PriceCurrency, currencyINR)
	if err != nil {
		return err
	}
	spec.PriceCurrency = currency

	for i := range spec.Licenses {
		currency, err := normalizeCurrency(spec.Licenses[i].PriceCurrency, spec.PriceCurrency)
		if err != nil {
			return err
		}
		spec.Licenses[i].PriceCurrency = currency
	}
	return nil
}

func normalizeCurrency(value, fallback string) (string, error) {
	currency := strings.ToUpper(strings.TrimSpace(value))
	if currency == "" {
		currency = fallback
	}
	if _, ok := money.LookupCurrency(currency); !ok {
		return "", domain.ErrInvalidCurrency
	}
	return currency, nil
}

func (s *specService) generateUniqueSlug(ctx context.Context, title string) (string, error) {
	base := slugify(title)
	if base == "" {
		base = "untitled-beat"
	}

	slug := base
	for attempt := 0; attempt < 10; attempt++ {
		existing, err := s.repo.GetBySlug(ctx, slug)
		if err == nil && existing != nil {
			slug = fmt.Sprintf("%s-%s", base, randomCodeSegment(3))
			continue
		}
		return slug, nil
	}

	return "", errors.New("failed to generate unique slug")
}

func (s *specService) generateUniqueShortCode(ctx context.Context) (string, error) {
	for attempt := 0; attempt < 10; attempt++ {
		code := randomCodeSegment(shortCodeLength)
		existing, err := s.repo.GetByShortCode(ctx, code)
		if err == nil && existing != nil {
			continue
		}
		return code, nil
	}

	return "", errors.New("failed to generate unique short code")
}

func slugify(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	var out []rune
	prevDash := false

	for _, r := range input {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			out = append(out, r)
			prevDash = false
		case unicode.IsSpace(r) || r == '-' || r == '_' || r == '.':
			if !prevDash && len(out) > 0 {
				out = append(out, '-')
				prevDash = true
			}
		}
	}

	slug := strings.Trim(string(out), "-")
	return slug
}

func randomCodeSegment(length int) string {
	id := strings.ReplaceAll(uuid.NewString(), "-", "")
	if length <= 0 {
		return ""
	}
	if length > len(id) {
		return id
	}
	return strings.ToLower(id[:length])
}
