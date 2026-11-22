package application

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/catalog/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockRepo struct {
	createFn         func(context.Context, *domain.Spec) error
	getByIDFn        func(context.Context, uuid.UUID) (*domain.Spec, error)
	listFn           func(context.Context, domain.SpecFilter) ([]domain.Spec, int, error)
	updateFn         func(context.Context, *domain.Spec) error
	deleteFn         func(context.Context, uuid.UUID, uuid.UUID) error
	listByUserIDFn   func(context.Context, uuid.UUID, int, int) ([]domain.Spec, int, error)
	getByShortCodeFn func(context.Context, string) (*domain.Spec, error)
	getBySlugFn      func(context.Context, string) (*domain.Spec, error)
	statsFn          func(context.Context) (*domain.HomepageStats, error)
	newestFn         func(context.Context, int) ([]domain.Spec, error)
	rankedFn         func(context.Context, string, string, int) ([]domain.RankingRow, error)
}

func (m mockRepo) Create(ctx context.Context, s *domain.Spec) error { return m.createFn(ctx, s) }
func (m mockRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Spec, error) {
	return m.getByIDFn(ctx, id)
}
func (m mockRepo) GetByIDSystem(context.Context, uuid.UUID) (*domain.Spec, error) { return nil, nil }
func (m mockRepo) List(ctx context.Context, f domain.SpecFilter) ([]domain.Spec, int, error) {
	return m.listFn(ctx, f)
}
func (m mockRepo) Update(ctx context.Context, s *domain.Spec) error { return m.updateFn(ctx, s) }
func (m mockRepo) Delete(ctx context.Context, id uuid.UUID, producerID uuid.UUID) error {
	return m.deleteFn(ctx, id, producerID)
}
func (m mockRepo) ListByUserID(ctx context.Context, producerID uuid.UUID, limit, offset int, filters ...domain.ProducerSpecFilter) ([]domain.Spec, int, error) {
	return m.listByUserIDFn(ctx, producerID, limit, offset)
}
func (m mockRepo) GetByShortCode(ctx context.Context, code string) (*domain.Spec, error) {
	if m.getByShortCodeFn != nil {
		return m.getByShortCodeFn(ctx, code)
	}
	return nil, nil
}
func (m mockRepo) GetBySlug(ctx context.Context, slug string) (*domain.Spec, error) {
	if m.getBySlugFn != nil {
		return m.getBySlugFn(ctx, slug)
	}
	return nil, nil
}
func (m mockRepo) UpdateFilesAndStatus(ctx context.Context, id uuid.UUID, files map[string]*string, status domain.ProcessingStatus) error {
	return nil
}
func (m mockRepo) GetHomepageStats(ctx context.Context) (*domain.HomepageStats, error) {
	if m.statsFn != nil {
		return m.statsFn(ctx)
	}
	return &domain.HomepageStats{}, nil
}
func (m mockRepo) GetNewestBeats(ctx context.Context, limit int) ([]domain.Spec, error) {
	if m.newestFn != nil {
		return m.newestFn(ctx, limit)
	}
	return []domain.Spec{}, nil
}
func (m mockRepo) GetRankedSpecs(ctx context.Context, section, period string, limit int) ([]domain.RankingRow, error) {
	if m.rankedFn != nil {
		return m.rankedFn(ctx, section, period, limit)
	}
	return []domain.RankingRow{}, nil
}
func (m mockRepo) GetRankingFreshness(context.Context, string, string) (*domain.RankingFreshness, error) {
	return &domain.RankingFreshness{}, nil
}
func (m mockRepo) RecalculateBeatRankings(context.Context, string, string) error {
	return nil
}

func TestSpecService_CreateSpecValidation(t *testing.T) {
	svc := NewSpecService(mockRepo{createFn: func(context.Context, *domain.Spec) error { return nil }})
	ctx := context.Background()

	err := svc.CreateSpec(ctx, &domain.Spec{})
	require.EqualError(t, err, "title is required")

	err = svc.CreateSpec(ctx, &domain.Spec{Title: "x", BasePrice: -1})
	require.EqualError(t, err, "price cannot be negative")

	err = svc.CreateSpec(ctx, &domain.Spec{Title: "x", BasePrice: 1, Category: domain.CategoryBeat, BPM: 20})
	require.EqualError(t, err, "BPM must be between 60 and 300")

	stems := "stems"
	beat := domain.Spec{
		Title: "x", BasePrice: 1, Category: domain.CategoryBeat, BPM: 120, Key: "C MAJOR",
		Genres:   []domain.Genre{{Name: "TRAP"}},
		Licenses: []domain.LicenseOption{{LicenseType: domain.LicenseBasic, Name: "Basic"}},
	}
	beat.StemsUrl = &stems
	err = svc.CreateSpec(ctx, &beat)
	require.EqualError(t, err, "WAV file is required!")

	wav := "wav"
	beat.StemsUrl = nil
	beat.WavUrl = &wav
	err = svc.CreateSpec(ctx, &beat)
	require.EqualError(t, err, "stems file is mandatory for beats")

	err = svc.CreateSpec(ctx, &domain.Spec{Title: "ok", BasePrice: 1, Category: domain.CategorySample})
	require.NoError(t, err)
}

func TestSpecService_GetHomeBuildsAllSections(t *testing.T) {
	repo := mockRepo{
		statsFn: func(context.Context) (*domain.HomepageStats, error) {
			return &domain.HomepageStats{TotalLiveBeats: 3}, nil
		},
		newestFn: func(context.Context, int) ([]domain.Spec, error) {
			return []domain.Spec{{ID: uuid.New(), Title: "Newest"}}, nil
		},
		rankedFn: func(_ context.Context, section, _ string, _ int) ([]domain.RankingRow, error) {
			if section == domain.HomeSectionTrending {
				previous := 2
				return []domain.RankingRow{{Spec: domain.Spec{ID: uuid.New(), Title: "Trending"}, Rank: 1, PreviousRank: &previous}}, nil
			}
			return []domain.RankingRow{}, nil
		},
	}
	home, err := NewSpecService(repo).GetHome(context.Background(), domain.HomepageParams{Limit: 99})
	require.NoError(t, err)
	assert.Equal(t, 20, home.CacheTTLSeconds/45)
	assert.Len(t, home.Sections, 4)
	assert.Equal(t, "algorithmic", home.Sections[domain.HomeSectionTrending].Source)
	assert.Equal(t, "algorithmic", home.Sections[domain.HomeSectionTopCharts].Source)
}

func TestCatalogHelpers(t *testing.T) {
	assert.Equal(t, 1, func() int { p, _ := normalizePageAndLimit(0, 0); return p }())
	_, limit := normalizePageAndLimit(2, 999)
	assert.Equal(t, 50, limit)
	assert.Equal(t, 8, normalizeHomeLimit(0))
	assert.Equal(t, 20, normalizeHomeLimit(99))
	assert.Equal(t, domain.HomePeriod30D, normalizeHomePeriod("wrong"))
	assert.Len(t, normalizeHomeSections([]string{"invalid", domain.HomeSectionTrending}), 1)
	assert.Equal(t, "down", movementForRanks(1, func() *int { n := 0; return &n }()))
	assert.Equal(t, "up", movementForRanks(1, func() *int { n := 2; return &n }()))
	assert.Equal(t, "down", movementForRanks(2, func() *int { n := 1; return &n }()))
	assert.Equal(t, "-", movementForRanks(1, func() *int { n := 1; return &n }()))
	assert.Equal(t, "my-beat-2026", slugify("My Beat! 2026"))
	assert.Len(t, randomCodeSegment(12), 12)
	assert.Equal(t, domain.BeatRankingMetrics{}, parseRankingMetrics([]byte("bad")))
}

func TestSpecService_CreateSpecRejectsInvalidCurrency(t *testing.T) {
	svc := NewSpecService(mockRepo{createFn: func(context.Context, *domain.Spec) error {
		return errors.New("create should not be called")
	}})
	ctx := context.Background()

	err := svc.CreateSpec(ctx, &domain.Spec{Title: "bad", BasePrice: 1, Category: domain.CategorySample, PriceCurrency: "eur"})
	require.ErrorIs(t, err, domain.ErrInvalidCurrency)

	err = svc.CreateSpec(ctx, &domain.Spec{
		Title:         "bad license",
		BasePrice:     1,
		Category:      domain.CategorySample,
		PriceCurrency: "USD",
		Licenses:      []domain.LicenseOption{{PriceCurrency: "gbp"}},
	})
	require.ErrorIs(t, err, domain.ErrInvalidCurrency)
}

func TestSpecService_CreateSpecNormalizesCurrencies(t *testing.T) {
	svc := NewSpecService(mockRepo{createFn: func(_ context.Context, spec *domain.Spec) error {
		assert.Equal(t, "USD", spec.PriceCurrency)
		require.Len(t, spec.Licenses, 2)
		assert.Equal(t, "USD", spec.Licenses[0].PriceCurrency)
		assert.Equal(t, "INR", spec.Licenses[1].PriceCurrency)
		return nil
	}})
	ctx := context.Background()

	err := svc.CreateSpec(ctx, &domain.Spec{
		Title:         "ok",
		BasePrice:     1,
		Category:      domain.CategorySample,
		PriceCurrency: " usd ",
		Licenses: []domain.LicenseOption{
			{},
			{PriceCurrency: " inr "},
		},
	})
	require.NoError(t, err)
}

func TestSpecService_DelegatesAndUpdate(t *testing.T) {
	owner := uuid.New()
	specID := uuid.New()
	repo := mockRepo{
		getByIDFn: func(context.Context, uuid.UUID) (*domain.Spec, error) {
			return &domain.Spec{ID: specID, ProducerID: owner, Title: "old", Category: domain.CategorySample}, nil
		},
		updateFn: func(_ context.Context, s *domain.Spec) error {
			if s.ProducerID != owner {
				return errors.New("bad owner")
			}
			return nil
		},
		listFn: func(context.Context, domain.SpecFilter) ([]domain.Spec, int, error) {
			return []domain.Spec{{ID: specID}}, 1, nil
		},
		deleteFn: func(context.Context, uuid.UUID, uuid.UUID) error { return nil },
		listByUserIDFn: func(_ context.Context, _ uuid.UUID, limit, offset int) ([]domain.Spec, int, error) {
			assert.Equal(t, 20, limit)
			assert.Equal(t, 0, offset)
			return []domain.Spec{{ID: specID}}, 1, nil
		},
	}
	svc := NewSpecService(repo)
	ctx := context.Background()

	_, _, err := svc.ListSpecs(ctx, domain.SpecFilter{})
	require.NoError(t, err)
	require.NoError(t, svc.DeleteSpec(ctx, specID, owner))
	_, _, err = svc.GetUserSpecs(ctx, owner, 1, -1)

	upd := &domain.Spec{ID: specID, Title: "new", BasePrice: 10, Category: domain.CategorySample}
	require.NoError(t, svc.UpdateSpec(ctx, upd, owner))

	err = svc.UpdateSpec(ctx, &domain.Spec{ID: specID, Title: "", BasePrice: 10}, owner)
	require.EqualError(t, err, "title is required")

	err = svc.UpdateSpec(ctx, &domain.Spec{ID: specID, Title: "a", BasePrice: -1}, owner)
	require.EqualError(t, err, "price cannot be negative")

	err = svc.UpdateSpec(ctx, &domain.Spec{ID: specID, Title: "a", BasePrice: 1, Category: domain.CategoryBeat, BPM: 400}, owner)
	require.EqualError(t, err, "BPM must be between 60 and 300")
}

func TestSpecService_UpdateSpecGuards(t *testing.T) {
	specID := uuid.New()
	other := uuid.New()
	repo := mockRepo{getByIDFn: func(context.Context, uuid.UUID) (*domain.Spec, error) {
		return &domain.Spec{ID: specID, ProducerID: uuid.New()}, nil
	}}
	svc := NewSpecService(repo)

	err := svc.UpdateSpec(context.Background(), &domain.Spec{ID: specID, Title: "x", BasePrice: 1}, other)
	require.EqualError(t, err, "unauthorized: you can only update your own specs")

	svc = NewSpecService(mockRepo{getByIDFn: func(context.Context, uuid.UUID) (*domain.Spec, error) { return nil, nil }})
	err = svc.UpdateSpec(context.Background(), &domain.Spec{ID: specID, Title: "x", BasePrice: 1}, other)
	require.EqualError(t, err, "spec not found")

	svc = NewSpecService(mockRepo{getByIDFn: func(context.Context, uuid.UUID) (*domain.Spec, error) { return nil, errors.New("db") }})
	err = svc.UpdateSpec(context.Background(), &domain.Spec{ID: specID, Title: "x", BasePrice: 1}, other)
	require.EqualError(t, err, "db")
}

func TestSpecService_UpdateSpecRejectsInvalidCurrency(t *testing.T) {
	specID := uuid.New()
	owner := uuid.New()
	repo := mockRepo{
		getByIDFn: func(context.Context, uuid.UUID) (*domain.Spec, error) {
			return &domain.Spec{ID: specID, ProducerID: owner}, nil
		},
		updateFn: func(context.Context, *domain.Spec) error {
			return errors.New("update should not be called")
		},
	}
	svc := NewSpecService(repo)

	err := svc.UpdateSpec(context.Background(), &domain.Spec{ID: specID, Title: "x", BasePrice: 1, Category: domain.CategorySample, PriceCurrency: "JPY"}, owner)
	require.ErrorIs(t, err, domain.ErrInvalidCurrency)
}

func TestSpecService_RejectsUpdateAndDeleteWhileProcessing(t *testing.T) {
	specID := uuid.New()
	owner := uuid.New()
	repo := mockRepo{
		getByIDFn: func(context.Context, uuid.UUID) (*domain.Spec, error) {
			return &domain.Spec{
				ID:               specID,
				ProducerID:       owner,
				ProcessingStatus: domain.ProcessingStatusProcessing,
			}, nil
		},
		updateFn: func(context.Context, *domain.Spec) error {
			return errors.New("update must not be called")
		},
		deleteFn: func(context.Context, uuid.UUID, uuid.UUID) error {
			return errors.New("delete must not be called")
		},
	}
	svc := NewSpecService(repo)

	err := svc.UpdateSpec(context.Background(), &domain.Spec{ID: specID}, owner)
	require.ErrorIs(t, err, domain.ErrSpecProcessing)
	err = svc.DeleteSpec(context.Background(), specID, owner)
	require.ErrorIs(t, err, domain.ErrSpecProcessing)
}

func TestLegacyGenreSlugPreservesRAndBCompatibility(t *testing.T) {
	require.Equal(t, "r&b", legacyGenreSlug("R&B"))
	require.Equal(t, "hip-hop", legacyGenreSlug("HIP-HOP"))
}
