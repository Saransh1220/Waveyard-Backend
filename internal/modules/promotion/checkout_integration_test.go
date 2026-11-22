package promotion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/saransh1220/blueprint-audio/internal/gateway/middleware"
	catalog "github.com/saransh1220/blueprint-audio/internal/modules/catalog/infrastructure/persistence/postgres"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment"
	paymentdomain "github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/infrastructure/dodo"
	"github.com/saransh1220/blueprint-audio/internal/modules/promotion/application"
	"github.com/saransh1220/blueprint-audio/internal/modules/promotion/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/promotion/infrastructure/persistence/postgres"
	"github.com/stretchr/testify/require"
)

func TestCheckoutUsesConfirmedPublishedCoupon(t *testing.T) {
	dsn := os.Getenv("TEST_PROMOTIONS_DB_URL")
	if dsn == "" {
		t.Skip("TEST_PROMOTIONS_DB_URL must point to a disposable, migrated PostgreSQL database")
	}
	db, err := sqlx.Connect("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	producer, beat, option := seedCatalog(t, db)
	buyer, otherBeat, otherOption := seedCatalog(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	basic := uuid.New()
	_, err = db.ExecContext(ctx, `INSERT INTO license_options(id,spec_id,license_type,name,price,features,file_types,price_currency)
		VALUES($1,$2,'Basic','MP3',8,'{}','{MP3}','USD')`, basic, beat)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO license_option_prices(id,license_option_id,currency,amount_minor,amount_major)
		VALUES($1,$2,'USD',800,8),($3,$4,'INR',100000,1000)`, uuid.New(), basic, uuid.New(), option)
	require.NoError(t, err)

	var providerCalls atomic.Int32
	providerAmounts := make(chan int, 4)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Cart     []struct{ Amount int } `json:"product_cart"`
			Currency string                 `json:"billing_currency"`
		}
		if r.URL.Path != "/checkouts" || json.NewDecoder(r.Body).Decode(&request) != nil ||
			len(request.Cart) != 1 || request.Currency != "USD" {
			t.Error("provider received incorrect checkout amount or currency")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		call := providerCalls.Add(1)
		select {
		case providerAmounts <- request.Cart[0].Amount:
		default:
			t.Error("unexpected additional checkout")
		}
		fmt.Fprintf(w, `{"session_id":"test-session-%d","checkout_url":"https://provider.example/checkout/%d"}`, call, call)
	}))
	defer provider.Close()
	module := payment.NewModule(db, catalog.NewSpecRepository(db), nil, nil, nil, "https://waveyard.example",
		payment.GatewayConfig{
			CouponsEnabled: true,
			Dodo:           dodo.Config{APIURL: provider.URL, APIKey: "test", ProductID: "test"},
		})
	handler := module.HTTPHandler()
	quote := func(spec, license uuid.UUID, currency string) paymentdomain.CheckoutQuote {
		t.Helper()
		values := url.Values{"spec_id": {spec.String()}, "license_option_id": {license.String()}, "currency": {currency}}
		r := httptest.NewRequest(http.MethodGet, "/orders/quote?"+values.Encode(), nil).WithContext(ctx)
		w := httptest.NewRecorder()
		handler.GetCheckoutQuote(w, r)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		var result paymentdomain.CheckoutQuote
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		return result
	}
	post := func(body map[string]any, key string, user uuid.UUID) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		r := httptest.NewRequest(http.MethodPost, "/orders", bytes.NewReader(raw))
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		handler.CreateOrder(w, r.WithContext(context.WithValue(ctx, middleware.ContextKeyUserId, user)))
		return w
	}
	confirm := func(q paymentdomain.CheckoutQuote, key string) *httptest.ResponseRecorder {
		t.Helper()
		return post(map[string]any{
			"spec_id": q.SpecID, "license_option_id": q.LicenseOptionID, "currency": q.RequestedCurrency,
			"expected_price_id": q.Offer.PriceID, "expected_revision": q.Offer.Revision,
			"expected_amount_minor": q.Offer.AmountMinor, "expected_currency": q.Offer.Currency, "quote_id": q.ID,
		}, key, buyer)
	}
	readOrder := func(response *httptest.ResponseRecorder) paymentdomain.Order {
		t.Helper()
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var order paymentdomain.Order
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &order))
		return order
	}
	assertProviderAmount := func(want int) {
		t.Helper()
		select {
		case amount := <-providerAmounts:
			require.Equal(t, want, amount)
		default:
			t.Fatal("provider did not receive the expected checkout")
		}
	}

	promotions := application.NewService(postgres.NewRepository(db, catalog.LockPromotionOptions), time.Now)
	rules := domain.Rules{
		Name: "WAV sale", PercentBPS: 2500, Currencies: []string{"USD"},
		Enrollments: []domain.Enrollment{{BeatID: beat, OptionIDs: []uuid.UUID{option}}},
	}
	coupon, err := promotions.Create(ctx, producer, rules)
	require.NoError(t, err)
	beforePublish := quote(beat, option, "USD")
	require.Equal(t, 1000, beforePublish.PayableMinor)
	legacyKey := uuid.NewString()
	resume := map[string]any{"spec_id": beat, "license_option_id": option, "resume_only": true}
	resumeMissing := post(resume, legacyKey, buyer)
	require.Equal(t, http.StatusNotFound, resumeMissing.Code)
	require.Contains(t, resumeMissing.Body.String(), "CHECKOUT_NOT_FOUND")
	require.Equal(t, http.StatusBadRequest, post(resume, "", buyer).Code)
	require.Zero(t, providerCalls.Load())
	legacyEvidence := beforePublish
	legacyEvidence.ID = ""
	legacyOrder := readOrder(confirm(legacyEvidence, legacyKey))
	require.Equal(t, 1000, legacyOrder.Amount)
	assertProviderAmount(1000)
	catalogCoupons := NewCheckoutCouponReader(db)
	active, err := catalogCoupons.ReadActiveCoupons(ctx, []uuid.UUID{beat, otherBeat})
	require.NoError(t, err)
	require.Empty(t, active)
	coupon, err = promotions.Change(ctx, producer, coupon.ID, coupon.Revision, domain.Publish, nil)
	require.NoError(t, err)
	active, err = catalogCoupons.ReadActiveCoupons(ctx, []uuid.UUID{beat, otherBeat})
	require.NoError(t, err)
	require.Len(t, active[beat], 1)
	require.Empty(t, active[otherBeat])
	require.Zero(t, quote(beat, basic, "USD").DiscountMinor)
	require.Zero(t, quote(beat, option, "INR").DiscountMinor)
	require.Zero(t, quote(otherBeat, otherOption, "USD").DiscountMinor)

	key := uuid.NewString()
	stale := confirm(beforePublish, key)
	require.Equal(t, http.StatusConflict, stale.Code, stale.Body.String())
	require.Contains(t, stale.Body.String(), "PRICE_CHANGED")
	require.EqualValues(t, 1, providerCalls.Load())
	quoted := quote(beat, option, "USD")
	require.Equal(t, 250, quoted.DiscountMinor)
	require.Equal(t, 2500, quoted.DiscountPercentBPS)
	require.Equal(t, active[beat][0].Rules.PercentBPS, quoted.DiscountPercentBPS)
	require.Equal(t, 750, quoted.PayableMinor)
	require.Equal(t, quote(beat, option, "USD").ID, quoted.ID)
	require.NotNil(t, quoted.Coupon)
	require.Equal(t, coupon.ID, quoted.Coupon.ID)
	withoutEvidence := quoted
	withoutEvidence.ID = ""
	require.Equal(t, http.StatusConflict, confirm(withoutEvidence, key).Code)
	require.EqualValues(t, 1, providerCalls.Load())
	require.Equal(t, http.StatusConflict, confirm(quoted, legacyKey).Code)
	require.Equal(t, http.StatusNotFound, post(resume, legacyKey, producer).Code)
	wrongLicense := map[string]any{"spec_id": beat, "license_option_id": basic, "resume_only": true}
	require.Equal(t, http.StatusConflict, post(wrongLicense, legacyKey, buyer).Code)
	wrongSpec := map[string]any{"spec_id": otherBeat, "license_option_id": option, "resume_only": true}
	require.Equal(t, http.StatusConflict, post(wrongSpec, legacyKey, buyer).Code)
	resumed := readOrder(post(resume, legacyKey, buyer))
	require.Equal(t, legacyOrder.ID, resumed.ID)
	require.Equal(t, 1000, resumed.Amount)
	require.Equal(t, legacyOrder.CheckoutURL, resumed.CheckoutURL)
	require.EqualValues(t, 1, providerCalls.Load())
	response := confirm(quoted, key)
	order := readOrder(response)
	require.Equal(t, 750, order.Amount)
	assertProviderAmount(750)
	persisted, err := module.Service().GetOrder(ctx, order.ID, buyer)
	require.NoError(t, err)
	snapshot, ok := persisted.Notes["pricing_snapshot"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, coupon.ID.String(), snapshot["coupon_id"])
	require.EqualValues(t, 1000, snapshot["amount_minor"])
	require.EqualValues(t, 250, snapshot["discount_minor"])
	require.EqualValues(t, 750, snapshot["payable_minor"])

	_, err = promotions.Change(ctx, producer, coupon.ID, coupon.Revision, domain.Pause, nil)
	require.NoError(t, err)
	active, err = catalogCoupons.ReadActiveCoupons(ctx, []uuid.UUID{beat})
	require.NoError(t, err)
	require.Empty(t, active)
	afterPause := quote(beat, option, "USD")
	require.Equal(t, 1000, afterPause.PayableMinor)
	require.Equal(t, http.StatusConflict, confirm(quoted, uuid.NewString()).Code)
	require.Equal(t, http.StatusConflict, confirm(afterPause, key).Code)
	retry := confirm(quoted, key)
	require.Equal(t, http.StatusOK, retry.Code, retry.Body.String())
	require.Contains(t, retry.Body.String(), order.ID.String())
	require.EqualValues(t, 2, providerCalls.Load())

	tooLarge := rules
	tooLarge.PercentBPS = 0
	tooLarge.FixedMinor = map[string]int{"USD": 975}
	oversizedCoupon, err := promotions.Create(ctx, producer, tooLarge)
	require.NoError(t, err)
	_, err = promotions.Change(ctx, producer, oversizedCoupon.ID, oversizedCoupon.Revision, domain.Publish, nil)
	require.NoError(t, err)
	regularPrice := quote(beat, option, "USD")
	require.Equal(t, afterPause, regularPrice)
	baseOrder := readOrder(confirm(regularPrice, uuid.NewString()))
	require.Equal(t, 1000, baseOrder.Amount)
	assertProviderAmount(1000)
	require.EqualValues(t, 3, providerCalls.Load())
}
