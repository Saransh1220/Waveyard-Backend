package payment

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	authDomain "github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	catalogDomain "github.com/saransh1220/blueprint-audio/internal/modules/catalog/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/infrastructure/dodo"
	sharedemail "github.com/saransh1220/blueprint-audio/internal/shared/infrastructure/email"
	sharedmoney "github.com/saransh1220/blueprint-audio/internal/shared/money"
	"github.com/stretchr/testify/require"
)

type nilSpecFinder struct{}

func (nilSpecFinder) FindByID(_ context.Context, _ uuid.UUID) (*catalogDomain.Spec, error) {
	return nil, nil
}
func (nilSpecFinder) FindByIDIncludingDeleted(_ context.Context, _ uuid.UUID) (*catalogDomain.Spec, error) {
	return nil, nil
}
func (nilSpecFinder) FindWithLicenses(_ context.Context, _ uuid.UUID) (*catalogDomain.Spec, error) {
	return &catalogDomain.Spec{}, nil
}
func (nilSpecFinder) Exists(_ context.Context, _ uuid.UUID) (bool, error) { return false, nil }
func (nilSpecFinder) GetLicenseByID(_ context.Context, _ uuid.UUID) (*catalogDomain.LicenseOption, error) {
	return nil, nil
}
func (nilSpecFinder) GetLicensePrices(_ context.Context, _ uuid.UUID) ([]sharedmoney.StoredPrice, error) {
	return nil, nil
}
func (nilSpecFinder) UpsertLicensePrices(_ context.Context, _ uuid.UUID, _ []sharedmoney.StoredPrice) error {
	return nil
}

type nilFileService struct{}

func (nilFileService) GetKeyFromUrl(_ string) (string, error) { return "", nil }
func (nilFileService) GetPresignedURL(_ context.Context, _ string, _ time.Duration) (string, error) {
	return "", nil
}

func (nilFileService) GetPresignedDownloadURL(_ context.Context, _, _ string, _ time.Duration) (string, error) {
	return "", nil
}

type nilUserFinder struct{}

func (nilUserFinder) FindByID(_ context.Context, id uuid.UUID) (*authDomain.User, error) {
	return &authDomain.User{ID: id}, nil
}
func (nilUserFinder) Exists(_ context.Context, _ uuid.UUID) (bool, error) { return true, nil }

type dummySnapshotter struct{}

func (dummySnapshotter) Snapshot(_ context.Context, _ string) (*domain.OrderFXSnapshot, error) {
	return nil, nil
}

func TestModuleAccessors(t *testing.T) {
	m := NewModule(&sqlx.DB{}, nilSpecFinder{}, nilUserFinder{}, nilFileService{}, sharedemail.NewSender(sharedemail.Config{}), "http://localhost:4200", GatewayConfig{
		Dodo: dodo.Config{ReturnURL: "http://localhost:4200/custom-return"},
	})
	require.NotNil(t, m)
	require.NotNil(t, m.HTTPHandler())
	require.NotNil(t, m.Service())
	m.SetSnapshotter(dummySnapshotter{})
	require.Equal(t, map[string]string{"INR": "razorpay", "USD": "dodo"}, m.CurrencyGatewayNames())
}
