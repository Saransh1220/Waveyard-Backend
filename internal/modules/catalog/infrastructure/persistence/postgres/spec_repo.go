package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/saransh1220/blueprint-audio/internal/modules/catalog/domain"
	"github.com/saransh1220/blueprint-audio/internal/shared/money"
)

var errRankingRefreshSkipped = errors.New("ranking refresh skipped because another worker holds the lock")

type PgSpecRepository struct {
	db *sqlx.DB
}

func NewSpecRepository(db *sqlx.DB) *PgSpecRepository {
	return &PgSpecRepository{db: db}
}

func (r *PgSpecRepository) Create(ctx context.Context, spec *domain.Spec) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := createSpecTx(ctx, tx, spec); err != nil {
		return err
	}
	return tx.Commit()
}

// createSpecTx inserts a complete spec aggregate using the caller's transaction.
// Upload finalization uses the same helper so the spec and its durable processing
// job become visible atomically.
func createSpecTx(ctx context.Context, tx *sqlx.Tx, spec *domain.Spec) error {
	if spec.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		spec.ID = id
	}
	if spec.CreatedAt.IsZero() {
		spec.CreatedAt = time.Now()
	}
	spec.UpdatedAt = time.Now()

	// 1b. Set Default Processing Status
	if spec.ProcessingStatus == "" {
		spec.ProcessingStatus = domain.ProcessingStatusPending
	}

	query := `
        INSERT INTO specs (
            id, producer_id, title, category, type, bpm, key, 
            base_price, price_currency, image_url, preview_url, wav_url, stems_url,
            tags, description, duration, free_mp3_enabled,
            created_at, updated_at, processing_status,moods,instruments,slug,short_code
        ) VALUES (
            :id, :producer_id, :title, :category, :type, :bpm, :key, 
            :base_price, :price_currency, :image_url, :preview_url, :wav_url, :stems_url,
            :tags, :description, :duration, :free_mp3_enabled,
            :created_at, :updated_at, :processing_status, :moods, :instruments, :slug, :short_code
        )`

	_, err := tx.NamedExecContext(ctx, query, spec)
	if err != nil {
		return err
	}

	analyticsQuery := `INSERT INTO spec_analytics (spec_id) VALUES ($1) ON CONFLICT DO NOTHING`
	_, err = tx.ExecContext(ctx, analyticsQuery, spec.ID)
	if err != nil {
		return err
	}

	for _, genre := range spec.Genres {
		var genreID uuid.UUID

		if genre.ID != uuid.Nil {
			genreID = genre.ID
		} else {
			candidateID, err := uuid.NewV7()
			if err != nil {
				return err
			}
			// The no-op update makes PostgreSQL return the row that won a
			// concurrent first insert for this slug, without changing its
			// canonical name or ID.
			const getOrCreateGenreQuery = `
				INSERT INTO genres (id, name, slug, created_at)
				VALUES ($1, $2, $3, $4)
				ON CONFLICT (slug) DO UPDATE
				SET slug = EXCLUDED.slug
				RETURNING id`
			err = tx.GetContext(
				ctx,
				&genreID,
				getOrCreateGenreQuery,
				candidateID,
				genre.Name,
				genre.Slug,
				time.Now(),
			)
			if err != nil {
				return fmt.Errorf("failed to get or create genre %s: %w", genre.Name, err)
			}
		}

		genreQuery := `INSERT INTO spec_genres (spec_id, genre_id) VALUES ($1, $2)`
		_, err = tx.ExecContext(ctx, genreQuery, spec.ID, genreID)
		if err != nil {
			return err
		}
	}

	for i := range spec.Licenses {
		license := &spec.Licenses[i]
		if license.ID == uuid.Nil {
			id, err := uuid.NewV7()
			if err != nil {
				return err
			}
			license.ID = id
		}
		license.SpecID = spec.ID

		licenseQuery := `
            INSERT INTO license_options (
                id, spec_id, license_type, name, price, price_currency, features, file_types
            ) VALUES (
                :id, :spec_id, :license_type, :name, :price, :price_currency, :features, :file_types
            )`
		_, err = tx.NamedExecContext(ctx, licenseQuery, license)
		if err != nil {
			return err
		}

		if err := saveLicensePricesTx(ctx, tx, license.ID, license.Prices); err != nil {
			return err
		}
	}

	return nil
}

func (r *PgSpecRepository) hydrateLicenseOptionsPrices(ctx context.Context, licenses []domain.LicenseOption) error {
	if len(licenses) == 0 {
		return nil
	}
	licenseIDs := make([]uuid.UUID, len(licenses))
	for i, lic := range licenses {
		licenseIDs[i] = lic.ID
	}

	query, args, err := sqlx.In(`
		SELECT id, license_option_id, currency, amount_minor, amount_major, source, fx_rate_snapshot, revision, is_active, created_at, updated_at
		FROM license_option_prices
		WHERE license_option_id IN (?)
		ORDER BY currency ASC
	`, licenseIDs)
	if err != nil {
		return err
	}
	query = r.db.Rebind(query)

	var prices []money.StoredPrice
	err = r.db.SelectContext(ctx, &prices, query, args...)
	if err != nil {
		return fmt.Errorf("load license prices: %w", err)
	}

	priceMap := make(map[uuid.UUID][]money.StoredPrice)
	for _, p := range prices {
		priceMap[p.LicenseOptionID] = append(priceMap[p.LicenseOptionID], p)
	}

	for i := range licenses {
		if pList, ok := priceMap[licenses[i].ID]; ok {
			licenses[i].Prices = pList
		}
	}
	return nil
}

func (r *PgSpecRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Spec, error) {
	spec := &domain.Spec{}

	query := `
		SELECT s.*, u.display_name as producer_name, u.handle as producer_handle
		FROM specs s
		JOIN users u ON s.producer_id = u.id
		WHERE s.id = $1 AND s.is_deleted = FALSE
	`
	err := r.db.GetContext(ctx, spec, query, id)
	if err != nil {
		return nil, err
	}

	//fetch licences
	licenseQuery := `SELECT * FROM license_options WHERE spec_id = $1 AND is_deleted = FALSE`
	err = r.db.SelectContext(ctx, &spec.Licenses, licenseQuery, id)
	if err != nil {
		return nil, err
	}
	if err := r.hydrateLicenseOptionsPrices(ctx, spec.Licenses); err != nil {
		return nil, err
	}

	//fetch genres
	genreQuery := `SELECT g.* FROM genres g JOIN spec_genres sg ON g.id = sg.genre_id WHERE sg.spec_id = $1`

	err = r.db.SelectContext(ctx, &spec.Genres, genreQuery, id)
	if err != nil {
		return nil, err
	}
	return spec, nil
}

func (r *PgSpecRepository) GetBySlug(ctx context.Context, slug string) (*domain.Spec, error) {
	spec := &domain.Spec{}

	query := `
		SELECT s.*, u.display_name as producer_name, u.handle as producer_handle
		FROM specs s
		JOIN users u ON s.producer_id = u.id
		WHERE s.slug = $1 AND s.is_deleted = FALSE
	`
	err := r.db.GetContext(ctx, spec, query, slug)
	if err != nil {
		return nil, err
	}

	//fetch licences
	licenseQuery := `SELECT * FROM license_options WHERE spec_id = $1 AND is_deleted = FALSE`
	err = r.db.SelectContext(ctx, &spec.Licenses, licenseQuery, spec.ID)
	if err != nil {
		return nil, err
	}
	if err := r.hydrateLicenseOptionsPrices(ctx, spec.Licenses); err != nil {
		return nil, err
	}

	//fetch genres
	genreQuery := `SELECT g.* FROM genres g JOIN spec_genres sg ON g.id = sg.genre_id WHERE sg.spec_id = $1`

	err = r.db.SelectContext(ctx, &spec.Genres, genreQuery, spec.ID)
	if err != nil {
		return nil, err
	}
	return spec, nil

}

func (r *PgSpecRepository) GetByShortCode(ctx context.Context, shortCode string) (*domain.Spec, error) {
	spec := &domain.Spec{}

	query := `SELECT s.*, u.display_name as producer_name, u.handle as producer_handle
		FROM specs s 
		JOIN users u ON s.producer_id = u.id
		WHERE s.short_code = $1 AND s.is_deleted = FALSE`
	err := r.db.GetContext(ctx, spec, query, shortCode)
	if err != nil {
		return nil, err
	}

	//fetch licences
	licenseQuery := `SELECT * FROM license_options WHERE spec_id = $1 AND is_deleted = FALSE`
	err = r.db.SelectContext(ctx, &spec.Licenses, licenseQuery, spec.ID)
	if err != nil {
		return nil, err
	}
	if err := r.hydrateLicenseOptionsPrices(ctx, spec.Licenses); err != nil {
		return nil, err
	}

	//fetch genres
	genreQuery := `SELECT g.* FROM genres g JOIN spec_genres sg ON g.id = sg.genre_id WHERE sg.spec_id = $1`

	err = r.db.SelectContext(ctx, &spec.Genres, genreQuery, spec.ID)
	if err != nil {
		return nil, err
	}
	return spec, nil

}

func (r *PgSpecRepository) List(ctx context.Context, filter domain.SpecFilter) ([]domain.Spec, int, error) {
	// Use a struct to hold the result including the window function count
	var results []struct {
		domain.Spec
		TotalCount int `db:"total_count"`
	}

	query := `
		SELECT s.*, u.display_name as producer_name, u.handle as producer_handle, COUNT(*) OVER() as total_count
		FROM specs s
		JOIN users u ON s.producer_id = u.id
		WHERE s.is_deleted = FALSE
		  AND s.processing_status = 'completed'
	`
	args := []interface{}{}
	argId := 1
	priceColumn := "s.base_price"
	if filter.Currency != "" {
		currency, ok := money.LookupCurrency(filter.Currency)
		if !ok {
			return nil, 0, money.ErrInvalidCurrency
		}
		query = strings.Replace(query, "WHERE s.is_deleted", `LEFT JOIN LATERAL (
   SELECT MIN(p.amount_minor)::numeric / 100 AS amount FROM license_options lo
   JOIN license_option_prices p ON p.license_option_id=lo.id
   WHERE lo.spec_id=s.id AND lo.is_deleted=FALSE AND p.is_active=TRUE AND p.currency=$1
  ) selected_price ON TRUE WHERE s.is_deleted`, 1)
		args = append(args, currency.Code)
		argId++
		priceColumn = "selected_price.amount"
	}

	if filter.Category != "" {
		query += fmt.Sprintf(" AND s.category = $%d", argId)
		args = append(args, filter.Category)
		argId++
	}

	if len(filter.Genres) > 0 {
		query += fmt.Sprintf(` AND s.id IN (
            SELECT spec_id FROM spec_genres sg 
            JOIN genres g ON sg.genre_id = g.id 
            WHERE g.slug ILIKE ANY($%d::text[]) OR g.name ILIKE ANY($%d::text[])
        )`, argId, argId)
		args = append(args, pq.Array(filter.Genres))
		argId++
	}

	if len(filter.Tags) > 0 {
		query += fmt.Sprintf(" AND s.tags @> $%d", argId)
		args = append(args, pq.Array(filter.Tags))
		argId++
	}

	if filter.Search != "" {
		searchTerm := "%" + filter.Search + "%"
		query += fmt.Sprintf(` AND (
			s.title ILIKE $%d OR
			array_to_string(s.tags, ',') ILIKE $%d OR
			array_to_string(s.moods, ',') ILIKE $%d OR
			u.display_name ILIKE $%d OR
			s.description ILIKE $%d
		)`, argId, argId, argId, argId, argId)
		args = append(args, searchTerm)
		argId++
	}

	if filter.MinBPM > 0 {
		query += fmt.Sprintf(" AND s.bpm >= $%d", argId)
		args = append(args, filter.MinBPM)
		argId++
	}

	if filter.MaxBPM > 0 {
		query += fmt.Sprintf(" AND s.bpm <= $%d", argId)
		args = append(args, filter.MaxBPM)
		argId++
	}

	if filter.MinPrice >= 0 {
		query += fmt.Sprintf(" AND %s >= $%d", priceColumn, argId)
		args = append(args, filter.MinPrice)
		argId++
	}

	if filter.MaxPrice > 0 {
		query += fmt.Sprintf(" AND %s <= $%d", priceColumn, argId)
		args = append(args, filter.MaxPrice)
		argId++
	}

	if filter.Key != "" {
		query += fmt.Sprintf(" AND s.key = $%d", argId)
		args = append(args, filter.Key)
		argId++
	}

	if len(filter.Moods) > 0 {
		query += fmt.Sprintf(" AND s.moods && $%d", argId)
		args = append(args, pq.Array(filter.Moods))
		argId++
	}

	if len(filter.Instruments) > 0 {
		query += fmt.Sprintf(" AND s.instruments && $%d", argId)
		args = append(args, pq.Array(filter.Instruments))
		argId++
	}

	if filter.MinDuration > 0 {
		query += fmt.Sprintf(" AND s.duration >= $%d", argId)
		args = append(args, filter.MinDuration)
		argId++
	}

	if filter.MaxDuration > 0 {
		query += fmt.Sprintf(" AND s.duration <= $%d", argId)
		args = append(args, filter.MaxDuration)
		argId++
	}

	// Dynamic Sorting
	orderBy := "s.created_at DESC" // Default
	switch filter.Sort {
	case "newest":
		orderBy = "s.created_at DESC"
	case "oldest":
		orderBy = "s.created_at ASC"
	case "price_asc":
		orderBy = priceColumn + " ASC NULLS LAST"
	case "price_desc":
		orderBy = priceColumn + " DESC NULLS LAST"
	case "bpm_asc":
		orderBy = "s.bpm ASC"
	case "bpm_desc":
		orderBy = "s.bpm DESC"
	}

	query += fmt.Sprintf(" ORDER BY %s LIMIT $%d OFFSET $%d", orderBy, argId, argId+1)
	args = append(args, filter.Limit, filter.Offset)

	err := r.db.SelectContext(ctx, &results, query, args...)
	if err != nil {
		return nil, 0, err
	}

	if len(results) == 0 {
		return []domain.Spec{}, 0, nil
	}

	total := results[0].TotalCount
	specs := make([]domain.Spec, len(results))

	// Create a map for O(1) lookup to assign relations
	specMap := make(map[uuid.UUID]*domain.Spec, len(results))
	specIDs := make([]uuid.UUID, len(results))

	for i, res := range results {
		specs[i] = res.Spec
		specs[i].Genres = []domain.Genre{}           // Initialize empty slice
		specs[i].Licenses = []domain.LicenseOption{} // Initialize empty slice
		specMap[specs[i].ID] = &specs[i]
		specIDs[i] = specs[i].ID
	}

	// 1. Bulk Fetch Genres
	// Use sqlx.In to handle IN clause with slice
	genreQuery, args, err := sqlx.In(`
		SELECT sg.spec_id, g.* 
		FROM genres g 
		JOIN spec_genres sg ON g.id = sg.genre_id 
		WHERE sg.spec_id IN (?)`, specIDs)
	if err != nil {
		return nil, 0, err
	}
	genreQuery = r.db.Rebind(genreQuery)

	var genreRows []struct {
		SpecID uuid.UUID `db:"spec_id"`
		domain.Genre
	}

	err = r.db.SelectContext(ctx, &genreRows, genreQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to fetch genres: %w", err)
	}

	for _, row := range genreRows {
		if spec, ok := specMap[row.SpecID]; ok {
			spec.Genres = append(spec.Genres, row.Genre)
		}
	}

	// 2. Bulk Fetch Licenses
	licenseQuery, args, err := sqlx.In(`SELECT * FROM license_options WHERE spec_id IN (?) AND is_deleted = FALSE`, specIDs)
	if err != nil {
		return nil, 0, err
	}
	licenseQuery = r.db.Rebind(licenseQuery)

	var licenses []domain.LicenseOption
	err = r.db.SelectContext(ctx, &licenses, licenseQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to fetch licenses: %w", err)
	}

	if err := r.hydrateLicenseOptionsPrices(ctx, licenses); err != nil {
		return nil, 0, err
	}
	for _, lic := range licenses {
		if spec, ok := specMap[lic.SpecID]; ok {
			spec.Licenses = append(spec.Licenses, lic)
		}
	}

	return specs, total, nil
}

func (r *PgSpecRepository) GetHomepageStats(ctx context.Context) (*domain.HomepageStats, error) {
	stats := &domain.HomepageStats{}
	log.Printf("[CatalogRepo.Home] querying homepage stats")
	query := `
		SELECT
			COUNT(*) FILTER (
				WHERE s.category = 'beat'
				  AND s.processing_status = 'completed'
				  AND s.is_deleted = FALSE
			) AS total_live_beats,
			COUNT(*) FILTER (
				WHERE s.category = 'beat'
				  AND s.processing_status = 'completed'
				  AND s.is_deleted = FALSE
				  AND s.created_at >= NOW() - INTERVAL '7 days'
			) AS new_releases_7d,
			COUNT(DISTINCT s.producer_id) FILTER (
				WHERE s.category = 'beat'
				  AND s.processing_status = 'completed'
				  AND s.is_deleted = FALSE
			) AS total_producers
		FROM specs s`

	if err := r.db.GetContext(ctx, stats, query); err != nil {
		log.Printf("[CatalogRepo.Home] homepage stats query failed: %v", err)
		return nil, err
	}
	log.Printf("[CatalogRepo.Home] homepage stats loaded total_live_beats=%d new_releases_7d=%d total_producers=%d", stats.TotalLiveBeats, stats.NewReleases7D, stats.TotalProducers)
	return stats, nil
}

func (r *PgSpecRepository) GetNewestBeats(ctx context.Context, limit int) ([]domain.Spec, error) {
	if limit <= 0 {
		limit = 8
	}

	specs := []domain.Spec{}
	log.Printf("[CatalogRepo.Home] querying newest beats limit=%d", limit)
	query := `
		SELECT s.*, u.display_name as producer_name, u.handle as producer_handle
		FROM specs s
		JOIN users u ON s.producer_id = u.id
		WHERE s.category = 'beat'
		  AND s.processing_status = 'completed'
		  AND s.is_deleted = FALSE
		ORDER BY s.created_at DESC
		LIMIT $1`

	if err := r.db.SelectContext(ctx, &specs, query, limit); err != nil {
		log.Printf("[CatalogRepo.Home] newest beats query failed: %v", err)
		return nil, err
	}
	if err := r.hydrateSpecRelations(ctx, specs); err != nil {
		log.Printf("[CatalogRepo.Home] newest beats relation hydration failed: %v", err)
		return nil, err
	}
	log.Printf("[CatalogRepo.Home] newest beats loaded count=%d", len(specs))
	return specs, nil
}

func (r *PgSpecRepository) GetRankedSpecs(ctx context.Context, section, period string, limit int) ([]domain.RankingRow, error) {
	if limit <= 0 {
		limit = 8
	}
	log.Printf("[CatalogRepo.Home] querying ranked specs section=%s period=%s limit=%d", section, period, limit)

	var rows []struct {
		domain.Spec
		Rank         int             `db:"rank"`
		Score        float64         `db:"score"`
		PreviousRank sql.NullInt64   `db:"previous_rank"`
		MetricsJSON  json.RawMessage `db:"metrics"`
		CalculatedAt time.Time       `db:"calculated_at"`
	}

	query := `
		SELECT
			s.*, u.display_name as producer_name, u.handle as producer_handle,
			br.rank, br.score, br.previous_rank, br.metrics, br.calculated_at
		FROM beat_rankings br
		JOIN specs s ON s.id = br.spec_id
		JOIN users u ON s.producer_id = u.id
		WHERE br.section = $1
		  AND br.period = $2
		  AND ($1 <> 'top_charts' OR br.metrics->>'algorithm_version' = '3')
		  AND s.category = 'beat'
		  AND s.processing_status = 'completed'
		  AND s.is_deleted = FALSE
		ORDER BY br.rank ASC
		LIMIT $3`

	if err := r.db.SelectContext(ctx, &rows, query, section, period, limit); err != nil {
		log.Printf("[CatalogRepo.Home] ranked specs query failed section=%s period=%s err=%v", section, period, err)
		return nil, err
	}

	specs := make([]domain.Spec, len(rows))
	for i := range rows {
		specs[i] = rows[i].Spec
	}
	if err := r.hydrateSpecRelations(ctx, specs); err != nil {
		log.Printf("[CatalogRepo.Home] ranked specs relation hydration failed section=%s period=%s err=%v", section, period, err)
		return nil, err
	}

	result := make([]domain.RankingRow, len(rows))
	for i := range rows {
		var previousRank *int
		if rows[i].PreviousRank.Valid {
			v := int(rows[i].PreviousRank.Int64)
			previousRank = &v
		}
		result[i] = domain.RankingRow{
			Spec:         specs[i],
			Rank:         rows[i].Rank,
			Score:        rows[i].Score,
			PreviousRank: previousRank,
			MetricsJSON:  rows[i].MetricsJSON,
			CalculatedAt: rows[i].CalculatedAt,
		}
	}
	log.Printf("[CatalogRepo.Home] ranked specs loaded section=%s period=%s count=%d", section, period, len(result))
	return result, nil
}

func (r *PgSpecRepository) GetRankingFreshness(ctx context.Context, section, period string) (*domain.RankingFreshness, error) {
	var row struct {
		Count        int          `db:"count"`
		CalculatedAt sql.NullTime `db:"calculated_at"`
	}
	query := `
		SELECT COUNT(*) AS count, MAX(calculated_at) AS calculated_at
		FROM beat_rankings
		WHERE section = $1
		  AND period = $2
		  AND ($1 <> 'top_charts' OR metrics->>'algorithm_version' = '3')`
	if err := r.db.GetContext(ctx, &row, query, section, period); err != nil {
		log.Printf("[CatalogRepo.Home] ranking freshness query failed section=%s period=%s err=%v", section, period, err)
		return nil, err
	}
	freshness := &domain.RankingFreshness{Count: row.Count}
	if row.CalculatedAt.Valid {
		freshness.CalculatedAt = row.CalculatedAt.Time
	}
	log.Printf("[CatalogRepo.Home] ranking freshness section=%s period=%s count=%d calculated_at=%s", section, period, freshness.Count, freshness.CalculatedAt.Format(time.RFC3339))
	return freshness, nil
}

func (r *PgSpecRepository) RecalculateBeatRankings(ctx context.Context, section, period string) error {
	interval, err := rankingInterval(period)
	if err != nil {
		return err
	}
	switch section {
	case domain.HomeSectionTrending:
		log.Printf("[CatalogRepo.Home] recalculating trending rankings period=%s interval=%s", period, interval)
		return r.recalculateTrendingRankings(ctx, period, interval)
	case domain.HomeSectionTopCharts:
		log.Printf("[CatalogRepo.Home] recalculating top chart rankings period=%s interval=%s", period, interval)
		return r.recalculateTopChartRankings(ctx, period, interval)
	default:
		return fmt.Errorf("unsupported ranking section: %s", section)
	}
}

func (r *PgSpecRepository) recalculateTrendingRankings(ctx context.Context, period, interval string) error {
	lockKey := fmt.Sprintf("beat_rankings:%s:%s", domain.HomeSectionTrending, period)
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var locked bool
	if err = tx.GetContext(ctx, &locked, "SELECT pg_try_advisory_xact_lock(hashtext($1))", lockKey); err != nil || !locked {
		if err != nil {
			log.Printf("[CatalogRepo.Home] advisory lock failed key=%s err=%v", lockKey, err)
		} else {
			log.Printf("[CatalogRepo.Home] advisory lock busy key=%s", lockKey)
			err = errRankingRefreshSkipped
		}
		return err
	}

	query := `
		WITH current_metrics AS (
			SELECT
				s.id AS spec_id,
				COUNT(*) FILTER (WHERE ae.event_type = 'play') AS plays,
				COUNT(DISTINCT ae.user_id) FILTER (WHERE ae.user_id IS NOT NULL) AS unique_listeners,
				COUNT(*) FILTER (WHERE ae.event_type = 'favorite') AS favorites,
				COUNT(*) FILTER (WHERE ae.event_type = 'download') AS downloads
			FROM specs s
			LEFT JOIN analytics_events ae
				ON ae.spec_id = s.id
			   AND ae.created_at >= NOW() - $1::interval
			WHERE s.category = 'beat'
			  AND s.processing_status = 'completed'
			  AND s.is_deleted = FALSE
			GROUP BY s.id
		),
		previous_metrics AS (
			SELECT
				s.id AS spec_id,
				COUNT(*) FILTER (WHERE ae.event_type = 'play') AS plays,
				COUNT(DISTINCT ae.user_id) FILTER (WHERE ae.user_id IS NOT NULL) AS unique_listeners,
				COUNT(*) FILTER (WHERE ae.event_type = 'favorite') AS favorites,
				COUNT(*) FILTER (WHERE ae.event_type = 'download') AS downloads
			FROM specs s
			LEFT JOIN analytics_events ae
				ON ae.spec_id = s.id
			   AND ae.created_at < NOW() - $1::interval
			   AND ae.created_at >= NOW() - ($1::interval * 2)
			WHERE s.category = 'beat'
			  AND s.processing_status = 'completed'
			  AND s.is_deleted = FALSE
			GROUP BY s.id
		),
		current_orders AS (
			SELECT
				s.id AS spec_id,
				COUNT(o.id) AS purchases,
				COALESCE(SUM(o.amount), 0) / 100.0 AS revenue
			FROM specs s
			LEFT JOIN orders o
				ON o.spec_id = s.id
			   AND o.status = 'paid'
			   AND o.created_at >= NOW() - $1::interval
			WHERE s.category = 'beat'
			  AND s.processing_status = 'completed'
			  AND s.is_deleted = FALSE
			GROUP BY s.id
		),
		scored AS (
			SELECT
				s.id AS spec_id,
				cm.plays,
				cm.unique_listeners,
				cm.favorites,
				cm.downloads,
				co.purchases,
				co.revenue,
				(
					(cm.plays * 1.0)
					+ (cm.unique_listeners * 1.5)
					+ (cm.favorites * 4.0)
					+ (cm.downloads * 6.0)
					+ (co.purchases * 15.0)
					+ (co.revenue * 0.15)
					+ GREATEST(
						(
							(cm.plays * 1.0)
							+ (cm.unique_listeners * 1.5)
							+ (cm.favorites * 4.0)
							+ (cm.downloads * 6.0)
						) - (
							(pm.plays * 1.0)
							+ (pm.unique_listeners * 1.5)
							+ (pm.favorites * 4.0)
							+ (pm.downloads * 6.0)
						),
						0
					) * 0.35
					+ CASE
						WHEN s.created_at >= NOW() - INTERVAL '7 days'
						THEN 10 * GREATEST(0, 1 - (EXTRACT(EPOCH FROM (NOW() - s.created_at)) / 604800.0))
						ELSE 0
					  END
					- (GREATEST((EXTRACT(EPOCH FROM (NOW() - s.created_at)) / 86400.0) - 30, 0) * 0.05)
				) AS score,
				s.created_at
			FROM specs s
			JOIN current_metrics cm ON cm.spec_id = s.id
			JOIN previous_metrics pm ON pm.spec_id = s.id
			JOIN current_orders co ON co.spec_id = s.id
		),
		ranked AS (
			SELECT
				spec_id,
				ROW_NUMBER() OVER (
					ORDER BY score DESC, purchases DESC, plays DESC, created_at DESC, spec_id
				) AS rank,
				score,
				jsonb_build_object(
					'plays', plays,
					'unique_listeners', unique_listeners,
					'favorites', favorites,
					'downloads', downloads,
					'purchases', purchases,
					'revenue', revenue
				) AS metrics
			FROM scored
			WHERE score > 0
		),
		old AS (
			SELECT spec_id, rank
			FROM beat_rankings
			WHERE section = 'trending' AND period = $2
		),
		deleted AS (
			DELETE FROM beat_rankings
			WHERE section = 'trending' AND period = $2
		)
		INSERT INTO beat_rankings (section, period, spec_id, rank, score, previous_rank, metrics, calculated_at)
		SELECT 'trending', $2, ranked.spec_id, ranked.rank, ranked.score, old.rank, ranked.metrics, NOW()
		FROM ranked
		LEFT JOIN old ON old.spec_id = ranked.spec_id`

	_, err = tx.ExecContext(ctx, query, interval, period)
	if err != nil {
		log.Printf("[CatalogRepo.Home] trending ranking recalculation failed period=%s err=%v", period, err)
		return err
	}
	log.Printf("[CatalogRepo.Home] trending ranking recalculation complete period=%s", period)
	return tx.Commit()
}

func (r *PgSpecRepository) recalculateTopChartRankings(ctx context.Context, period, interval string) error {
	lockKey := fmt.Sprintf("beat_rankings:%s:%s", domain.HomeSectionTopCharts, period)
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var locked bool
	if err = tx.GetContext(ctx, &locked, "SELECT pg_try_advisory_xact_lock(hashtext($1))", lockKey); err != nil || !locked {
		if err != nil {
			log.Printf("[CatalogRepo.Home] advisory lock failed key=%s err=%v", lockKey, err)
		} else {
			log.Printf("[CatalogRepo.Home] advisory lock busy key=%s", lockKey)
			err = errRankingRefreshSkipped
		}
		return err
	}

	query := `
		WITH event_metrics AS (
			SELECT
				s.id AS spec_id,
				COUNT(*) FILTER (WHERE ae.event_type = 'play') AS plays,
				COUNT(*) FILTER (WHERE ae.event_type = 'favorite') AS favorites,
				COUNT(*) FILTER (WHERE ae.event_type = 'download') AS downloads
			FROM specs s
			LEFT JOIN analytics_events ae
				ON ae.spec_id = s.id
			   AND ae.created_at >= NOW() - $1::interval
			WHERE s.category = 'beat'
			  AND s.processing_status = 'completed'
			  AND s.is_deleted = FALSE
			GROUP BY s.id
		),
		order_metrics AS (
			SELECT
				s.id AS spec_id,
				COUNT(o.id) AS purchases,
				COALESCE(SUM(o.amount), 0) / 100.0 AS revenue
			FROM specs s
			LEFT JOIN orders o
				ON o.spec_id = s.id
			   AND o.status = 'paid'
			   AND o.created_at >= NOW() - $1::interval
			WHERE s.category = 'beat'
			  AND s.processing_status = 'completed'
			  AND s.is_deleted = FALSE
			GROUP BY s.id
		),
		lifetime_metrics AS (
			SELECT
				s.id AS spec_id,
				COALESCE(sa.play_count, 0) AS lifetime_plays,
				COALESCE(sa.favorite_count, 0) AS lifetime_favorites,
				COALESCE(sa.free_download_count, 0) AS lifetime_downloads,
				COALESCE(sa.total_purchase_count, 0) AS lifetime_purchases
			FROM specs s
			LEFT JOIN spec_analytics sa ON sa.spec_id = s.id
			WHERE s.category = 'beat'
			  AND s.processing_status = 'completed'
			  AND s.is_deleted = FALSE
		),
		scored AS (
			SELECT
				s.id AS spec_id,
				em.plays,
				em.favorites,
				em.downloads,
				om.purchases,
				om.revenue,
				(
					(
						(em.plays * 1.0)
						+ (em.favorites * 3.0)
						+ (em.downloads * 4.0)
						+ (om.purchases * 20.0)
						+ (om.revenue * 0.25)
					)
					+ (
						(
							(lm.lifetime_plays * 0.20)
							+ (lm.lifetime_favorites * 1.00)
							+ (lm.lifetime_downloads * 1.50)
							+ (lm.lifetime_purchases * 6.00)
						)
						*
						CASE
							WHEN (
								(em.plays * 1.0)
								+ (em.favorites * 3.0)
								+ (em.downloads * 4.0)
								+ (om.purchases * 20.0)
								+ (om.revenue * 0.25)
							) >= 5 THEN 0.22
							WHEN (
								(em.plays * 1.0)
								+ (em.favorites * 3.0)
								+ (em.downloads * 4.0)
								+ (om.purchases * 20.0)
								+ (om.revenue * 0.25)
							) > 0 THEN 0.12
							ELSE 0.03
						END
					)
				) AS score,
				(
					(em.plays * 1.0)
					+ (em.favorites * 3.0)
					+ (em.downloads * 4.0)
					+ (om.purchases * 20.0)
					+ (om.revenue * 0.25)
				) AS recent_score,
				(
					(lm.lifetime_plays * 0.20)
					+ (lm.lifetime_favorites * 1.00)
					+ (lm.lifetime_downloads * 1.50)
					+ (lm.lifetime_purchases * 6.00)
				) AS lifetime_authority_score,
				lm.lifetime_plays,
				lm.lifetime_favorites,
				lm.lifetime_downloads,
				lm.lifetime_purchases,
				s.created_at
			FROM specs s
			JOIN event_metrics em ON em.spec_id = s.id
			JOIN order_metrics om ON om.spec_id = s.id
			JOIN lifetime_metrics lm ON lm.spec_id = s.id
		),
		ranked AS (
			SELECT
				spec_id,
				ROW_NUMBER() OVER (
					ORDER BY score DESC, revenue DESC, purchases DESC, lifetime_plays DESC, plays DESC, created_at DESC, spec_id
				) AS rank,
				score,
				jsonb_build_object(
					'plays', GREATEST(plays, lifetime_plays),
					'favorites', GREATEST(favorites, lifetime_favorites),
					'downloads', GREATEST(downloads, lifetime_downloads),
					'purchases', GREATEST(purchases, lifetime_purchases),
					'revenue', revenue,
					'recent_score', recent_score,
					'lifetime_authority_score', lifetime_authority_score,
					'algorithm_version', 3
				) AS metrics
			FROM scored
			WHERE score > 0
		),
		old AS (
			SELECT spec_id, rank
			FROM beat_rankings
			WHERE section = 'top_charts' AND period = $2
		),
		deleted AS (
			DELETE FROM beat_rankings
			WHERE section = 'top_charts' AND period = $2
		)
		INSERT INTO beat_rankings (section, period, spec_id, rank, score, previous_rank, metrics, calculated_at)
		SELECT 'top_charts', $2, ranked.spec_id, ranked.rank, ranked.score, old.rank, ranked.metrics, NOW()
		FROM ranked
		LEFT JOIN old ON old.spec_id = ranked.spec_id`

	_, err = tx.ExecContext(ctx, query, interval, period)
	if err != nil {
		log.Printf("[CatalogRepo.Home] top chart ranking recalculation failed period=%s err=%v", period, err)
		return err
	}
	log.Printf("[CatalogRepo.Home] top chart ranking recalculation complete period=%s", period)
	return tx.Commit()
}

func (r *PgSpecRepository) hydrateSpecRelations(ctx context.Context, specs []domain.Spec) error {
	if len(specs) == 0 {
		return nil
	}

	specMap := make(map[uuid.UUID]*domain.Spec, len(specs))
	specIDs := make([]uuid.UUID, len(specs))
	for i := range specs {
		specs[i].Genres = []domain.Genre{}
		specs[i].Licenses = []domain.LicenseOption{}
		specMap[specs[i].ID] = &specs[i]
		specIDs[i] = specs[i].ID
	}

	genreQuery, args, err := sqlx.In(`
		SELECT sg.spec_id, g.*
		FROM genres g
		JOIN spec_genres sg ON g.id = sg.genre_id
		WHERE sg.spec_id IN (?)`, specIDs)
	if err != nil {
		return err
	}
	genreQuery = r.db.Rebind(genreQuery)

	var genreRows []struct {
		SpecID uuid.UUID `db:"spec_id"`
		domain.Genre
	}
	if err := r.db.SelectContext(ctx, &genreRows, genreQuery, args...); err != nil {
		return fmt.Errorf("failed to fetch genres: %w", err)
	}
	for _, row := range genreRows {
		if spec, ok := specMap[row.SpecID]; ok {
			spec.Genres = append(spec.Genres, row.Genre)
		}
	}

	licenseQuery, args, err := sqlx.In(`SELECT * FROM license_options WHERE spec_id IN (?) AND is_deleted = FALSE`, specIDs)
	if err != nil {
		return err
	}
	licenseQuery = r.db.Rebind(licenseQuery)

	var licenses []domain.LicenseOption
	if err := r.db.SelectContext(ctx, &licenses, licenseQuery, args...); err != nil {
		return fmt.Errorf("failed to fetch licenses: %w", err)
	}
	if err := r.hydrateLicenseOptionsPrices(ctx, licenses); err != nil {
		return err
	}
	for _, lic := range licenses {
		if spec, ok := specMap[lic.SpecID]; ok {
			spec.Licenses = append(spec.Licenses, lic)
		}
	}
	return nil
}

func rankingInterval(period string) (string, error) {
	switch period {
	case domain.HomePeriod24H:
		return "24 hours", nil
	case domain.HomePeriod7D:
		return "7 days", nil
	case domain.HomePeriod30D:
		return "30 days", nil
	default:
		return "", fmt.Errorf("unsupported ranking period: %s", period)
	}
}

func (r *PgSpecRepository) Delete(ctx context.Context, id uuid.UUID, producerId uuid.UUID) error {
	// 1. Check if ANY purchases exist (licenses table)
	// We check licenses table as it represents completed purchases granting access.
	// You could also check orders table if you want to be stricter (e.g. pending orders).
	var licenseCount int
	err := r.db.GetContext(ctx, &licenseCount, "SELECT COUNT(*) FROM licenses WHERE spec_id = $1", id)
	if err != nil {
		return fmt.Errorf("failed to check license existence: %w", err)
	}

	if licenseCount > 0 {
		// Soft Delete
		query := `UPDATE specs SET is_deleted = TRUE, deleted_at = NOW() WHERE id = $1 AND producer_id = $2`
		result, err := r.db.ExecContext(ctx, query, id, producerId)
		if err != nil {
			return err
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			return domain.ErrSpecNotFound
		}
		return domain.ErrSpecSoftDeleted
	}

	// Hard Delete (No purchases)
	query := `DELETE FROM specs WHERE id = $1 AND producer_id = $2`

	result, err := r.db.ExecContext(ctx, query, id, producerId)
	if err != nil {
		// Check for potential constraint violation just in case (e.g. from orders table)
		if strings.Contains(err.Error(), "violates foreign key constraint") {
			// Fallback to soft delete if constrained by something else (like an older order that didn't generate a license?)
			// But for now, let's treat it as an error or decide to soft delete.
			// Given the user request "maybe a user does want to delete", soft delete seems safer as fallback.
			// Let's retry with soft delete logic?
			// Simplified: Just return error for now to confirm behavior.
			return fmt.Errorf("cannot delete spec with existing dependencies: %w", err)
		}
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return domain.ErrSpecNotFound
	}

	return nil
}

func (r *PgSpecRepository) Update(ctx context.Context, spec *domain.Spec) error {
	spec.UpdatedAt = time.Now()

	// 1. Start Transaction
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 2. Update Spec Details
	query := `
		UPDATE specs 
		SET title = :title,
		    category = :category,
		    type = :type,
		    bpm = :bpm,
		    key = :key,
		    base_price = :base_price,
		    image_url = :image_url,
		    description = :description,
		    tags = :tags,
		    moods = :moods,
		    instruments = :instruments,
		    slug = :slug,
		    duration = :duration,
		    free_mp3_enabled = :free_mp3_enabled,
		    updated_at = :updated_at
		WHERE id = :id AND producer_id = :producer_id
	`

	result, err := tx.NamedExecContext(ctx, query, spec)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return domain.ErrSpecNotFound
	}

	// 3. Update Licenses (Smart Sync)
	if spec.Licenses != nil {
		// A. Fetch existing (active) licenses to compare
		var existingLicenses []domain.LicenseOption
		err = tx.SelectContext(ctx, &existingLicenses, "SELECT * FROM license_options WHERE spec_id = $1 AND is_deleted = FALSE", spec.ID)
		if err != nil {
			return err
		}

		existingMap := make(map[uuid.UUID]bool)
		existingByType := make(map[domain.LicenseType]domain.LicenseOption)
		for _, l := range existingLicenses {
			existingMap[l.ID] = true
			existingByType[l.LicenseType] = l
		}

		// Keep track of IDs processed in the update (to identify deletions)
		processedIDs := make(map[uuid.UUID]bool)

		// Define Queries
		insertQuery := `
            INSERT INTO license_options (
                id, spec_id, license_type, name, price, price_currency, features, file_types
            ) VALUES (
                :id, :spec_id, :license_type, :name, :price, :price_currency, :features, :file_types
            )`

		updateQuery := `
			UPDATE license_options SET
				license_type = :license_type,
				name = :name,
				price = :price,
				price_currency = :price_currency,
				features = :features,
				file_types = :file_types,
				is_deleted = FALSE,
				updated_at = NOW()
			WHERE id = :id AND spec_id = :spec_id
		`

		// B. Upsert (Insert or Update)
		for i := range spec.Licenses {
			license := &spec.Licenses[i]
			license.SpecID = spec.ID // Ensure SpecID is set

			// Try to match existing license by type if ID is missing (to prevent ID rotation)
			if license.ID == uuid.Nil {
				if existing, found := existingByType[license.LicenseType]; found {
					license.ID = existing.ID
				}
			}

			if license.ID == uuid.Nil {
				// New License (and no match found) -> INSERT
				id, err := uuid.NewV7()
				if err != nil {
					return err
				}
				license.ID = id
				_, err = tx.NamedExecContext(ctx, insertQuery, license)
				if err != nil {
					return err
				}
			} else {
				// Existing ID (or matched)
				if existingMap[license.ID] {
					// Update existing
					_, err = tx.NamedExecContext(ctx, updateQuery, license)
					if err != nil {
						return err
					}
					processedIDs[license.ID] = true
				} else {
					// ID provided but not in DB (or soft deleted)
					// Verify it exists in DB (maybe soft deleted) to decide whether to INSERT with new ID or UPDATE
					// For simplicity and since we matched by Type if possible:
					// If we are here, it means ID is not in active existingMap.
					// It could be a soft-deleted ID.
					// Attempt UPDATE (which reactivates due to is_deleted=FALSE).
					result, err := tx.NamedExecContext(ctx, updateQuery, license)
					if err != nil {
						return err
					}
					rows, _ := result.RowsAffected()
					if rows == 0 {
						// ID doesn't exist at all -> Insert
						_, err = tx.NamedExecContext(ctx, insertQuery, license)
						if err != nil {
							return err
						}
					}
					processedIDs[license.ID] = true
				}
			}
			if err := saveLicensePricesTx(ctx, tx, license.ID, license.Prices); err != nil {
				return err
			}
		}

		// C. Hande Deletions
		// Collect IDs to delete
		var idsToDelete []uuid.UUID
		for existingID := range existingMap {
			if !processedIDs[existingID] {
				idsToDelete = append(idsToDelete, existingID)
			}
		}

		// Sort IDs to ensure deterministic execution order (crucial for tests and consistency)
		sort.Slice(idsToDelete, func(i, j int) bool {
			return idsToDelete[i].String() < idsToDelete[j].String()
		})

		for _, existingID := range idsToDelete {
			// This license was NOT in the update payload -> DELETE it.

			// 1. Check if used in any purchases
			var usageCount int
			err := tx.GetContext(ctx, &usageCount, "SELECT COUNT(*) FROM licenses WHERE license_option_id = $1", existingID)
			if err != nil {
				return err
			}

			if usageCount > 0 {
				// Used -> Soft Delete
				_, err = tx.ExecContext(ctx, "UPDATE license_options SET is_deleted = TRUE, updated_at = NOW() WHERE id = $1", existingID)
				if err != nil {
					return err
				}
			} else {
				// Not Used -> Hard Delete
				_, err = tx.ExecContext(ctx, "DELETE FROM license_options WHERE id = $1", existingID)
				if err != nil {
					// In case of race condition or other constraint, fall back to soft delete isn't safe if tx aborted,
					// but usageCount check minimizes this risk significantly.
					return err
				}
			}
		}
	}

	return tx.Commit()
}

// ListByUserID retrieves all specs for a specific producer with pagination.
func (r *PgSpecRepository) ListByUserID(ctx context.Context, producerID uuid.UUID, limit, offset int) ([]domain.Spec, int, error) {
	var results []struct {
		domain.Spec
		TotalCount int `db:"total_count"`
	}

	query := `
		SELECT s.*, u.display_name as producer_name, u.handle as producer_handle, COUNT(*) OVER() as total_count
		FROM specs s
		JOIN users u ON s.producer_id = u.id
		WHERE s.producer_id = $1
		  AND s.is_deleted = FALSE
		  AND s.processing_status = 'completed'
		ORDER BY s.created_at DESC 
		LIMIT $2 OFFSET $3
	`

	err := r.db.SelectContext(ctx, &results, query, producerID, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	if len(results) == 0 {
		return []domain.Spec{}, 0, nil
	}

	total := results[0].TotalCount
	specs := make([]domain.Spec, len(results))

	specMap := make(map[uuid.UUID]*domain.Spec, len(results))
	specIDs := make([]uuid.UUID, len(results))

	for i, res := range results {
		specs[i] = res.Spec
		specs[i].Genres = []domain.Genre{}
		specs[i].Licenses = []domain.LicenseOption{}
		specMap[specs[i].ID] = &specs[i]
		specIDs[i] = specs[i].ID
	}

	// 1. Bulk Fetch Genres
	genreQuery, args, err := sqlx.In(`
		SELECT sg.spec_id, g.* 
		FROM genres g 
		JOIN spec_genres sg ON g.id = sg.genre_id 
		WHERE sg.spec_id IN (?)`, specIDs)
	if err != nil {
		return nil, 0, err
	}
	genreQuery = r.db.Rebind(genreQuery)

	var genreRows []struct {
		SpecID uuid.UUID `db:"spec_id"`
		domain.Genre
	}

	err = r.db.SelectContext(ctx, &genreRows, genreQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to fetch genres: %w", err)
	}

	for _, row := range genreRows {
		if spec, ok := specMap[row.SpecID]; ok {
			spec.Genres = append(spec.Genres, row.Genre)
		}
	}

	// 2. Bulk Fetch Licenses
	licenseQuery, args, err := sqlx.In(`SELECT * FROM license_options WHERE spec_id IN (?) AND is_deleted = FALSE`, specIDs)
	if err != nil {
		return nil, 0, err
	}
	licenseQuery = r.db.Rebind(licenseQuery)

	var licenses []domain.LicenseOption
	err = r.db.SelectContext(ctx, &licenses, licenseQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to fetch licenses: %w", err)
	}

	if err := r.hydrateLicenseOptionsPrices(ctx, licenses); err != nil {
		return nil, 0, err
	}
	for _, lic := range licenses {
		if spec, ok := specMap[lic.SpecID]; ok {
			spec.Licenses = append(spec.Licenses, lic)
		}
	}

	return specs, total, nil
}

// GetByIDSystem retrieves a spec by ID without filtering deleted ones.
func (r *PgSpecRepository) GetByIDSystem(ctx context.Context, id uuid.UUID) (*domain.Spec, error) {
	spec := &domain.Spec{}

	query := `
		SELECT s.*, u.display_name as producer_name, u.handle as producer_handle
		FROM specs s
		JOIN users u ON s.producer_id = u.id
		WHERE s.id = $1
	`
	err := r.db.GetContext(ctx, spec, query, id)
	if err != nil {
		return nil, err
	}

	// Fetch licenses
	licenseQuery := `SELECT * FROM license_options WHERE spec_id = $1 AND is_deleted = FALSE`
	err = r.db.SelectContext(ctx, &spec.Licenses, licenseQuery, id)
	if err != nil {
		return nil, err
	}
	if err := r.hydrateLicenseOptionsPrices(ctx, spec.Licenses); err != nil {
		return nil, err
	}

	// Fetch genres
	genreQuery := `SELECT g.* FROM genres g JOIN spec_genres sg ON g.id = sg.genre_id WHERE sg.spec_id = $1`

	err = r.db.SelectContext(ctx, &spec.Genres, genreQuery, id)
	if err != nil {
		return nil, err
	}
	return spec, nil
}

// FindByIDIncludingDeleted implements domain.SpecFinder interface
// Alias for GetByIDSystem - retrieves a spec even if it's soft-deleted
func (r *PgSpecRepository) FindByIDIncludingDeleted(ctx context.Context, id uuid.UUID) (*domain.Spec, error) {
	return r.GetByIDSystem(ctx, id)
}

func (r *PgSpecRepository) UpdateFilesAndStatus(ctx context.Context, id uuid.UUID, files map[string]*string, status domain.ProcessingStatus) error {
	// Build dynamic query
	query := "UPDATE specs SET processing_status = :status, updated_at = :updated_at"
	params := map[string]interface{}{
		"id":         id,
		"status":     status,
		"updated_at": time.Now(),
	}

	if val, ok := files["image_url"]; ok && val != nil {
		query += ", image_url = :image_url"
		params["image_url"] = *val
	}
	if val, ok := files["preview_url"]; ok && val != nil {
		query += ", preview_url = :preview_url"
		params["preview_url"] = *val
	}
	if val, ok := files["wav_url"]; ok && val != nil {
		query += ", wav_url = :wav_url"
		params["wav_url"] = *val
	}
	if val, ok := files["stems_url"]; ok && val != nil {
		query += ", stems_url = :stems_url"
		params["stems_url"] = *val
	}
	if val, ok := files["waveform_peaks"]; ok && val != nil {
		var peaks []int64
		if err := json.Unmarshal([]byte(*val), &peaks); err != nil {
			return fmt.Errorf("decode waveform peaks: %w", err)
		}
		query += ", waveform_peaks = :waveform_peaks"
		params["waveform_peaks"] = pq.Int64Array(peaks)
	}

	query += " WHERE id = :id"

	result, err := r.db.NamedExecContext(ctx, query, params)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return domain.ErrSpecNotFound
	}

	return nil
}
