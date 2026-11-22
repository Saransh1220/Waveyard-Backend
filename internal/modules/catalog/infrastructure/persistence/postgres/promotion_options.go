package postgres

import (
	"context"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// LockPromotionOptions returns the active paid options on a producer's beat.
// Holding share locks through enrollment prevents an ownership change or option
// deletion from crossing the enrollment transaction.
func LockPromotionOptions(ctx context.Context, tx *sqlx.Tx, owner, beat uuid.UUID, currencies []string) ([]uuid.UUID, error) {
	var options []uuid.UUID
	err := tx.SelectContext(ctx, &options, `
		SELECT lo.id
		FROM specs s JOIN license_options lo ON lo.spec_id = s.id
		WHERE s.id = $1 AND s.producer_id = $2 AND s.category = 'beat' AND NOT s.is_deleted
			AND s.processing_status = 'completed' AND NOT lo.is_deleted
			AND EXISTS (
				SELECT 1 FROM license_option_prices p
				WHERE p.license_option_id = lo.id AND p.is_active
					AND p.amount_minor > 0 AND p.currency = ANY($3)
			)
		ORDER BY lo.id
		FOR SHARE OF s, lo`, beat, owner, pq.Array(currencies))
	return options, err
}
