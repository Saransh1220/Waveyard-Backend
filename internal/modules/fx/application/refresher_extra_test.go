package application_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/saransh1220/blueprint-audio/internal/modules/fx/application"
	"github.com/saransh1220/blueprint-audio/internal/modules/fx/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestRefresher_ExtraBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("default config when non-positive", func(t *testing.T) {
		r := application.NewRefresher(nil, nil, nil, application.Config{})
		assert.NotNil(t, r)
		_, err := r.Refresh(ctx, false)
		assert.EqualError(t, err, "no fx rate provider configured")
	})

	t.Run("manual refresh cooldown active", func(t *testing.T) {
		repo := new(mockRateRepo)
		prov := new(mockRateProvider)
		lockMgr := new(mockLockManager)

		r := application.NewRefresher(repo, lockMgr, prov, application.Config{RefreshInterval: time.Hour})

		lockMgr.On("TryLock", mock.Anything, mock.Anything).Return(func() error { return nil }, true, nil)
		repo.On("LatestPerPair", mock.Anything).Return([]domain.Rate{}, nil)
		repo.On("Latest", mock.Anything, mock.Anything, mock.Anything).Return(&domain.Rate{}, nil).Maybe()
		prov.On("Fetch", mock.Anything, mock.Anything, mock.Anything).Return(&domain.ProviderRates{
			Base:  "INR",
			AsOf:  time.Now(),
			Rates: map[string]*big.Rat{"USD": big.NewRat(1, 85)},
		}, nil).Maybe()
		repo.On("AppendBatch", mock.Anything, mock.Anything).Return(nil).Once()

		res, err := r.Refresh(ctx, true)
		require.NoError(t, err)
		assert.False(t, res.Skipped)

		// Immediate second manual refresh triggers ErrCooldownActive
		_, err = r.Refresh(ctx, true)
		require.ErrorIs(t, err, domain.ErrCooldownActive)
	})
}
