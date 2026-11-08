package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	authDomain "github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	catalogDomain "github.com/saransh1220/blueprint-audio/internal/modules/catalog/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
	sharedemail "github.com/saransh1220/blueprint-audio/internal/shared/infrastructure/email"
	sharedmoney "github.com/saransh1220/blueprint-audio/internal/shared/money"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type orderRepoMock struct{ mock.Mock }

func (m *orderRepoMock) Create(ctx context.Context, order *domain.Order) error {
	args := m.Called(ctx, order)
	return args.Error(0)
}
func (m *orderRepoMock) GetByID(ctx context.Context, id uuid.UUID) (*domain.Order, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Order), args.Error(1)
}
func (m *orderRepoMock) GetByProviderCheckoutID(ctx context.Context, provider, checkoutID string) (*domain.Order, error) {
	args := m.Called(ctx, provider, checkoutID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Order), args.Error(1)
}
func (m *orderRepoMock) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.OrderStatus) error {
	args := m.Called(ctx, id, status)
	return args.Error(0)
}
func (m *orderRepoMock) MarkPaidIfPayable(ctx context.Context, id uuid.UUID, providerPaymentID string) (bool, error) {
	args := m.Called(ctx, id, providerPaymentID)
	return args.Bool(0), args.Error(1)
}
func (m *orderRepoMock) ListByUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]domain.Order, error) {
	args := m.Called(ctx, userID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Order), args.Error(1)
}
func (m *orderRepoMock) ListByProducer(ctx context.Context, producerID uuid.UUID, limit, offset int) ([]domain.OrderWithBuyer, int, error) {
	args := m.Called(ctx, producerID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]domain.OrderWithBuyer), args.Int(1), args.Error(2)
}
func (m *orderRepoMock) AttachCheckoutDetails(ctx context.Context, id uuid.UUID, providerCheckoutID, checkoutURL string) error {
	args := m.Called(ctx, id, providerCheckoutID, checkoutURL)
	return args.Error(0)
}
func (m *orderRepoMock) MarkRefunded(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}


type paymentRepoMock struct{ mock.Mock }

func (m *paymentRepoMock) Create(ctx context.Context, payment *domain.Payment) error {
	args := m.Called(ctx, payment)
	return args.Error(0)
}
func (m *paymentRepoMock) GetByID(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Payment), args.Error(1)
}
func (m *paymentRepoMock) GetByOrderID(ctx context.Context, orderID uuid.UUID) (*domain.Payment, error) {
	args := m.Called(ctx, orderID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Payment), args.Error(1)
}
func (m *paymentRepoMock) GetByProviderPaymentID(ctx context.Context, provider, providerPaymentID string) (*domain.Payment, error) {
	args := m.Called(ctx, provider, providerPaymentID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Payment), args.Error(1)
}
func (m *paymentRepoMock) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.PaymentStatus) error {
	args := m.Called(ctx, id, status)
	return args.Error(0)
}


type licenseRepoMock struct{ mock.Mock }

func (m *licenseRepoMock) Create(ctx context.Context, license *domain.License) error {
	args := m.Called(ctx, license)
	return args.Error(0)
}
func (m *licenseRepoMock) GetByID(ctx context.Context, id uuid.UUID) (*domain.License, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.License), args.Error(1)
}
func (m *licenseRepoMock) GetByOrderID(ctx context.Context, orderID uuid.UUID) (*domain.License, error) {
	args := m.Called(ctx, orderID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.License), args.Error(1)
}
func (m *licenseRepoMock) ListByUser(ctx context.Context, userID uuid.UUID, limit, offset int, search, licenseType string) ([]domain.License, int, error) {
	args := m.Called(ctx, userID, limit, offset, search, licenseType)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]domain.License), args.Int(1), args.Error(2)
}
func (m *licenseRepoMock) IncrementDownloads(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}
func (m *licenseRepoMock) Revoke(ctx context.Context, id uuid.UUID, reason string) error {
	args := m.Called(ctx, id, reason)
	return args.Error(0)
}

type specFinderMock struct{ mock.Mock }

func (m *specFinderMock) FindByID(ctx context.Context, id uuid.UUID) (*catalogDomain.Spec, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*catalogDomain.Spec), args.Error(1)
}
func (m *specFinderMock) FindByIDIncludingDeleted(ctx context.Context, id uuid.UUID) (*catalogDomain.Spec, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*catalogDomain.Spec), args.Error(1)
}
func (m *specFinderMock) FindWithLicenses(ctx context.Context, id uuid.UUID) (*catalogDomain.Spec, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*catalogDomain.Spec), args.Error(1)
}
func (m *specFinderMock) Exists(ctx context.Context, id uuid.UUID) (bool, error) {
	args := m.Called(ctx, id)
	return args.Bool(0), args.Error(1)
}
func (m *specFinderMock) GetLicenseByID(ctx context.Context, licenseID uuid.UUID) (*catalogDomain.LicenseOption, error) {
	args := m.Called(ctx, licenseID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*catalogDomain.LicenseOption), args.Error(1)
}
func (m *specFinderMock) GetLicensePrices(ctx context.Context, licenseID uuid.UUID) ([]sharedmoney.StoredPrice, error) {
	args := m.Called(ctx, licenseID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]sharedmoney.StoredPrice), args.Error(1)
}
func (m *specFinderMock) UpsertLicensePrices(ctx context.Context, licenseID uuid.UUID, prices []sharedmoney.StoredPrice) error {
	args := m.Called(ctx, licenseID, prices)
	return args.Error(0)
}

type fileSvcMock struct{ mock.Mock }

func (m *fileSvcMock) GetKeyFromUrl(fileURL string) (string, error) {
	args := m.Called(fileURL)
	return args.String(0), args.Error(1)
}
func (m *fileSvcMock) GetPresignedURL(ctx context.Context, key string, expiration time.Duration) (string, error) {
	args := m.Called(ctx, key, expiration)
	return args.String(0), args.Error(1)
}

type userFinderMock struct{ mock.Mock }

func (m *userFinderMock) FindByID(ctx context.Context, id uuid.UUID) (*authDomain.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*authDomain.User), args.Error(1)
}
func (m *userFinderMock) Exists(ctx context.Context, id uuid.UUID) (bool, error) {
	args := m.Called(ctx, id)
	return args.Bool(0), args.Error(1)
}

type emailSenderMock struct{ mock.Mock }

func (m *emailSenderMock) Send(ctx context.Context, msg sharedemail.Message) error {
	args := m.Called(ctx, msg)
	return args.Error(0)
}

func newPaymentSvc() (*paymentService, *orderRepoMock, *paymentRepoMock, *licenseRepoMock, *specFinderMock, *fileSvcMock, *userFinderMock, *emailSenderMock) {
	or := new(orderRepoMock)
	pr := new(paymentRepoMock)
	lr := new(licenseRepoMock)
	sf := new(specFinderMock)
	fs := new(fileSvcMock)
	uf := new(userFinderMock)
	es := new(emailSenderMock)
	or.On("AttachCheckoutDetails", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	return &paymentService{
		orderRepo:   or,
		paymentRepo: pr,
		licenseRepo: lr,
		specFinder:  sf,
		userFinder:  uf,
		fileService: fs,
		gateways:    newFakeRegistry(),
		emailSender: es,
		appBaseURL:  "http://localhost:4200",
	}, or, pr, lr, sf, fs, uf, es
}

func TestPaymentService_CreateOrder_Errors(t *testing.T) {
	s, _, _, _, sf, _, _, _ := newPaymentSvc()
	ctx := context.Background()
	userID := uuid.New()
	specID := uuid.New()
	licenseID := uuid.New()

	sf.On("FindWithLicenses", ctx, specID).Return(nil, errors.New("not found")).Once()
	_, err := s.CreateOrder(ctx, userID, specID, licenseID, "INR")
	assert.EqualError(t, err, "Beat/Sample not found")

	spec := &catalogDomain.Spec{ID: specID, Title: "Track", Licenses: []catalogDomain.LicenseOption{}}
	sf.On("FindWithLicenses", ctx, specID).Return(spec, nil).Once()
	_, err = s.CreateOrder(ctx, userID, specID, licenseID, "INR")
	assert.EqualError(t, err, "license option not found")

	spec.ProcessingStatus = catalogDomain.ProcessingStatusProcessing
	sf.On("FindWithLicenses", ctx, specID).Return(spec, nil).Once()
	_, err = s.CreateOrder(ctx, userID, specID, licenseID, "INR")
	assert.EqualError(t, err, "Beat/Sample is not ready for purchase")
}

func TestPaymentService_GetUserOrdersAndProducerOrders(t *testing.T) {
	s, or, _, _, _, _, _, _ := newPaymentSvc()
	ctx := context.Background()
	userID := uuid.New()
	producerID := uuid.New()

	or.On("ListByUser", ctx, userID, 20, 0).Return([]domain.Order{}, nil).Once()
	orders, err := s.GetUserOrders(ctx, userID, -1)
	assert.NoError(t, err)
	assert.NotNil(t, orders)

	or.On("ListByProducer", ctx, producerID, 50, 0).Return([]domain.OrderWithBuyer{}, 0, nil).Once()
	resp, err := s.GetProducerOrders(ctx, producerID, 0, 50)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 50, resp.Limit)
}

func TestPaymentService_GetUserLicensesAndDownloads(t *testing.T) {
	s, _, _, lr, sf, fs, _, _ := newPaymentSvc()
	ctx := context.Background()
	userID := uuid.New()
	specID := uuid.New()
	licenseID := uuid.New()
	wav := "http://bucket/wav.wav"
	stems := "http://bucket/stems.zip"

	licenses := []domain.License{
		{ID: licenseID, UserID: userID, SpecID: specID, LicenseType: "Premium", IsActive: true, SpecImage: ptr("http://bucket/img.jpg")},
	}
	lr.On("ListByUser", ctx, userID, 5, 0, "", "").Return(licenses, 1, nil).Once()
	fs.On("GetKeyFromUrl", "http://bucket/img.jpg").Return("img.jpg", nil).Once()
	fs.On("GetPresignedURL", ctx, "img.jpg", mock.Anything).Return("signed-img", nil).Once()
	out, total, err := s.GetUserLicenses(ctx, userID, 0, "", "")
	assert.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.NotNil(t, out[0].SpecImage)

	lic := &domain.License{ID: licenseID, UserID: userID, SpecID: specID, LicenseType: "Unlimited", IsActive: true}
	spec := &catalogDomain.Spec{ID: specID, Title: "Track", PreviewUrl: "http://bucket/prev.mp3", WavUrl: &wav, StemsUrl: &stems}
	lr.On("GetByID", ctx, licenseID).Return(lic, nil).Once()
	sf.On("FindByIDIncludingDeleted", ctx, specID).Return(spec, nil).Once()
	fs.On("GetKeyFromUrl", "http://bucket/prev.mp3").Return("prev.mp3", nil).Once()
	fs.On("GetPresignedURL", ctx, "prev.mp3", mock.Anything).Return("signed-prev", nil).Once()
	fs.On("GetKeyFromUrl", wav).Return("wav.wav", nil).Once()
	fs.On("GetPresignedURL", ctx, "wav.wav", mock.Anything).Return("signed-wav", nil).Once()
	fs.On("GetKeyFromUrl", stems).Return("stems.zip", nil).Once()
	fs.On("GetPresignedURL", ctx, "stems.zip", mock.Anything).Return("signed-stems", nil).Once()
	lr.On("IncrementDownloads", ctx, licenseID).Return(nil).Once()
	dl, err := s.GetLicenseDownloads(ctx, licenseID, userID)
	assert.NoError(t, err)
	assert.NotNil(t, dl)
	assert.NotNil(t, dl.MP3URL)
	assert.NotNil(t, dl.WAVURL)
	assert.NotNil(t, dl.StemsURL)
}

func TestPaymentService_GetLicenseDownloads_Errors(t *testing.T) {
	s, _, _, lr, sf, _, _, _ := newPaymentSvc()
	ctx := context.Background()
	licenseID := uuid.New()
	userID := uuid.New()
	specID := uuid.New()

	lr.On("GetByID", ctx, licenseID).Return(nil, errors.New("missing")).Once()
	_, err := s.GetLicenseDownloads(ctx, licenseID, userID)
	assert.EqualError(t, err, "license not found")

	lr.On("GetByID", ctx, licenseID).Return(&domain.License{ID: licenseID, UserID: uuid.New(), SpecID: specID, IsActive: true}, nil).Once()
	_, err = s.GetLicenseDownloads(ctx, licenseID, userID)
	assert.EqualError(t, err, "unauthorized: you do not own this license")

	lr.On("GetByID", ctx, licenseID).Return(&domain.License{ID: licenseID, UserID: userID, SpecID: specID, IsActive: false}, nil).Once()
	_, err = s.GetLicenseDownloads(ctx, licenseID, userID)
	assert.EqualError(t, err, "license is not active")

	lr.On("GetByID", ctx, licenseID).Return(&domain.License{ID: licenseID, UserID: userID, SpecID: specID, IsActive: true, IsRevoked: true}, nil).Once()
	_, err = s.GetLicenseDownloads(ctx, licenseID, userID)
	assert.EqualError(t, err, "license has been revoked")

	lr.On("GetByID", ctx, licenseID).Return(&domain.License{ID: licenseID, UserID: userID, SpecID: specID, IsActive: true}, nil).Once()
	sf.On("FindByIDIncludingDeleted", ctx, specID).Return(nil, errors.New("missing")).Once()
	_, err = s.GetLicenseDownloads(ctx, licenseID, userID)
	assert.EqualError(t, err, "spec not found")
}

func TestPaymentService_IssueLicense(t *testing.T) {
	s, _, _, lr, sf, _, _, _ := newPaymentSvc()
	ctx := context.Background()
	specID := uuid.New()
	loID := uuid.New()

	order := &domain.Order{
		ID:          uuid.New(),
		UserID:      uuid.New(),
		SpecID:      specID,
		LicenseType: "Premium",
		Amount:      1200,
		Notes:       map[string]any{"license_option_id": loID.String()},
	}

	spec := &catalogDomain.Spec{
		ID: specID,
		Licenses: []catalogDomain.LicenseOption{
			{ID: loID, LicenseType: catalogDomain.LicenseType("Premium"), Name: "Premium"},
		},
	}
	sf.On("FindWithLicenses", ctx, specID).Return(spec, nil).Once()
	lr.On("Create", ctx, mock.AnythingOfType("*domain.License")).Return(nil).Once()
	lic, err := s.issueLicense(ctx, order)
	assert.NoError(t, err)
	assert.Equal(t, loID, lic.LicenseOptionID)
}

func TestPaymentService_IssueLicense_InvalidLicenseOptionID(t *testing.T) {
	s, _, _, _, _, _, _, _ := newPaymentSvc()
	ctx := context.Background()
	order := &domain.Order{
		ID:          uuid.New(),
		UserID:      uuid.New(),
		SpecID:      uuid.New(),
		LicenseType: "Premium",
		Amount:      1200,
		Notes:       map[string]any{"license_option_id": "not-a-uuid"},
	}
	_, err := s.issueLicense(ctx, order)
	assert.EqualError(t, err, "invalid license_option_id")
}

func ptr(s string) *string { return &s }

func TestPaymentService_GetUserLicensesAndProducerOrders_ErrorsAndFallbacks(t *testing.T) {
	s, or, _, lr, _, fs, _, _ := newPaymentSvc()
	ctx := context.Background()
	userID := uuid.New()
	producerID := uuid.New()

	lr.On("ListByUser", ctx, userID, 5, 0, "q", "Basic").Return(nil, 0, errors.New("db")).Once()
	_, _, err := s.GetUserLicenses(ctx, userID, 1, "q", "Basic")
	assert.EqualError(t, err, "db")

	img := "http://bucket/img.jpg"
	licenses := []domain.License{{ID: uuid.New(), UserID: userID, SpecImage: &img}}
	lr.On("ListByUser", ctx, userID, 5, 0, "", "").Return(licenses, 1, nil).Once()
	fs.On("GetKeyFromUrl", img).Return("", errors.New("bad key")).Once()
	out, total, err := s.GetUserLicenses(ctx, userID, 1, "", "")
	assert.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Equal(t, img, *out[0].SpecImage)

	or.On("ListByProducer", ctx, producerID, 50, 0).Return(nil, 0, errors.New("repo")).Once()
	_, err = s.GetProducerOrders(ctx, producerID, 1, 50)
	assert.EqualError(t, err, "repo")
}

func TestPaymentService_IssueLicense_MissingOptionID(t *testing.T) {
	s, _, _, _, _, _, _, _ := newPaymentSvc()
	_, err := s.issueLicense(context.Background(), &domain.Order{Notes: map[string]any{}})
	assert.EqualError(t, err, "license_option_id missing")
}
