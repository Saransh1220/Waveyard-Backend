package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/payment/domain"
	promotion "github.com/saransh1220/blueprint-audio/internal/modules/promotion/domain"
	promotionpg "github.com/saransh1220/blueprint-audio/internal/modules/promotion/infrastructure/persistence/postgres"
	"github.com/saransh1220/blueprint-audio/internal/shared/money"
)

func (r *PgTxRunner) ReadQuote(ctx context.Context, specID, optionID uuid.UUID, currency string) (domain.CheckoutQuote, error) {
	db, err := r.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return domain.CheckoutQuote{}, err
	}
	defer db.Rollback()
	tx := &paymentTx{db: db, applyCoupons: r.applyCoupons}
	return tx.checkoutQuote(ctx, specID, optionID, currency, false)
}

func (tx *paymentTx) ReadQuote(ctx context.Context, specID, optionID uuid.UUID, currency string) (domain.CheckoutQuote, error) {
	return tx.checkoutQuote(ctx, specID, optionID, currency, true)
}

func (tx *paymentTx) checkoutQuote(ctx context.Context, specID, optionID uuid.UUID, currency string, reserve bool) (domain.CheckoutQuote, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		currency = money.CurrencyINR
	}
	if _, ok := money.LookupCurrency(currency); !ok {
		return domain.CheckoutQuote{}, money.ErrInvalidCurrency
	}

	// Promotions take a conflicting producer lock before writing published
	// revisions. Preview reads share a read-only snapshot and reserve no rows.
	ownerLock, listingLock, priceLock := "", "", ""
	if reserve {
		ownerLock = " FOR SHARE OF u"
		listingLock = " FOR SHARE OF s FOR UPDATE OF lo"
		priceLock = " FOR SHARE"
	}
	var producer uuid.UUID
	if err := tx.db.GetContext(ctx, &producer, `
		SELECT u.id FROM users u JOIN specs s ON s.producer_id = u.id
		WHERE s.id = $1`+ownerLock, specID); err != nil {
		return domain.CheckoutQuote{}, unavailableIfMissing(err)
	}
	var listing struct {
		Category       string `db:"category"`
		NativeCurrency string `db:"price_currency"`
	}
	if err := tx.db.GetContext(ctx, &listing, `
		SELECT s.category, lo.price_currency FROM specs s
		JOIN license_options lo ON lo.spec_id = s.id
		WHERE s.id = $1 AND lo.id = $2 AND s.producer_id = $3
			AND NOT s.is_deleted AND s.processing_status = 'completed' AND NOT lo.is_deleted`+listingLock,
		specID, optionID, producer); err != nil {
		return domain.CheckoutQuote{}, unavailableIfMissing(err)
	}

	var prices []money.StoredPrice
	if err := tx.db.SelectContext(ctx, &prices, `
		SELECT id, currency, amount_minor, revision, is_active FROM license_option_prices
		WHERE license_option_id = $1 ORDER BY currency`+priceLock, optionID); err != nil {
		return domain.CheckoutQuote{}, err
	}
	offer := money.SelectPriceOffer(prices, listing.NativeCurrency, currency)
	if !tx.applyCoupons || listing.Category != "beat" {
		return domain.NewCheckoutQuote(specID, optionID, currency, offer, nil, 0, 0)
	}
	published, err := promotionpg.ReadActiveCoupons(ctx, tx.db, []uuid.UUID{specID})
	if err != nil {
		return domain.CheckoutQuote{}, err
	}
	coupon, err := promotion.MatchCheckoutCoupon(published[specID], optionID, offer.Currency)
	if err != nil {
		return domain.CheckoutQuote{}, err
	}
	if coupon == nil {
		return domain.NewCheckoutQuote(specID, optionID, currency, offer, nil, 0, 0)
	}
	return domain.NewCheckoutQuote(specID, optionID, currency, offer,
		&domain.CheckoutCoupon{ID: coupon.ID, Revision: coupon.Revision}, coupon.Rules.PercentBPS, coupon.Rules.FixedMinor[offer.Currency])
}

func unavailableIfMissing(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return money.ErrPriceUnavailable
	}
	return err
}
