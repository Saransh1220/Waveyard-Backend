package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/saransh1220/blueprint-audio/internal/modules/catalog/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/catalog/infrastructure/persistence/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPGSpecRepository_CreateAndListEmpty(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewSpecRepository(db)
	ctx := context.Background()
	producerID := uuid.New()

	spec := &domain.Spec{
		ID:             uuid.New(),
		ProducerID:     producerID,
		Title:          "Track",
		Category:       domain.CategoryBeat,
		Type:           "WAV",
		BPM:            120,
		Key:            "C",
		BasePrice:      100,
		ImageUrl:       "img",
		PreviewUrl:     "preview",
		Tags:           pq.StringArray{"trap"},
		Duration:       120,
		FreeMp3Enabled: true,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO specs").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO spec_analytics").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, repo.Create(ctx, spec))

	mock.ExpectQuery(`SELECT s\.\*, u\.display_name as producer_name, u.handle as producer_handle, COUNT\(\*\) OVER\(\) as total_count FROM specs s JOIN users u ON s\.producer_id = u\.id WHERE s\.is_deleted = FALSE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "producer_id", "title", "category", "type", "bpm", "key", "base_price", "image_url", "preview_url", "duration", "free_mp3_enabled", "total_count"}))
	out, total, err := repo.List(ctx, domain.SpecFilter{Limit: 20, Offset: 0, MinPrice: -1})
	require.NoError(t, err)
	assert.Len(t, out, 0)
	assert.Equal(t, 0, total)
}

func TestPGSpecRepository_DeletePaths(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewSpecRepository(db)
	ctx := context.Background()
	id := uuid.New()
	producerID := uuid.New()

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM licenses WHERE spec_id = \\$1").
		WithArgs(id).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectExec("UPDATE specs SET is_deleted = TRUE, deleted_at = NOW\\(\\) WHERE id = \\$1 AND producer_id = \\$2").
		WithArgs(id, producerID).WillReturnResult(sqlmock.NewResult(0, 1))
	err := repo.Delete(ctx, id, producerID)
	assert.ErrorIs(t, err, domain.ErrSpecSoftDeleted)

	id2 := uuid.New()
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM licenses WHERE spec_id = \\$1").
		WithArgs(id2).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec("DELETE FROM specs WHERE id = \\$1 AND producer_id = \\$2").
		WithArgs(id2, producerID).WillReturnResult(sqlmock.NewResult(0, 0))
	err = repo.Delete(ctx, id2, producerID)
	assert.ErrorIs(t, err, domain.ErrSpecNotFound)
}

func TestPGSpecRepository_UpdateAndGetErrors(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewSpecRepository(db)
	ctx := context.Background()
	id := uuid.New()
	producerID := uuid.New()

	spec := &domain.Spec{
		ID:             id,
		ProducerID:     producerID,
		Title:          "Track",
		Category:       domain.CategorySample,
		Type:           "WAV",
		BPM:            120,
		Key:            "C",
		BasePrice:      100,
		ImageUrl:       "img",
		Description:    "desc",
		Tags:           pq.StringArray{"a"},
		Duration:       10,
		FreeMp3Enabled: false,
	}

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE specs").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, repo.Update(ctx, spec))

	mock.ExpectQuery(`SELECT s\.\*, u\.display_name as producer_name, u.handle as producer_handle FROM specs s JOIN users u ON s\.producer_id = u\.id WHERE s\.id = \$1 AND s\.is_deleted = FALSE`).
		WithArgs(id).WillReturnError(errors.New("db"))
	_, err := repo.GetByID(ctx, id)
	assert.EqualError(t, err, "db")

	mock.ExpectQuery(`SELECT s\.\*, u\.display_name as producer_name, u.handle as producer_handle FROM specs s JOIN users u ON s\.producer_id = u\.id WHERE s\.id = \$1`).
		WithArgs(id).WillReturnError(errors.New("db"))
	_, err = repo.GetByIDSystem(ctx, id)
	assert.EqualError(t, err, "db")
}

func TestPGSpecRepository_GetByIDAndListByUserID(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewSpecRepository(db)
	ctx := context.Background()
	id := uuid.New()
	userID := uuid.New()

	rows := sqlmock.NewRows([]string{
		"id", "producer_id", "title", "category", "type", "bpm", "key", "base_price",
		"image_url", "preview_url", "duration", "free_mp3_enabled", "is_deleted", "producer_name",
	}).
		AddRow(id, userID, "Track", "beat", "WAV", 120, "C", 100, "img", "prev", 120, true, false, "Producer Name")
	mock.ExpectQuery(`SELECT s\.\*, u\.display_name as producer_name, u.handle as producer_handle FROM specs s JOIN users u ON s\.producer_id = u\.id WHERE s\.id = \$1 AND s\.is_deleted = FALSE`).WithArgs(id).WillReturnRows(rows)
	mock.ExpectQuery("SELECT \\* FROM license_options WHERE spec_id = \\$1 AND is_deleted = FALSE").WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"id", "spec_id", "license_type", "name", "price", "features", "file_types", "is_deleted"}))
	mock.ExpectQuery("SELECT g\\.\\* FROM genres g JOIN spec_genres sg ON g.id = sg.genre_id WHERE sg.spec_id = \\$1").WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "created_at"}))
	_, err := repo.GetByID(ctx, id)
	require.NoError(t, err)

	mock.ExpectQuery(`SELECT s\.\*, u\.display_name as producer_name, u.handle as producer_handle, COUNT\(\*\) OVER\(\) as total_count FROM specs s JOIN users u ON s\.producer_id = u\.id WHERE s\.producer_id = \$1 AND s\.is_deleted = FALSE AND s\.processing_status = 'completed' ORDER BY s\.created_at DESC LIMIT \$2 OFFSET \$3`).
		WithArgs(userID, 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "producer_id", "title", "category", "type", "bpm", "key", "base_price",
			"image_url", "preview_url", "duration", "free_mp3_enabled", "is_deleted", "total_count", "producer_name",
		}))
	specs, total, err := repo.ListByUserID(ctx, userID, 20, 0)
	require.NoError(t, err)
	assert.Len(t, specs, 0)
	assert.Equal(t, 0, total)
}

func TestPGSpecRepository_ListWithRelationsAndUpdateNotFound(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewSpecRepository(db)
	ctx := context.Background()

	specID := uuid.New()
	genreID := uuid.New()
	licenseID := uuid.New()
	producerID := uuid.New()

	mainRows := sqlmock.NewRows([]string{
		"id", "producer_id", "title", "category", "type", "bpm", "key", "base_price", "image_url", "preview_url", "duration", "free_mp3_enabled", "total_count", "producer_name",
	}).AddRow(specID, producerID, "Track", "beat", "WAV", 120, "C", 10.0, "img", "prev", 120, true, 1, "Producer Name")

	mock.ExpectQuery(`SELECT s\.\*, u\.display_name as producer_name, u.handle as producer_handle, COUNT\(\*\) OVER\(\) as total_count FROM specs s JOIN users u ON s\.producer_id = u\.id WHERE s\.is_deleted = FALSE`).
		WillReturnRows(mainRows)
	mock.ExpectQuery("SELECT sg.spec_id, g.\\*").
		WillReturnRows(sqlmock.NewRows([]string{"spec_id", "id", "name", "slug", "created_at"}).AddRow(specID, genreID, "Hip Hop", "hip-hop", time.Now()))
	mock.ExpectQuery("SELECT \\* FROM license_options WHERE spec_id IN").
		WillReturnRows(sqlmock.NewRows([]string{"id", "spec_id", "license_type", "name", "price", "features", "file_types", "is_deleted"}).
			AddRow(licenseID, specID, "Basic", "Basic", 10.0, pq.StringArray{"f1"}, pq.StringArray{"mp3"}, false))

	expectEmptyLicensePrices(mock)
	out, total, err := repo.List(ctx, domain.SpecFilter{Limit: 20, Offset: 0, MinPrice: -1, Sort: "price_asc"})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, 1, total)
	assert.Len(t, out[0].Genres, 1)
	assert.Len(t, out[0].Licenses, 1)

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE specs").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	err = repo.Update(ctx, &domain.Spec{ID: specID, ProducerID: producerID})
	assert.ErrorIs(t, err, domain.ErrSpecNotFound)
}

func TestPGSpecRepository_UpdateLicenseSyncDeletionBranches(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewSpecRepository(db)
	ctx := context.Background()

	specID := uuid.New()
	producerID := uuid.New()
	// Use fixed UUIDs to ensure deterministic order (aaaaaaaa... comes before bbbbbbbb...)
	usedLicenseID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	unusedLicenseID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	spec := &domain.Spec{
		ID:         specID,
		ProducerID: producerID,
		Title:      "Track",
		Category:   domain.CategoryBeat,
		Type:       "WAV",
		BPM:        120,
		Key:        "C",
		BasePrice:  10,
		ImageUrl:   "img",
		Tags:       pq.StringArray{"a"},
		Duration:   120,
		Licenses:   []domain.LicenseOption{},
	}

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE specs").
		WillReturnResult(sqlmock.NewResult(0, 1))
	existingRows := sqlmock.NewRows([]string{
		"id", "spec_id", "license_type", "name", "price", "features", "file_types", "is_deleted",
	}).
		AddRow(usedLicenseID, specID, "Basic", "Basic", 10, pq.StringArray{"f1"}, pq.StringArray{"mp3"}, false).
		AddRow(unusedLicenseID, specID, "Premium", "Premium", 20, pq.StringArray{"f2"}, pq.StringArray{"wav"}, false)
	mock.ExpectQuery("SELECT \\* FROM license_options WHERE spec_id = \\$1 AND is_deleted = FALSE").
		WithArgs(specID).WillReturnRows(existingRows)

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM licenses WHERE license_option_id = \\$1").
		WithArgs(usedLicenseID).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectExec("UPDATE license_options SET is_deleted = TRUE, updated_at = NOW\\(\\) WHERE id = \\$1").
		WithArgs(usedLicenseID).WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM licenses WHERE license_option_id = \\$1").
		WithArgs(unusedLicenseID).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec("DELETE FROM license_options WHERE id = \\$1").
		WithArgs(unusedLicenseID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.Update(ctx, spec))
}

func TestPGSpecRepository_CreateWithGenresAndLicenses(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewSpecRepository(db)
	ctx := context.Background()

	specID := uuid.New()
	producerID := uuid.New()
	spec := &domain.Spec{
		ID:         specID,
		ProducerID: producerID,
		Title:      "Track",
		Category:   domain.CategoryBeat,
		Type:       "WAV",
		BPM:        120,
		Key:        "C",
		BasePrice:  10,
		ImageUrl:   "img",
		PreviewUrl: "prev",
		Genres: []domain.Genre{
			{Name: "Hip Hop", Slug: "hip-hop"},
		},
		Licenses: []domain.LicenseOption{
			{LicenseType: domain.LicenseBasic, Name: "Basic", Price: 10, Features: pq.StringArray{"f1"}, FileTypes: pq.StringArray{"mp3"}},
		},
	}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO specs").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO spec_analytics").WithArgs(specID).WillReturnResult(sqlmock.NewResult(0, 1))
	genreID := uuid.New()
	mock.ExpectQuery("INSERT INTO genres[\\s\\S]*ON CONFLICT \\(slug\\) DO UPDATE[\\s\\S]*RETURNING id").
		WithArgs(sqlmock.AnyArg(), "Hip Hop", "hip-hop", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(genreID))
	mock.ExpectExec("INSERT INTO spec_genres \\(spec_id, genre_id\\) VALUES \\(\\$1, \\$2\\)").WithArgs(specID, genreID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO license_options").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.Create(ctx, spec))
}

func TestPGSpecRepository_UpdateLicenseUpsertBranches(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewSpecRepository(db)
	ctx := context.Background()

	specID := uuid.New()
	producerID := uuid.New()
	existingID := uuid.New()
	spec := &domain.Spec{
		ID:         specID,
		ProducerID: producerID,
		Title:      "Track",
		Category:   domain.CategoryBeat,
		Type:       "WAV",
		BPM:        120,
		Key:        "C",
		BasePrice:  10,
		ImageUrl:   "img",
		Tags:       pq.StringArray{"a"},
		Duration:   120,
		Licenses: []domain.LicenseOption{
			{LicenseType: domain.LicenseBasic, Name: "Basic", Price: 12, Features: pq.StringArray{"f1"}, FileTypes: pq.StringArray{"mp3"}},
			{LicenseType: domain.LicensePremium, Name: "Premium", Price: 20, Features: pq.StringArray{"f2"}, FileTypes: pq.StringArray{"wav"}},
		},
	}

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE specs").WillReturnResult(sqlmock.NewResult(0, 1))
	rows := sqlmock.NewRows([]string{
		"id", "spec_id", "license_type", "name", "price", "features", "file_types", "is_deleted",
	}).AddRow(existingID, specID, "Basic", "Basic", 10, pq.StringArray{"f1"}, pq.StringArray{"mp3"}, false)
	mock.ExpectQuery("SELECT \\* FROM license_options WHERE spec_id = \\$1 AND is_deleted = FALSE").
		WithArgs(specID).WillReturnRows(rows)
	mock.ExpectExec("UPDATE license_options SET").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO license_options").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.Update(ctx, spec))
}

func TestPGSpecRepository_List_WithAllFiltersAndProducerName(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewSpecRepository(db)
	ctx := context.Background()

	specID := uuid.New()
	producerID := uuid.New()
	mainRows := sqlmock.NewRows([]string{
		"id", "producer_id", "title", "category", "type", "bpm", "key", "base_price",
		"image_url", "preview_url", "duration", "free_mp3_enabled", "total_count", "producer_name",
	}).AddRow(specID, producerID, "Track", "beat", "WAV", 120, "C", 10.0, "img", "prev", 120, true, 1, "Producer Alias")

	mock.ExpectQuery("SELECT s\\.\\*, u\\.display_name as producer_name, u.handle as producer_handle, COUNT\\(\\*\\) OVER\\(\\) as total_count").
		WithArgs("beat", sqlmock.AnyArg(), sqlmock.AnyArg(), "%track%", 100, 160, 5.0, 20.0, "C", 10, 0).
		WillReturnRows(mainRows)
	mock.ExpectQuery("SELECT sg\\.spec_id, g\\.\\*").
		WillReturnRows(sqlmock.NewRows([]string{"spec_id", "id", "name", "slug", "created_at"}))
	mock.ExpectQuery("SELECT \\* FROM license_options WHERE spec_id IN").
		WillReturnRows(sqlmock.NewRows([]string{"id", "spec_id", "license_type", "name", "price", "features", "file_types", "is_deleted"}))

	out, total, err := repo.List(ctx, domain.SpecFilter{
		Category: "beat",
		Genres:   []string{"hip-hop"},
		Tags:     []string{"trap"},
		Search:   "track",
		MinBPM:   100,
		MaxBPM:   160,
		MinPrice: 5,
		MaxPrice: 20,
		Key:      "C",
		Sort:     "bpm_desc",
		Limit:    10,
		Offset:   0,
	})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, 1, total)
	assert.Equal(t, "Producer Alias", out[0].ProducerName)
}

func TestPGSpecRepository_ListByUserID_WithRowsAndRelations(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewSpecRepository(db)
	ctx := context.Background()

	specID := uuid.New()
	producerID := uuid.New()
	licenseID := uuid.New()
	genreID := uuid.New()

	mainRows := sqlmock.NewRows([]string{
		"id", "producer_id", "title", "category", "type", "bpm", "key", "base_price",
		"image_url", "preview_url", "duration", "free_mp3_enabled", "is_deleted", "total_count", "producer_name",
	}).AddRow(specID, producerID, "Track", "beat", "WAV", 120, "C", 10.0, "img", "prev", 120, true, false, 1, "Producer Alias")

	mock.ExpectQuery(`SELECT s\.\*, u\.display_name as producer_name, u.handle as producer_handle, COUNT\(\*\) OVER\(\) as total_count`).
		WithArgs(producerID, 10, 0).
		WillReturnRows(mainRows)
	mock.ExpectQuery("SELECT sg\\.spec_id, g\\.\\*").
		WillReturnRows(sqlmock.NewRows([]string{"spec_id", "id", "name", "slug", "created_at"}).
			AddRow(specID, genreID, "Hip Hop", "hip-hop", time.Now()))
	mock.ExpectQuery("SELECT \\* FROM license_options WHERE spec_id IN").
		WillReturnRows(sqlmock.NewRows([]string{"id", "spec_id", "license_type", "name", "price", "features", "file_types", "is_deleted"}).
			AddRow(licenseID, specID, "Basic", "Basic", 10.0, pq.StringArray{"f1"}, pq.StringArray{"mp3"}, false))

	expectEmptyLicensePrices(mock)
	out, total, err := repo.ListByUserID(ctx, producerID, 10, 0)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, 1, total)
	assert.Equal(t, "Producer Alias", out[0].ProducerName)
	assert.Len(t, out[0].Genres, 1)
	assert.Len(t, out[0].Licenses, 1)
}

func TestPGSpecRepository_ListByUserID_ErrorBranches(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewSpecRepository(db)
	ctx := context.Background()
	producerID := uuid.New()
	specID := uuid.New()

	t.Run("main query error", func(t *testing.T) {
		mock.ExpectQuery(`SELECT s\.\*, u\.display_name as producer_name, u.handle as producer_handle, COUNT\(\*\) OVER\(\) as total_count`).
			WithArgs(producerID, 10, 0).
			WillReturnError(errors.New("select failed"))

		_, _, err := repo.ListByUserID(ctx, producerID, 10, 0)
		assert.EqualError(t, err, "select failed")
	})

	t.Run("genre fetch error", func(t *testing.T) {
		mainRows := sqlmock.NewRows([]string{
			"id", "producer_id", "title", "category", "type", "bpm", "key", "base_price",
			"image_url", "preview_url", "duration", "free_mp3_enabled", "is_deleted", "total_count", "producer_name",
		}).AddRow(specID, producerID, "Track", "beat", "WAV", 120, "C", 10.0, "img", "prev", 120, true, false, 1, "Producer Alias")

		mock.ExpectQuery(`SELECT s\.\*, u\.display_name as producer_name, u.handle as producer_handle, COUNT\(\*\) OVER\(\) as total_count`).
			WithArgs(producerID, 10, 0).
			WillReturnRows(mainRows)
		mock.ExpectQuery("SELECT sg\\.spec_id, g\\.\\*").
			WillReturnError(errors.New("genres failed"))

		_, _, err := repo.ListByUserID(ctx, producerID, 10, 0)
		assert.EqualError(t, err, "failed to fetch genres: genres failed")
	})

	t.Run("license fetch error", func(t *testing.T) {
		mainRows := sqlmock.NewRows([]string{
			"id", "producer_id", "title", "category", "type", "bpm", "key", "base_price",
			"image_url", "preview_url", "duration", "free_mp3_enabled", "is_deleted", "total_count", "producer_name",
		}).AddRow(specID, producerID, "Track", "beat", "WAV", 120, "C", 10.0, "img", "prev", 120, true, false, 1, "Producer Alias")

		mock.ExpectQuery(`SELECT s\.\*, u\.display_name as producer_name, u.handle as producer_handle, COUNT\(\*\) OVER\(\) as total_count`).
			WithArgs(producerID, 10, 0).
			WillReturnRows(mainRows)
		mock.ExpectQuery("SELECT sg\\.spec_id, g\\.\\*").
			WillReturnRows(sqlmock.NewRows([]string{"spec_id", "id", "name", "slug", "created_at"}))
		mock.ExpectQuery("SELECT \\* FROM license_options WHERE spec_id IN").
			WillReturnError(errors.New("licenses failed"))

		_, _, err := repo.ListByUserID(ctx, producerID, 10, 0)
		assert.EqualError(t, err, "failed to fetch licenses: licenses failed")
	})
}

func TestPGSpecRepository_GetByIDSystem_SuccessAndErrors(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewSpecRepository(db)
	ctx := context.Background()
	id := uuid.New()
	userID := uuid.New()

	t.Run("success", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{
			"id", "producer_id", "title", "category", "type", "bpm", "key", "base_price",
			"image_url", "preview_url", "duration", "free_mp3_enabled", "is_deleted", "producer_name",
		}).AddRow(id, userID, "Track", "beat", "WAV", 120, "C", 100, "img", "prev", 120, true, false, "Producer Name")

		mock.ExpectQuery(`SELECT s\.\*, u\.display_name as producer_name, u.handle as producer_handle FROM specs s JOIN users u ON s\.producer_id = u\.id WHERE s\.id = \$1`).
			WithArgs(id).WillReturnRows(rows)
		mock.ExpectQuery("SELECT \\* FROM license_options WHERE spec_id = \\$1 AND is_deleted = FALSE").
			WithArgs(id).
			WillReturnRows(sqlmock.NewRows([]string{"id", "spec_id", "license_type", "name", "price", "features", "file_types", "is_deleted"}))
		mock.ExpectQuery("SELECT g\\.\\* FROM genres g JOIN spec_genres sg ON g.id = sg.genre_id WHERE sg.spec_id = \\$1").
			WithArgs(id).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "created_at"}))

		spec, err := repo.GetByIDSystem(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, "Producer Name", spec.ProducerName)
	})

	t.Run("license query error", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{
			"id", "producer_id", "title", "category", "type", "bpm", "key", "base_price",
			"image_url", "preview_url", "duration", "free_mp3_enabled", "is_deleted", "producer_name",
		}).AddRow(id, userID, "Track", "beat", "WAV", 120, "C", 100, "img", "prev", 120, true, false, "Producer Name")

		mock.ExpectQuery(`SELECT s\.\*, u\.display_name as producer_name, u.handle as producer_handle FROM specs s JOIN users u ON s\.producer_id = u\.id WHERE s\.id = \$1`).
			WithArgs(id).WillReturnRows(rows)
		mock.ExpectQuery("SELECT \\* FROM license_options WHERE spec_id = \\$1 AND is_deleted = FALSE").
			WithArgs(id).
			WillReturnError(errors.New("license query failed"))

		_, err := repo.GetByIDSystem(ctx, id)
		assert.EqualError(t, err, "license query failed")
	})

	t.Run("genre query error", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{
			"id", "producer_id", "title", "category", "type", "bpm", "key", "base_price",
			"image_url", "preview_url", "duration", "free_mp3_enabled", "is_deleted", "producer_name",
		}).AddRow(id, userID, "Track", "beat", "WAV", 120, "C", 100, "img", "prev", 120, true, false, "Producer Name")

		mock.ExpectQuery(`SELECT s\.\*, u\.display_name as producer_name, u.handle as producer_handle FROM specs s JOIN users u ON s\.producer_id = u\.id WHERE s\.id = \$1`).
			WithArgs(id).WillReturnRows(rows)
		mock.ExpectQuery("SELECT \\* FROM license_options WHERE spec_id = \\$1 AND is_deleted = FALSE").
			WithArgs(id).
			WillReturnRows(sqlmock.NewRows([]string{"id", "spec_id", "license_type", "name", "price", "features", "file_types", "is_deleted"}))
		mock.ExpectQuery("SELECT g\\.\\* FROM genres g JOIN spec_genres sg ON g.id = sg.genre_id WHERE sg.spec_id = \\$1").
			WithArgs(id).
			WillReturnError(errors.New("genre query failed"))

		_, err := repo.GetByIDSystem(ctx, id)
		assert.EqualError(t, err, "genre query failed")
	})
}

func TestPGSpecRepository_Delete_ErrorBranches(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewSpecRepository(db)
	ctx := context.Background()
	id := uuid.New()
	producerID := uuid.New()

	t.Run("count query error", func(t *testing.T) {
		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM licenses WHERE spec_id = \\$1").
			WithArgs(id).WillReturnError(errors.New("count failed"))

		err := repo.Delete(ctx, id, producerID)
		assert.EqualError(t, err, "failed to check license existence: count failed")
	})

	t.Run("hard delete constraint error", func(t *testing.T) {
		id2 := uuid.New()
		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM licenses WHERE spec_id = \\$1").
			WithArgs(id2).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectExec("DELETE FROM specs WHERE id = \\$1 AND producer_id = \\$2").
			WithArgs(id2, producerID).
			WillReturnError(errors.New("violates foreign key constraint"))

		err := repo.Delete(ctx, id2, producerID)
		assert.EqualError(t, err, "cannot delete spec with existing dependencies: violates foreign key constraint")
	})

	t.Run("hard delete success", func(t *testing.T) {
		id3 := uuid.New()
		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM licenses WHERE spec_id = \\$1").
			WithArgs(id3).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectExec("DELETE FROM specs WHERE id = \\$1 AND producer_id = \\$2").
			WithArgs(id3, producerID).
			WillReturnResult(sqlmock.NewResult(0, 1))
		err := repo.Delete(ctx, id3, producerID)
		assert.NoError(t, err)
	})
}

func TestPGSpecRepository_GetByID_QueryFailures(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewSpecRepository(db)
	ctx := context.Background()
	id := uuid.New()
	userID := uuid.New()

	rows := sqlmock.NewRows([]string{
		"id", "producer_id", "title", "category", "type", "bpm", "key", "base_price",
		"image_url", "preview_url", "duration", "free_mp3_enabled", "is_deleted", "producer_name",
	}).AddRow(id, userID, "Track", "beat", "WAV", 120, "C", 100, "img", "prev", 120, true, false, "Producer Name")

	mock.ExpectQuery(`SELECT s\.\*, u\.display_name as producer_name, u.handle as producer_handle FROM specs s JOIN users u ON s\.producer_id = u\.id WHERE s\.id = \$1 AND s\.is_deleted = FALSE`).
		WithArgs(id).WillReturnRows(rows)
	mock.ExpectQuery("SELECT \\* FROM license_options WHERE spec_id = \\$1 AND is_deleted = FALSE").
		WithArgs(id).
		WillReturnError(errors.New("license failed"))
	_, err := repo.GetByID(ctx, id)
	assert.EqualError(t, err, "license failed")

	rows2 := sqlmock.NewRows([]string{
		"id", "producer_id", "title", "category", "type", "bpm", "key", "base_price",
		"image_url", "preview_url", "duration", "free_mp3_enabled", "is_deleted", "producer_name",
	}).AddRow(id, userID, "Track", "beat", "WAV", 120, "C", 100, "img", "prev", 120, true, false, "Producer Name")
	mock.ExpectQuery(`SELECT s\.\*, u\.display_name as producer_name, u.handle as producer_handle FROM specs s JOIN users u ON s\.producer_id = u\.id WHERE s\.id = \$1 AND s\.is_deleted = FALSE`).
		WithArgs(id).WillReturnRows(rows2)
	mock.ExpectQuery("SELECT \\* FROM license_options WHERE spec_id = \\$1 AND is_deleted = FALSE").
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"id", "spec_id", "license_type", "name", "price", "features", "file_types", "is_deleted"}))
	mock.ExpectQuery("SELECT g\\.\\* FROM genres g JOIN spec_genres sg ON g.id = sg.genre_id WHERE sg.spec_id = \\$1").
		WithArgs(id).
		WillReturnError(errors.New("genres failed"))
	_, err = repo.GetByID(ctx, id)
	assert.EqualError(t, err, "genres failed")
}

func TestPGSpecRepository_List_QueryFailures(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewSpecRepository(db)
	ctx := context.Background()
	specID := uuid.New()
	producerID := uuid.New()

	mock.ExpectQuery("SELECT s\\.\\*, u\\.display_name as producer_name, u.handle as producer_handle, COUNT\\(\\*\\) OVER\\(\\) as total_count").
		WillReturnError(errors.New("list failed"))
	_, _, err := repo.List(ctx, domain.SpecFilter{Limit: 10, Offset: 0, MinPrice: -1})
	assert.EqualError(t, err, "list failed")

	mainRows := sqlmock.NewRows([]string{
		"id", "producer_id", "title", "category", "type", "bpm", "key", "base_price",
		"image_url", "preview_url", "duration", "free_mp3_enabled", "total_count", "producer_name",
	}).AddRow(specID, producerID, "Track", "beat", "WAV", 120, "C", 10.0, "img", "prev", 120, true, 1, "Producer Alias")
	mock.ExpectQuery("SELECT s\\.\\*, u\\.display_name as producer_name, u.handle as producer_handle, COUNT\\(\\*\\) OVER\\(\\) as total_count").
		WillReturnRows(mainRows)
	mock.ExpectQuery("SELECT sg\\.spec_id, g\\.\\*").
		WillReturnError(errors.New("genres failed"))
	_, _, err = repo.List(ctx, domain.SpecFilter{Limit: 10, Offset: 0, MinPrice: -1})
	assert.EqualError(t, err, "failed to fetch genres: genres failed")

	mainRows = sqlmock.NewRows([]string{
		"id", "producer_id", "title", "category", "type", "bpm", "key", "base_price",
		"image_url", "preview_url", "duration", "free_mp3_enabled", "total_count", "producer_name",
	}).AddRow(specID, producerID, "Track", "beat", "WAV", 120, "C", 10.0, "img", "prev", 120, true, 1, "Producer Alias")
	mock.ExpectQuery("SELECT s\\.\\*, u\\.display_name as producer_name, u.handle as producer_handle, COUNT\\(\\*\\) OVER\\(\\) as total_count").
		WillReturnRows(mainRows)
	mock.ExpectQuery("SELECT sg\\.spec_id, g\\.\\*").
		WillReturnRows(sqlmock.NewRows([]string{"spec_id", "id", "name", "slug", "created_at"}))
	mock.ExpectQuery("SELECT \\* FROM license_options WHERE spec_id IN").
		WillReturnError(errors.New("licenses failed"))
	_, _, err = repo.List(ctx, domain.SpecFilter{Limit: 10, Offset: 0, MinPrice: -1})
	assert.EqualError(t, err, "failed to fetch licenses: licenses failed")
}

func TestPGSpecRepository_List_SortModes(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewSpecRepository(db)
	ctx := context.Background()

	sorts := []string{"newest", "oldest", "price_asc", "price_desc", "bpm_asc", "bpm_desc"}
	for _, sortMode := range sorts {
		mock.ExpectQuery("SELECT s\\.\\*, u\\.display_name as producer_name, u.handle as producer_handle, COUNT\\(\\*\\) OVER\\(\\) as total_count").
			WillReturnRows(sqlmock.NewRows([]string{"id", "producer_id", "title", "category", "type", "bpm", "key", "base_price", "image_url", "preview_url", "duration", "free_mp3_enabled", "total_count", "producer_name"}))
		_, _, err := repo.List(ctx, domain.SpecFilter{Sort: sortMode, Limit: 10, Offset: 0, MinPrice: -1})
		require.NoError(t, err)
	}
}

func TestPGSpecRepository_Create_ErrorAndAlternateBranches(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewSpecRepository(db)
	ctx := context.Background()
	specID := uuid.New()
	producerID := uuid.New()

	t.Run("begin tx error", func(t *testing.T) {
		mock.ExpectBegin().WillReturnError(errors.New("begin failed"))
		err := repo.Create(ctx, &domain.Spec{ID: specID, ProducerID: producerID, Title: "T"})
		assert.EqualError(t, err, "begin failed")
	})

	t.Run("insert spec error", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO specs").WillReturnError(errors.New("insert failed"))
		mock.ExpectRollback()
		err := repo.Create(ctx, &domain.Spec{ID: specID, ProducerID: producerID, Title: "T"})
		assert.EqualError(t, err, "insert failed")
	})

	t.Run("analytics insert error", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO specs").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO spec_analytics").WillReturnError(errors.New("analytics failed"))
		mock.ExpectRollback()
		err := repo.Create(ctx, &domain.Spec{ID: specID, ProducerID: producerID, Title: "T"})
		assert.EqualError(t, err, "analytics failed")
	})

	t.Run("genre id provided path", func(t *testing.T) {
		genreID := uuid.New()
		spec := &domain.Spec{
			ID:         uuid.New(),
			ProducerID: producerID,
			Title:      "Track",
			Genres: []domain.Genre{
				{ID: genreID, Name: "Hip Hop", Slug: "hip-hop"},
			},
		}
		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO specs").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO spec_analytics").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO spec_genres \\(spec_id, genre_id\\) VALUES \\(\\$1, \\$2\\)").
			WithArgs(spec.ID, genreID).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
		require.NoError(t, repo.Create(ctx, spec))
	})
}

func TestPGSpecRepository_Update_ErrorBranches(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewSpecRepository(db)
	ctx := context.Background()
	specID := uuid.New()
	producerID := uuid.New()

	baseSpec := &domain.Spec{
		ID:         specID,
		ProducerID: producerID,
		Title:      "Track",
		Category:   domain.CategoryBeat,
		Type:       "WAV",
		BPM:        120,
		Key:        "C",
		BasePrice:  10,
		ImageUrl:   "img",
		Tags:       pq.StringArray{"a"},
		Duration:   120,
	}

	t.Run("begin tx error", func(t *testing.T) {
		mock.ExpectBegin().WillReturnError(errors.New("begin failed"))
		err := repo.Update(ctx, baseSpec)
		assert.EqualError(t, err, "begin failed")
	})

	t.Run("update main query error", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE specs").WillReturnError(errors.New("update failed"))
		mock.ExpectRollback()
		err := repo.Update(ctx, baseSpec)
		assert.EqualError(t, err, "update failed")
	})

	t.Run("existing licenses query error", func(t *testing.T) {
		spec := *baseSpec
		spec.Licenses = []domain.LicenseOption{{LicenseType: domain.LicenseBasic, Name: "Basic", Price: 10}}
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE specs").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery("SELECT \\* FROM license_options WHERE spec_id = \\$1 AND is_deleted = FALSE").
			WithArgs(specID).WillReturnError(errors.New("select failed"))
		mock.ExpectRollback()
		err := repo.Update(ctx, &spec)
		assert.EqualError(t, err, "select failed")
	})

	t.Run("usage count query error during deletions", func(t *testing.T) {
		spec := *baseSpec
		spec.Licenses = []domain.LicenseOption{}
		existingID := uuid.New()
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE specs").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery("SELECT \\* FROM license_options WHERE spec_id = \\$1 AND is_deleted = FALSE").
			WithArgs(specID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "spec_id", "license_type", "name", "price", "features", "file_types", "is_deleted"}).
				AddRow(existingID, specID, "Basic", "Basic", 10, pq.StringArray{"f1"}, pq.StringArray{"mp3"}, false))
		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM licenses WHERE license_option_id = \\$1").
			WithArgs(existingID).WillReturnError(errors.New("usage failed"))
		mock.ExpectRollback()
		err := repo.Update(ctx, &spec)
		assert.EqualError(t, err, "usage failed")
	})

	t.Run("unknown license id update then insert", func(t *testing.T) {
		spec := *baseSpec
		unknownID := uuid.New()
		spec.Licenses = []domain.LicenseOption{
			{
				ID:          unknownID,
				LicenseType: domain.LicensePremium,
				Name:        "Premium",
				Price:       20,
				Features:    pq.StringArray{"f1"},
				FileTypes:   pq.StringArray{"wav"},
			},
		}
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE specs").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery("SELECT \\* FROM license_options WHERE spec_id = \\$1 AND is_deleted = FALSE").
			WithArgs(specID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "spec_id", "license_type", "name", "price", "features", "file_types", "is_deleted"}))
		mock.ExpectExec("UPDATE license_options SET").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("INSERT INTO license_options").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
		require.NoError(t, repo.Update(ctx, &spec))
	})

	t.Run("soft delete exec error", func(t *testing.T) {
		spec := *baseSpec
		spec.Licenses = []domain.LicenseOption{}
		existingID := uuid.New()
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE specs").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery("SELECT \\* FROM license_options WHERE spec_id = \\$1 AND is_deleted = FALSE").
			WithArgs(specID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "spec_id", "license_type", "name", "price", "features", "file_types", "is_deleted"}).
				AddRow(existingID, specID, "Basic", "Basic", 10, pq.StringArray{"f1"}, pq.StringArray{"mp3"}, false))
		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM licenses WHERE license_option_id = \\$1").
			WithArgs(existingID).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		mock.ExpectExec("UPDATE license_options SET is_deleted = TRUE, updated_at = NOW\\(\\) WHERE id = \\$1").
			WithArgs(existingID).WillReturnError(errors.New("soft delete failed"))
		mock.ExpectRollback()
		err := repo.Update(ctx, &spec)
		assert.EqualError(t, err, "soft delete failed")
	})

	t.Run("update existing license error", func(t *testing.T) {
		existingID := uuid.New()
		spec := *baseSpec
		spec.Licenses = []domain.LicenseOption{
			{ID: existingID, LicenseType: domain.LicenseBasic, Name: "Basic", Price: 10},
		}
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE specs").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery("SELECT \\* FROM license_options WHERE spec_id = \\$1 AND is_deleted = FALSE").
			WithArgs(specID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "spec_id", "license_type", "name", "price", "features", "file_types", "is_deleted"}).
				AddRow(existingID, specID, "Basic", "Basic", 10, pq.StringArray{"f1"}, pq.StringArray{"mp3"}, false))
		mock.ExpectExec("UPDATE license_options SET").WillReturnError(errors.New("license update failed"))
		mock.ExpectRollback()
		err := repo.Update(ctx, &spec)
		assert.EqualError(t, err, "license update failed")
	})

	t.Run("new license insert error", func(t *testing.T) {
		spec := *baseSpec
		spec.Licenses = []domain.LicenseOption{
			{LicenseType: domain.LicensePremium, Name: "Premium", Price: 20},
		}
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE specs").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery("SELECT \\* FROM license_options WHERE spec_id = \\$1 AND is_deleted = FALSE").
			WithArgs(specID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "spec_id", "license_type", "name", "price", "features", "file_types", "is_deleted"}))
		mock.ExpectExec("INSERT INTO license_options").WillReturnError(errors.New("insert failed"))
		mock.ExpectRollback()
		err := repo.Update(ctx, &spec)
		assert.EqualError(t, err, "insert failed")
	})
}

func TestPGSpecRepository_Create_GenreUpsertReturnsConcurrentWinner(t *testing.T) {
	db, mock, cleanup := newMockDB(t)
	defer cleanup()
	repo := postgres.NewSpecRepository(db)
	ctx := context.Background()
	specID := uuid.New()
	producerID := uuid.New()
	genreID := uuid.New()

	spec := &domain.Spec{
		ID:         specID,
		ProducerID: producerID,
		Title:      "Track",
		Category:   domain.CategoryBeat,
		Type:       "WAV",
		BPM:        120,
		Key:        "C",
		BasePrice:  10,
		ImageUrl:   "img",
		PreviewUrl: "prev",
		Duration:   120,
		Genres: []domain.Genre{
			{Name: "Hip Hop", Slug: "hip-hop"},
		},
		Licenses: []domain.LicenseOption{
			{LicenseType: domain.LicenseBasic, Name: "Basic", Price: 10, Features: pq.StringArray{"f1"}, FileTypes: pq.StringArray{"mp3"}},
		},
	}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO specs").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO spec_analytics").WithArgs(specID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("INSERT INTO genres[\\s\\S]*ON CONFLICT \\(slug\\) DO UPDATE[\\s\\S]*RETURNING id").
		WithArgs(sqlmock.AnyArg(), "Hip Hop", "hip-hop", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(genreID))
	mock.ExpectExec("INSERT INTO spec_genres \\(spec_id, genre_id\\) VALUES \\(\\$1, \\$2\\)").WithArgs(specID, genreID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO license_options").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.Create(ctx, spec))
}
