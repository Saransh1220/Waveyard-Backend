package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	authDomain "github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	catalogDomain "github.com/saransh1220/blueprint-audio/internal/modules/catalog/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
	sharedmoney "github.com/saransh1220/blueprint-audio/internal/shared/money"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func strp(s string) *string { return &s }

func specWithLicense(loID uuid.UUID, status catalogDomain.ProcessingStatus, currency string) *catalogDomain.Spec {
	if currency == "" {
		currency = sharedmoney.CurrencyINR
	}
	priceID := uuid.New()
	minor, _ := sharedmoney.MajorToMinor(10)
	return &catalogDomain.Spec{
		ID:               uuid.New(),
		Title:            "Track",
		ProcessingStatus: status,
		PriceCurrency:    currency,
		Licenses: []catalogDomain.LicenseOption{
			{
				ID:            loID,
				LicenseType:   catalogDomain.LicenseBasic,
				Name:          "Basic",
				Price:         10,
				PriceCurrency: currency,
				Prices: []sharedmoney.StoredPrice{
					{
						ID:              priceID,
						LicenseOptionID: loID,
						Currency:        currency,
						AmountMinor:     minor,
						AmountMajor:     10.0,
						Revision:        1,
						IsActive:        true,
					},
				},
			},
		},
	}
}

func TestPaymentService_CreateOrder_ValidationBranches(t *testing.T) {
	ctx := context.Background()
	s, _, _, _, sf, _, _, _ := newPaymentSvc()
	specID, loID := uuid.New(), uuid.New()

	sf.On("FindWithLicenses", ctx, specID).Return(specWithLicense(loID, catalogDomain.ProcessingStatusProcessing, "INR"), nil).Once()
	_, err := s.CreateOrder(ctx, uuid.New(), specID, loID, "INR")
	assert.EqualError(t, err, "Beat/Sample is not ready for purchase")

	sf.On("FindWithLicenses", ctx, specID).Return(specWithLicense(loID, catalogDomain.ProcessingStatusCompleted, "INR"), nil).Once()
	_, err = s.CreateOrder(ctx, uuid.New(), specID, uuid.New(), "INR")
	assert.EqualError(t, err, "license option not found")
}

func pendingOrder(rzpID *string) *domain.Order {
	return &domain.Order{
		ID:                 uuid.New(),
		UserID:             uuid.New(),
		SpecID:             uuid.New(),
		Status:             domain.OrderStatusPending,
		ExpiresAt:          time.Now().Add(time.Hour),
		Provider:           domain.ProviderRazorpay,
		ProviderCheckoutID: rzpID,
		LicenseType:        "Basic",
		Amount:             1000,
		Currency:           "INR",
		Notes:              map[string]any{"license_option_id": uuid.NewString()},
	}
}

func TestPaymentService_GetOrder(t *testing.T) {
	ctx := context.Background()
	s, or, _, _, _, _, _, _ := newPaymentSvc()
	id := uuid.New()
	want := &domain.Order{ID: id}
	or.On("GetByID", ctx, id).Return(want, nil).Once()
	got, err := s.GetOrder(ctx, id, uuid.Nil)
	require.NoError(t, err)
	assert.Same(t, want, got)

	or.On("GetByID", ctx, id).Return(nil, errors.New("nf")).Once()
	_, err = s.GetOrder(ctx, id, uuid.Nil)
	assert.EqualError(t, err, "nf")
}

func TestPaymentService_GetUserLicenses_SignsImages(t *testing.T) {
	ctx := context.Background()
	s, _, _, lr, _, fs, _, _ := newPaymentSvc()
	uid := uuid.New()
	img1, img2, img3, empty := "https://cdn/a.png", "https://cdn/b.png", "https://cdn/c.png", ""
	lr.On("ListByUser", ctx, uid, 5, 10, "q", "Basic").Return([]domain.License{
		{SpecImage: &img1}, {SpecImage: &img2}, {SpecImage: &img3}, {SpecImage: &empty}, {},
	}, 12, nil).Once()
	fs.On("GetKeyFromUrl", img1).Return("a", nil)
	fs.On("GetPresignedURL", ctx, "a", time.Hour).Return("signed-a", nil)
	fs.On("GetKeyFromUrl", img2).Return("", errors.New("bad url"))
	fs.On("GetKeyFromUrl", img3).Return("c", nil)
	fs.On("GetPresignedURL", ctx, "c", time.Hour).Return("", errors.New("sign fail"))

	got, total, err := s.GetUserLicenses(ctx, uid, 3, "q", "Basic")
	require.NoError(t, err)
	assert.Equal(t, 12, total)
	assert.Equal(t, "signed-a", *got[0].SpecImage)
	assert.Equal(t, img2, *got[1].SpecImage, "unchanged when key extraction fails")
	assert.Equal(t, img3, *got[2].SpecImage, "unchanged when signing fails")
	assert.Equal(t, "", *got[3].SpecImage)
	assert.Nil(t, got[4].SpecImage)
}

func TestPaymentService_GetUserOrders_NegativePageClampsOffset(t *testing.T) {
	ctx := context.Background()
	s, or, _, _, _, _, _, _ := newPaymentSvc()
	uid := uuid.New()
	or.On("ListByUser", ctx, uid, 20, 0).Return([]domain.Order{}, nil).Once()
	_, err := s.GetUserOrders(ctx, uid, -5)
	assert.NoError(t, err)
	or.AssertExpectations(t)
}

func TestPaymentService_GetLicenseDownloads_ByLicenseType(t *testing.T) {
	ctx := context.Background()
	uid := uuid.New()
	wav, stems := "https://cdn/wav", "https://cdn/stems"
	emptyStr := ""

	mkSvc := func(licType string, spec *catalogDomain.Spec) (*paymentService, *licenseRepoMock, *fileSvcMock, uuid.UUID) {
		s, _, _, lr, sf, fs, _, _ := newPaymentSvc()
		lic := &domain.License{ID: uuid.New(), UserID: uid, SpecID: spec.ID, LicenseType: licType, IsActive: true}
		lr.On("GetByID", ctx, lic.ID).Return(lic, nil).Once()
		sf.On("FindByIDIncludingDeleted", ctx, spec.ID).Return(spec, nil).Once()
		lr.On("IncrementDownloads", ctx, lic.ID).Return(errors.New("ignored")).Once()
		return s, lr, fs, lic.ID
	}
	fullSpec := func() *catalogDomain.Spec {
		return &catalogDomain.Spec{ID: uuid.New(), Title: "T", PreviewUrl: "https://cdn/preview", WavUrl: &wav, StemsUrl: &stems}
	}

	t.Run("Basic only mp3", func(t *testing.T) {
		s, _, fs, id := mkSvc("Basic", fullSpec())
		fs.On("GetKeyFromUrl", "https://cdn/preview").Return("p", nil)
		fs.On("GetPresignedDownloadURL", ctx, "p", "T.mp3", time.Hour).Return("signed-p", nil)
		res, err := s.GetLicenseDownloads(ctx, id, uid)
		require.NoError(t, err)
		assert.Equal(t, "signed-p", *res.MP3URL)
		assert.Nil(t, res.WAVURL)
		assert.Nil(t, res.StemsURL)
		assert.Equal(t, 3600, res.ExpiresIn)
		assert.Equal(t, "T", res.SpecTitle)
	})
	t.Run("Premium download fails when an entitled file cannot be located", func(t *testing.T) {
		s, _, fs, id := mkSvc("Premium", fullSpec())
		fs.On("GetKeyFromUrl", "https://cdn/preview").Return("p", nil)
		fs.On("GetPresignedDownloadURL", ctx, "p", "T.mp3", time.Hour).Return("signed-p", nil)
		fs.On("GetKeyFromUrl", wav).Return("", errors.New("bad"))
		res, err := s.GetLicenseDownloads(ctx, id, uid)
		require.Error(t, err)
		assert.Nil(t, res)
	})
	for _, lt := range []string{"Trackout", "Unlimited"} {
		t.Run(lt+" fails when a link cannot be signed", func(t *testing.T) {
			s, _, fs, id := mkSvc(lt, fullSpec())
			fs.On("GetKeyFromUrl", mock.Anything).Return("k", nil)
			fs.On("GetPresignedDownloadURL", ctx, "k", "T.mp3", time.Hour).Return("", errors.New("sign"))
			res, err := s.GetLicenseDownloads(ctx, id, uid)
			require.Error(t, err)
			assert.Nil(t, res)
		})
	}
	t.Run("empty urls yield nil", func(t *testing.T) {
		spec := &catalogDomain.Spec{ID: uuid.New(), WavUrl: &emptyStr, StemsUrl: &emptyStr}
		s, _, _, id := mkSvc("Unlimited", spec)
		res, err := s.GetLicenseDownloads(ctx, id, uid)
		require.NoError(t, err)
		assert.Nil(t, res.MP3URL)
		assert.Nil(t, res.WAVURL)
		assert.Nil(t, res.StemsURL)
	})
	t.Run("unknown type yields no urls", func(t *testing.T) {
		s, _, _, id := mkSvc("Exclusive", fullSpec())
		res, err := s.GetLicenseDownloads(ctx, id, uid)
		require.NoError(t, err)
		assert.Nil(t, res.MP3URL)
		assert.Equal(t, "Exclusive", res.LicenseType)
	})
}

func TestPaymentService_GetProducerOrders_MapsAndClampsOffset(t *testing.T) {
	ctx := context.Background()
	s, or, _, _, _, _, _, _ := newPaymentSvc()
	pid := uuid.New()
	rz := "order_z"
	or.On("ListByProducer", ctx, pid, 10, 0).Return([]domain.OrderWithBuyer{{
		Order:     domain.Order{ID: uuid.New(), Amount: 1550, Currency: "USD", Status: domain.OrderStatusPaid, LicenseType: "Basic", Provider: domain.ProviderRazorpay, ProviderCheckoutID: &rz},
		BuyerName: "B", BuyerEmail: "b@x.com", SpecTitle: "S",
	}}, 1, nil).Once()
	res, err := s.GetProducerOrders(ctx, pid, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, 0, res.Offset)
	assert.Equal(t, 1, res.Total)
	require.Len(t, res.Orders, 1)
	assert.Equal(t, 15.5, res.Orders[0].Amount)
	assert.Equal(t, "B", res.Orders[0].BuyerName)
	assert.Equal(t, &rz, res.Orders[0].RazorpayOrderID)
}

func TestPaymentService_SendReceiptEmail_Branches(t *testing.T) {
	ctx := context.Background()
	order := &domain.Order{ID: uuid.New(), UserID: uuid.New(), Amount: 2500, Currency: "usd", LicenseType: "Basic", Notes: map[string]any{}}
	license := &domain.License{ID: uuid.New()}

	t.Run("nil sender or finder is a no-op", func(t *testing.T) {
		s, _, _, _, _, _, _, _ := newPaymentSvc()
		s.emailSender = nil
		assert.NoError(t, s.sendReceiptEmail(ctx, order, &domain.Payment{}, license))
		s2, _, _, _, _, _, _, _ := newPaymentSvc()
		s2.userFinder = nil
		assert.NoError(t, s2.sendReceiptEmail(ctx, order, &domain.Payment{}, license))
	})
	t.Run("user lookup error", func(t *testing.T) {
		s, _, _, _, _, _, uf, _ := newPaymentSvc()
		uf.On("FindByID", ctx, order.UserID).Return(nil, errors.New("no user")).Once()
		assert.EqualError(t, s.sendReceiptEmail(ctx, order, &domain.Payment{}, license), "no user")
	})
	t.Run("uses account email and default title", func(t *testing.T) {
		s, _, _, _, _, _, uf, es := newPaymentSvc()
		uf.On("FindByID", ctx, order.UserID).Return(&authDomain.User{Email: "acct@example.com", Name: "Acct"}, nil).Once()
		es.On("Send", ctx, mock.MatchedBy(func(m interface{}) bool { return true })).Return(errors.New("smtp")).Once()
		err := s.sendReceiptEmail(ctx, order, &domain.Payment{Email: strp("  ")}, license)
		assert.EqualError(t, err, "smtp")
		es.AssertNumberOfCalls(t, "Send", 1)
	})
	t.Run("payment email overrides", func(t *testing.T) {
		s, _, _, _, _, _, uf, es := newPaymentSvc()
		o := *order
		o.Notes = map[string]any{"spec_title": "Midnight"}
		uf.On("FindByID", ctx, o.UserID).Return(&authDomain.User{Email: "acct@example.com", Name: "Acct"}, nil).Once()
		es.On("Send", ctx, mock.Anything).Return(nil).Once()
		assert.NoError(t, s.sendReceiptEmail(ctx, &o, &domain.Payment{Email: strp("pay@example.com")}, license))
		msg := es.Calls[0].Arguments.Get(1)
		assert.Contains(t, toString(msg), "pay@example.com")
		assert.Contains(t, toString(msg), "Midnight")
	})
}

func toString(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestFormatMoney(t *testing.T) {
	assert.Equal(t, "INR 12.34", formatMoney(1234, "inr"))
	assert.Equal(t, "USD 5.00", formatMoney(500, "usd"))
	assert.Equal(t, "EUR 0.99", formatMoney(99, "Eur"))
}

func TestPaymentService_CreateOrderWithOffer_ExplicitPricing(t *testing.T) {
	ctx := context.Background()
	s, or, _, _, sf, _, _, _ := newPaymentSvc()
	specID, loID := uuid.New(), uuid.New()
	priceID := uuid.New()

	spec := &catalogDomain.Spec{
		ID:               specID,
		Title:            "Explicit Beat",
		ProcessingStatus: catalogDomain.ProcessingStatusCompleted,
		Licenses: []catalogDomain.LicenseOption{
			{
				ID:            loID,
				LicenseType:   catalogDomain.LicenseBasic,
				Name:          "Basic",
				Price:         15.00,
				PriceCurrency: "USD",
				Prices: []sharedmoney.StoredPrice{
					{
						ID:              priceID,
						LicenseOptionID: loID,
						Currency:        "USD",
						AmountMinor:     1500,
						AmountMajor:     15.00,
						Revision:        2,
						IsActive:        true,
					},
					{
						ID:              uuid.New(),
						LicenseOptionID: loID,
						Currency:        "INR",
						AmountMinor:     100000,
						AmountMajor:     1000.00,
						Revision:        1,
						IsActive:        true,
					},
				},
			},
		},
	}

	t.Run("Unsupported currency is rejected and never converted", func(t *testing.T) {
		sf.On("FindWithLicenses", ctx, specID).Return(spec, nil).Once()
		_, err := s.CreateOrderWithOffer(ctx, CreateOrderInput{
			UserID:          uuid.New(),
			SpecID:          specID,
			LicenseOptionID: loID,
			Currency:        "EUR",
		})
		assert.ErrorIs(t, err, sharedmoney.ErrInvalidCurrency)
	})

	t.Run("Stale amount returns PriceMismatchError with current offer", func(t *testing.T) {
		sf.On("FindWithLicenses", ctx, specID).Return(spec, nil).Once()
		staleAmount := 1499
		_, err := s.CreateOrderWithOffer(ctx, CreateOrderInput{
			UserID:              uuid.New(),
			SpecID:              specID,
			LicenseOptionID:     loID,
			Currency:            "USD",
			ExpectedAmountMinor: &staleAmount,
		})
		var mismatch *PriceMismatchError
		require.True(t, errors.As(err, &mismatch))
		assert.Equal(t, 1500, mismatch.CurrentOffer.AmountMinor)
		assert.Equal(t, "USD", mismatch.CurrentOffer.Currency)
	})

	t.Run("Stale revision returns PriceMismatchError", func(t *testing.T) {
		sf.On("FindWithLicenses", ctx, specID).Return(spec, nil).Once()
		oldRev := int64(1)
		_, err := s.CreateOrderWithOffer(ctx, CreateOrderInput{
			UserID:           uuid.New(),
			SpecID:           specID,
			LicenseOptionID:  loID,
			Currency:         "USD",
			ExpectedRevision: &oldRev,
		})
		var mismatch *PriceMismatchError
		require.True(t, errors.As(err, &mismatch))
		assert.Equal(t, int64(2), mismatch.CurrentOffer.Revision)
	})

	t.Run("Matching evidence succeeds, freezes exact amount and pricing snapshot", func(t *testing.T) {
		sf.On("FindWithLicenses", ctx, specID).Return(spec, nil).Once()
		or.On("Create", ctx, mock.Anything).Return(nil).Once()

		expectedMinor := 1500
		expectedRev := int64(2)
		expectedCurr := "USD"
		order, err := s.CreateOrderWithOffer(ctx, CreateOrderInput{
			UserID:              uuid.New(),
			SpecID:              specID,
			LicenseOptionID:     loID,
			Currency:            "USD",
			ExpectedPriceID:     &priceID,
			ExpectedAmountMinor: &expectedMinor,
			ExpectedRevision:    &expectedRev,
			ExpectedCurrency:    &expectedCurr,
		})
		require.NoError(t, err)
		assert.Equal(t, 1500, order.Amount)
		assert.Equal(t, "USD", order.Currency)
		assert.Equal(t, "dodo", order.Provider)

		snapshot, ok := order.Notes["pricing_snapshot"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, priceID.String(), snapshot["price_id"])
		assert.Equal(t, int64(2), snapshot["revision"])
		assert.Equal(t, 1500, snapshot["amount_minor"])
		assert.Equal(t, "USD", snapshot["currency"])
	})
}
