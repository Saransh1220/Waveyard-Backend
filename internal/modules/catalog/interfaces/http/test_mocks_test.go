package http_test

import (
	"context"
	"io"
	"mime/multipart"
	"time"

	"github.com/google/uuid"
	analyticsDomain "github.com/saransh1220/blueprint-audio/internal/modules/analytics/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/catalog/domain"
	notificationDomain "github.com/saransh1220/blueprint-audio/internal/modules/notification/domain"
	"github.com/stretchr/testify/mock"
)

type mockSpecService struct{ mock.Mock }

func (m *mockSpecService) CreateSpec(ctx context.Context, spec *domain.Spec) error {
	args := m.Called(ctx, spec)
	return args.Error(0)
}

func (m *mockSpecService) GetSpec(ctx context.Context, id uuid.UUID) (*domain.Spec, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Spec), args.Error(1)
}

func (m *mockSpecService) ListSpecs(ctx context.Context, filter domain.SpecFilter) ([]domain.Spec, int, error) {
	args := m.Called(ctx, filter)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]domain.Spec), args.Int(1), args.Error(2)
}

func (m *mockSpecService) UpdateSpec(ctx context.Context, spec *domain.Spec, producerID uuid.UUID) error {
	args := m.Called(ctx, spec, producerID)
	return args.Error(0)
}

func (m *mockSpecService) DeleteSpec(ctx context.Context, id uuid.UUID, producerID uuid.UUID) error {
	args := m.Called(ctx, id, producerID)
	return args.Error(0)
}

func (m *mockSpecService) GetUserSpecs(ctx context.Context, producerID uuid.UUID, page int, limit int, filters ...domain.ProducerSpecFilter) ([]domain.Spec, int, error) {
	args := m.Called(ctx, producerID, page, limit)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]domain.Spec), args.Int(1), args.Error(2)
}

func (m *mockSpecService) UpdateFilesAndStatus(ctx context.Context, id uuid.UUID, files map[string]*string, status domain.ProcessingStatus) error {
	args := m.Called(ctx, id, files, status)
	return args.Error(0)
}

func (m *mockSpecService) GetSpecByShortCode(ctx context.Context, shortCode string) (*domain.Spec, error) {
	args := m.Called(ctx, shortCode)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Spec), args.Error(1)
}

func (m *mockSpecService) GetSpecBySlug(ctx context.Context, slug string) (*domain.Spec, error) {
	args := m.Called(ctx, slug)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Spec), args.Error(1)
}

func (m *mockSpecService) GetHome(ctx context.Context, params domain.HomepageParams) (*domain.HomepageData, error) {
	args := m.Called(ctx, params)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.HomepageData), args.Error(1)
}

type mockAnalyticsService struct{ mock.Mock }

func (m *mockAnalyticsService) GetPublicAnalytics(ctx context.Context, specID uuid.UUID, userID *uuid.UUID) (*analyticsDomain.PublicAnalytics, error) {
	args := m.Called(ctx, specID, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*analyticsDomain.PublicAnalytics), args.Error(1)
}

func (m *mockAnalyticsService) TrackFreeDownload(ctx context.Context, specID uuid.UUID) error {
	args := m.Called(ctx, specID)
	return args.Error(0)
}

type mockFileService struct{ mock.Mock }

func (m *mockFileService) Upload(ctx context.Context, file multipart.File, header *multipart.FileHeader, folder string) (string, string, error) {
	args := m.Called(ctx, file, header, folder)
	return args.String(0), args.String(1), args.Error(2)
}

func (m *mockFileService) UploadWithKey(ctx context.Context, file io.Reader, key string, contentType string) (string, error) {
	args := m.Called(ctx, file, key, contentType)
	return args.String(0), args.Error(1)
}

func (m *mockFileService) GetPresignedURL(ctx context.Context, key string, expiration time.Duration) (string, error) {
	args := m.Called(ctx, key, expiration)
	return args.String(0), args.Error(1)
}

func (m *mockFileService) GetPresignedDownloadURL(ctx context.Context, key string, filename string, expiration time.Duration) (string, error) {
	args := m.Called(ctx, key, filename, expiration)
	return args.String(0), args.Error(1)
}

func (m *mockFileService) GetKeyFromUrl(fileURL string) (string, error) {
	args := m.Called(fileURL)
	return args.String(0), args.Error(1)
}

func (m *mockFileService) Delete(ctx context.Context, key string) error {
	args := m.Called(ctx, key)
	return args.Error(0)
}

type mockNotificationService struct{ mock.Mock }

func (m *mockNotificationService) Create(ctx context.Context, userID uuid.UUID, title, message string, notificationType notificationDomain.NotificationType) error {
	args := m.Called(ctx, userID, title, message, notificationType)
	return args.Error(0)
}
