package application

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	analyticsDomain "github.com/saransh1220/blueprint-audio/internal/modules/analytics/domain"
	catalogDomain "github.com/saransh1220/blueprint-audio/internal/modules/catalog/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockAnalyticsRepository struct{ mock.Mock }

func (m *mockAnalyticsRepository) GetSpecAnalytics(ctx context.Context, specID uuid.UUID) (*analyticsDomain.SpecAnalytics, error) {
	args := m.Called(ctx, specID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*analyticsDomain.SpecAnalytics), args.Error(1)
}
func (m *mockAnalyticsRepository) IncrementPlayCount(ctx context.Context, specID uuid.UUID) error {
	args := m.Called(ctx, specID)
	return args.Error(0)
}
func (m *mockAnalyticsRepository) IncrementFreeDownloadCount(ctx context.Context, specID uuid.UUID) error {
	args := m.Called(ctx, specID)
	return args.Error(0)
}
func (m *mockAnalyticsRepository) AddFavorite(ctx context.Context, userID, specID uuid.UUID) error {
	args := m.Called(ctx, userID, specID)
	return args.Error(0)
}
func (m *mockAnalyticsRepository) RemoveFavorite(ctx context.Context, userID, specID uuid.UUID) error {
	args := m.Called(ctx, userID, specID)
	return args.Error(0)
}
func (m *mockAnalyticsRepository) IsFavorited(ctx context.Context, userID, specID uuid.UUID) (bool, error) {
	args := m.Called(ctx, userID, specID)
	return args.Bool(0), args.Error(1)
}
func (m *mockAnalyticsRepository) ListUserFavorites(ctx context.Context, userID uuid.UUID, limit int, cursor *analyticsDomain.FavoriteCursor) (*analyticsDomain.FavoritePage, error) {
	args := m.Called(ctx, userID, limit, cursor)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*analyticsDomain.FavoritePage), args.Error(1)
}
func (m *mockAnalyticsRepository) GetLicensePurchaseCounts(ctx context.Context, specID uuid.UUID) (map[string]int, error) {
	args := m.Called(ctx, specID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]int), args.Error(1)
}
func (m *mockAnalyticsRepository) GetTotalPlays(ctx context.Context, producerID uuid.UUID, days int) (int, error) {
	args := m.Called(ctx, producerID, days)
	return args.Int(0), args.Error(1)
}
func (m *mockAnalyticsRepository) GetTotalFavorites(ctx context.Context, producerID uuid.UUID, days int) (int, error) {
	args := m.Called(ctx, producerID, days)
	return args.Int(0), args.Error(1)
}
func (m *mockAnalyticsRepository) GetTotalDownloads(ctx context.Context, producerID uuid.UUID, days int) (int, error) {
	args := m.Called(ctx, producerID, days)
	return args.Int(0), args.Error(1)
}
func (m *mockAnalyticsRepository) GetTotalRevenue(ctx context.Context, producerID uuid.UUID, days int) (float64, error) {
	args := m.Called(ctx, producerID, days)
	return args.Get(0).(float64), args.Error(1)
}
func (m *mockAnalyticsRepository) GetRevenueByLicenseGlobal(ctx context.Context, producerID uuid.UUID, days int) (map[string]float64, error) {
	args := m.Called(ctx, producerID, days)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]float64), args.Error(1)
}
func (m *mockAnalyticsRepository) GetPlaysByDay(ctx context.Context, producerID uuid.UUID, days int) ([]analyticsDomain.DailyStat, error) {
	args := m.Called(ctx, producerID, days)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]analyticsDomain.DailyStat), args.Error(1)
}
func (m *mockAnalyticsRepository) GetDownloadsByDay(ctx context.Context, producerID uuid.UUID, days int) ([]analyticsDomain.DailyStat, error) {
	args := m.Called(ctx, producerID, days)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]analyticsDomain.DailyStat), args.Error(1)
}
func (m *mockAnalyticsRepository) GetRevenueByDay(ctx context.Context, producerID uuid.UUID, days int) ([]analyticsDomain.DailyRevenueStat, error) {
	args := m.Called(ctx, producerID, days)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]analyticsDomain.DailyRevenueStat), args.Error(1)
}
func (m *mockAnalyticsRepository) GetTopSpecs(ctx context.Context, producerID uuid.UUID, limit int, sortBy string) ([]analyticsDomain.TopSpecStat, error) {
	args := m.Called(ctx, producerID, limit, sortBy)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]analyticsDomain.TopSpecStat), args.Error(1)
}

type mockSpecRepository struct{ mock.Mock }

func (m *mockSpecRepository) Create(ctx context.Context, spec *catalogDomain.Spec) error {
	args := m.Called(ctx, spec)
	return args.Error(0)
}
func (m *mockSpecRepository) GetByID(ctx context.Context, id uuid.UUID) (*catalogDomain.Spec, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*catalogDomain.Spec), args.Error(1)
}
func (m *mockSpecRepository) GetByIDSystem(ctx context.Context, id uuid.UUID) (*catalogDomain.Spec, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*catalogDomain.Spec), args.Error(1)
}
func (m *mockSpecRepository) List(ctx context.Context, filter catalogDomain.SpecFilter) ([]catalogDomain.Spec, int, error) {
	args := m.Called(ctx, filter)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]catalogDomain.Spec), args.Int(1), args.Error(2)
}
func (m *mockSpecRepository) Update(ctx context.Context, spec *catalogDomain.Spec) error {
	args := m.Called(ctx, spec)
	return args.Error(0)
}
func (m *mockSpecRepository) Delete(ctx context.Context, id uuid.UUID, producerID uuid.UUID) error {
	args := m.Called(ctx, id, producerID)
	return args.Error(0)
}
func (m *mockSpecRepository) ListByUserID(ctx context.Context, producerID uuid.UUID, limit, offset int, _ ...catalogDomain.ProducerSpecFilter) ([]catalogDomain.Spec, int, error) {
	args := m.Called(ctx, producerID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]catalogDomain.Spec), args.Int(1), args.Error(2)
}
func (m *mockSpecRepository) UpdateFilesAndStatus(ctx context.Context, id uuid.UUID, files map[string]*string, status catalogDomain.ProcessingStatus) error {
	args := m.Called(ctx, id, files, status)
	return args.Error(0)
}
func (m *mockSpecRepository) GetByShortCode(ctx context.Context, shortCode string) (*catalogDomain.Spec, error) {
	args := m.Called(ctx, shortCode)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*catalogDomain.Spec), args.Error(1)
}
func (m *mockSpecRepository) GetBySlug(ctx context.Context, slug string) (*catalogDomain.Spec, error) {
	args := m.Called(ctx, slug)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*catalogDomain.Spec), args.Error(1)
}
func (m *mockSpecRepository) GetHomepageStats(ctx context.Context) (*catalogDomain.HomepageStats, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*catalogDomain.HomepageStats), args.Error(1)
}
func (m *mockSpecRepository) GetNewestBeats(ctx context.Context, limit int) ([]catalogDomain.Spec, error) {
	args := m.Called(ctx, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]catalogDomain.Spec), args.Error(1)
}
func (m *mockSpecRepository) GetRankedSpecs(ctx context.Context, section, period string, limit int) ([]catalogDomain.RankingRow, error) {
	args := m.Called(ctx, section, period, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]catalogDomain.RankingRow), args.Error(1)
}
func (m *mockSpecRepository) GetRankingFreshness(ctx context.Context, section, period string) (*catalogDomain.RankingFreshness, error) {
	args := m.Called(ctx, section, period)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*catalogDomain.RankingFreshness), args.Error(1)
}
func (m *mockSpecRepository) RecalculateBeatRankings(ctx context.Context, section, period string) error {
	args := m.Called(ctx, section, period)
	return args.Error(0)
}

func TestAnalyticsService_ToggleFavorite(t *testing.T) {
	ctx := context.Background()
	ar := new(mockAnalyticsRepository)
	sr := new(mockSpecRepository)
	svc := NewAnalyticsService(ar, sr)
	userID := uuid.New()
	specID := uuid.New()

	ar.On("IsFavorited", ctx, userID, specID).Return(true, nil).Once()
	ar.On("RemoveFavorite", ctx, userID, specID).Return(nil).Once()
	val, err := svc.ToggleFavorite(ctx, userID, specID)
	assert.NoError(t, err)
	assert.False(t, val)

	ar.On("IsFavorited", ctx, userID, specID).Return(false, nil).Once()
	ar.On("AddFavorite", ctx, userID, specID).Return(nil).Once()
	val, err = svc.ToggleFavorite(ctx, userID, specID)
	assert.NoError(t, err)
	assert.True(t, val)

	ar.On("IsFavorited", ctx, userID, specID).Return(false, errors.New("db")).Once()
	_, err = svc.ToggleFavorite(ctx, userID, specID)
	assert.EqualError(t, err, "db")
}

func TestAnalyticsService_PublicAndProducer(t *testing.T) {
	ctx := context.Background()
	ar := new(mockAnalyticsRepository)
	sr := new(mockSpecRepository)
	svc := NewAnalyticsService(ar, sr)
	specID := uuid.New()
	userID := uuid.New()
	base := &analyticsDomain.SpecAnalytics{PlayCount: 10, FavoriteCount: 2, FreeDownloadCount: 3, TotalPurchaseCount: 4}

	ar.On("GetSpecAnalytics", ctx, specID).Return(base, nil).Once()
	ar.On("IsFavorited", ctx, userID, specID).Return(true, nil).Once()
	out, err := svc.GetPublicAnalytics(ctx, specID, &userID)
	assert.NoError(t, err)
	assert.Equal(t, 10, out.PlayCount)
	assert.True(t, out.IsFavorited)

	ar.On("GetSpecAnalytics", ctx, specID).Return(nil, errors.New("db")).Once()
	_, err = svc.GetPublicAnalytics(ctx, specID, nil)
	assert.EqualError(t, err, "db")

	sr.On("GetByID", ctx, specID).Return(&catalogDomain.Spec{ID: specID, ProducerID: userID}, nil).Once()
	ar.On("GetSpecAnalytics", ctx, specID).Return(base, nil).Once()
	ar.On("GetLicensePurchaseCounts", ctx, specID).Return(map[string]int{"Basic": 2}, nil).Once()
	pa, err := svc.GetProducerAnalytics(ctx, specID, userID)
	assert.NoError(t, err)
	assert.Equal(t, 10, pa.PlayCount)
	assert.Equal(t, 4, pa.TotalPurchaseCount)
}

func TestAnalyticsService_TrackOverviewAndTop(t *testing.T) {
	ctx := context.Background()
	ar := new(mockAnalyticsRepository)
	sr := new(mockSpecRepository)
	svc := NewAnalyticsService(ar, sr)
	userID := uuid.New()
	specID := uuid.New()

	ar.On("IncrementPlayCount", ctx, specID).Return(nil).Once()
	ar.On("IncrementFreeDownloadCount", ctx, specID).Return(nil).Once()
	ar.On("IsFavorited", ctx, userID, specID).Return(true, nil).Once()
	assert.NoError(t, svc.TrackPlay(ctx, specID))
	assert.NoError(t, svc.TrackFreeDownload(ctx, specID))
	fav, err := svc.IsFavorited(ctx, userID, specID)
	assert.NoError(t, err)
	assert.True(t, fav)

	ar.On("GetTotalPlays", ctx, userID, 30).Return(1, nil).Once()
	ar.On("GetTotalFavorites", ctx, userID, 30).Return(1, nil).Once()
	ar.On("GetTotalDownloads", ctx, userID, 30).Return(1, nil).Once()
	ar.On("GetTotalRevenue", ctx, userID, 30).Return(1.0, nil).Once()
	ar.On("GetPlaysByDay", ctx, userID, 30).Return([]analyticsDomain.DailyStat{}, nil).Once()
	ar.On("GetDownloadsByDay", ctx, userID, 30).Return([]analyticsDomain.DailyStat{}, nil).Once()
	ar.On("GetRevenueByDay", ctx, userID, 30).Return([]analyticsDomain.DailyRevenueStat{}, nil).Once()
	ar.On("GetTopSpecs", ctx, userID, 5, "").Return([]analyticsDomain.TopSpecStat{}, nil).Once()
	ar.On("GetRevenueByLicenseGlobal", ctx, userID, 30).Return(map[string]float64{}, nil).Once()
	overview, err := svc.GetStatsOverview(ctx, userID, 30, "")
	assert.NoError(t, err)
	assert.NotNil(t, overview)

	ar.On("GetTopSpecs", ctx, userID, 3, "revenue").Return([]analyticsDomain.TopSpecStat{{SpecID: specID, Title: "X"}}, nil).Once()
	top, err := svc.GetTopSpecs(ctx, userID, 3, "revenue")
	assert.NoError(t, err)
	assert.Len(t, top, 1)

	// days should clamp to 1 when input < 1
	ar.On("GetTotalPlays", ctx, userID, 1).Return(0, nil).Once()
	ar.On("GetTotalFavorites", ctx, userID, 1).Return(0, nil).Once()
	ar.On("GetTotalDownloads", ctx, userID, 1).Return(0, nil).Once()
	ar.On("GetTotalRevenue", ctx, userID, 1).Return(0.0, nil).Once()
	ar.On("GetPlaysByDay", ctx, userID, 1).Return([]analyticsDomain.DailyStat{}, nil).Once()
	ar.On("GetDownloadsByDay", ctx, userID, 1).Return([]analyticsDomain.DailyStat{}, nil).Once()
	ar.On("GetRevenueByDay", ctx, userID, 1).Return([]analyticsDomain.DailyRevenueStat{}, nil).Once()
	ar.On("GetTopSpecs", ctx, userID, 5, "").Return([]analyticsDomain.TopSpecStat{}, nil).Once()
	ar.On("GetRevenueByLicenseGlobal", ctx, userID, 1).Return(map[string]float64{}, nil).Once()
	_, err = svc.GetStatsOverview(ctx, userID, 0, "")
	assert.NoError(t, err)
}

func TestAnalyticsService_GetProducerAnalytics_ErrorBranches(t *testing.T) {
	ctx := context.Background()
	ar := new(mockAnalyticsRepository)
	sr := new(mockSpecRepository)
	svc := NewAnalyticsService(ar, sr)
	specID := uuid.New()
	producerID := uuid.New()

	sr.On("GetByID", ctx, specID).Return(nil, errors.New("db")).Once()
	_, err := svc.GetProducerAnalytics(ctx, specID, producerID)
	assert.EqualError(t, err, "db")

	sr.On("GetByID", ctx, specID).Return(nil, nil).Once()
	_, err = svc.GetProducerAnalytics(ctx, specID, producerID)
	assert.EqualError(t, err, "spec not found")

	sr.On("GetByID", ctx, specID).Return(&catalogDomain.Spec{ID: specID, ProducerID: uuid.New()}, nil).Once()
	_, err = svc.GetProducerAnalytics(ctx, specID, producerID)
	assert.EqualError(t, err, "unauthorized")
}

func TestAnalyticsService_GetStatsOverview_DaysCapAndError(t *testing.T) {
	ctx := context.Background()
	ar := new(mockAnalyticsRepository)
	sr := new(mockSpecRepository)
	svc := NewAnalyticsService(ar, sr)
	producerID := uuid.New()

	// days should clamp to 3650 when input > 3650
	ar.On("GetTotalPlays", ctx, producerID, 3650).Return(0, errors.New("plays fail")).Once()
	_, err := svc.GetStatsOverview(ctx, producerID, 5000, "")
	assert.EqualError(t, err, "plays fail")
}
