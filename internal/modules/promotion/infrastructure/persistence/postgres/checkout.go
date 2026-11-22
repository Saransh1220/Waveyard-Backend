package postgres

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/saransh1220/blueprint-audio/internal/modules/promotion/domain"
)

type CheckoutCouponReader struct {
	db *sqlx.DB
}

func NewCheckoutCouponReader(db *sqlx.DB) *CheckoutCouponReader {
	return &CheckoutCouponReader{db: db}
}

func (r *CheckoutCouponReader) ReadActiveCoupons(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]domain.CheckoutCoupon, error) {
	return ReadActiveCoupons(ctx, r.db, ids)
}

// ReadActiveCoupons also accepts the payment transaction, where the producer is locked.
func ReadActiveCoupons(ctx context.Context, db sqlx.QueryerContext, ids []uuid.UUID) (map[uuid.UUID][]domain.CheckoutCoupon, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []struct {
		BeatID     uuid.UUID `db:"beat_id"`
		ID         uuid.UUID `db:"id"`
		Revision   int64     `db:"revision"`
		Rules      []byte    `db:"rules"`
		Enrollment []byte    `db:"enrollment"`
	}
	err := sqlx.SelectContext(ctx, db, &rows, `
		SELECT s.id AS beat_id, p.id, r.revision,
			r.rules - 'enrollments' AS rules, e.value AS enrollment
		FROM specs s
		JOIN producer_promotions p ON p.producer_id = s.producer_id
		JOIN producer_promotion_revisions r ON r.promotion_id = p.id AND r.revision = p.published_revision
			AND (r.rules->>'starts_at' IS NULL OR (r.rules->>'starts_at')::timestamptz <= statement_timestamp())
			AND (r.rules->>'ends_at' IS NULL OR (r.rules->>'ends_at')::timestamptz > statement_timestamp())
			AND r.rules->'enrollments' @> jsonb_build_array(jsonb_build_object('beat_id', s.id))
		CROSS JOIN LATERAL jsonb_array_elements(r.rules->'enrollments') AS e(value)
		WHERE s.id = ANY($1) AND s.category = 'beat'
			AND NOT s.is_deleted AND s.processing_status = 'completed'
			AND e.value->>'beat_id' = s.id::text`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID][]domain.CheckoutCoupon)
	for _, row := range rows {
		coupon := domain.CheckoutCoupon{ID: row.ID, Revision: row.Revision}
		if err := json.Unmarshal(row.Rules, &coupon.Rules); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(row.Enrollment, &coupon.Enrollment); err != nil {
			return nil, err
		}
		result[row.BeatID] = append(result[row.BeatID], coupon)
	}
	return result, nil
}
