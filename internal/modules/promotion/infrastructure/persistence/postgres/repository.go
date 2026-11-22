package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/saransh1220/blueprint-audio/internal/modules/promotion/domain"
)

type OptionLookup func(context.Context, *sqlx.Tx, uuid.UUID, uuid.UUID, []string) ([]uuid.UUID, error)

type Repository struct {
	db      *sqlx.DB
	options OptionLookup
}

func NewRepository(db *sqlx.DB, options OptionLookup) *Repository {
	return &Repository{db: db, options: options}
}

type promotionRow struct {
	domain.Promotion
	RulesJSON []byte `db:"rules"`
}

func (row promotionRow) decode() (domain.Promotion, error) {
	promotion := row.Promotion
	err := json.Unmarshal(row.RulesJSON, &promotion.Rules)
	return promotion, err
}

const selectPromotion = `
	SELECT p.id, p.producer_id, p.revision, p.published_revision, p.updated_at,
		r.state, r.rules
	FROM producer_promotions p JOIN producer_promotion_revisions r
		ON r.promotion_id = p.id AND r.revision = p.revision
	WHERE p.producer_id = $1`

func (r *Repository) List(ctx context.Context, producer uuid.UUID, limit, offset int) ([]domain.Promotion, error) {
	var rows []promotionRow
	if err := r.db.SelectContext(ctx, &rows, selectPromotion+" ORDER BY p.updated_at DESC, p.id DESC LIMIT $2 OFFSET $3", producer, limit, offset); err != nil {
		return nil, err
	}
	promotions := make([]domain.Promotion, 0, len(rows))
	for _, row := range rows {
		promotion, err := row.decode()
		if err != nil {
			return nil, err
		}
		promotions = append(promotions, promotion)
	}
	return promotions, nil
}

func (r *Repository) Get(ctx context.Context, producer, id uuid.UUID) (domain.Promotion, error) {
	return readPromotion(ctx, r.db, selectPromotion+" AND p.id = $2", producer, id)
}

func (r *Repository) CheckEnrollment(ctx context.Context, producer, id uuid.UUID, beats []uuid.UUID, now time.Time) (domain.EnrollmentCheck, error) {
	promotion, err := r.Get(ctx, producer, id)
	if err != nil {
		return domain.EnrollmentCheck{}, err
	}
	conflicts, err := findConflicts(ctx, r.db, promotion, beats, now)
	if err != nil {
		return domain.EnrollmentCheck{}, err
	}
	unavailable := make([]uuid.UUID, 0)
	err = r.db.SelectContext(ctx, &unavailable, `
		SELECT DISTINCT requested.id
		FROM unnest($2::uuid[]) AS requested(id)
		WHERE NOT EXISTS (
			SELECT 1 FROM specs s WHERE s.id = requested.id AND s.producer_id = $1
				AND s.category = 'beat' AND NOT s.is_deleted AND s.processing_status = 'completed'
		)`, producer, pq.Array(beats))
	return domain.EnrollmentCheck{Conflicts: conflicts, UnavailableBeatIDs: unavailable}, err
}

func findConflicts(ctx context.Context, db sqlx.QueryerContext, p domain.Promotion, beats []uuid.UUID, now time.Time) ([]domain.EnrollmentConflict, error) {
	conflicts := make([]domain.EnrollmentConflict, 0)
	if len(beats) == 0 || (p.Rules.EndsAt != nil && !now.Before(*p.Rules.EndsAt)) {
		return conflicts, nil
	}
	// Compare half-open windows from now onward against the published revision,
	// which can remain live while its producer edits a separate draft.
	err := sqlx.SelectContext(ctx, db, &conflicts, `
		SELECT DISTINCT ON (enrollment.beat_id) enrollment.beat_id, p.id AS coupon_id,
			r.rules->>'name' AS coupon_name
		FROM producer_promotions p
		JOIN producer_promotion_revisions r
			ON r.promotion_id = p.id AND r.revision = p.published_revision
		CROSS JOIN LATERAL jsonb_to_recordset(COALESCE(NULLIF(r.rules->'enrollments', 'null'), '[]'))
			AS enrollment(beat_id uuid)
		WHERE p.producer_id = $1 AND p.id <> $2 AND enrollment.beat_id = ANY($3::uuid[])
			AND COALESCE((r.rules->>'ends_at')::timestamptz, 'infinity') > GREATEST($4::timestamptz, $5::timestamptz)
			AND COALESCE($6::timestamptz, 'infinity') > COALESCE((r.rules->>'starts_at')::timestamptz, '-infinity')
		ORDER BY enrollment.beat_id, p.id`, p.ProducerID, p.ID, pq.Array(beats), now, p.Rules.StartsAt, p.Rules.EndsAt)
	return conflicts, err
}

func lockProducer(ctx context.Context, tx *sqlx.Tx, producer uuid.UUID) error {
	// Every promotion write takes this lock before an individual promotion
	// lock. Concurrent publications on different coupons therefore serialize.
	var id uuid.UUID
	return tx.GetContext(ctx, &id, `SELECT id FROM users WHERE id = $1 FOR NO KEY UPDATE`, producer)
}

func (r *Repository) Preview(ctx context.Context, producer, id uuid.UUID) (domain.Preview, error) {
	var preview domain.Preview
	err := r.withTx(ctx, func(tx *sqlx.Tx) error {
		promotion, err := readPromotion(ctx, tx, selectPromotion+" AND p.id = $2", producer, id)
		if err != nil {
			return err
		}
		preview.Revision = promotion.Revision
		preview.Eligible, err = r.resolveEnrollments(ctx, tx, promotion)
		return err
	})
	return preview, err
}

func readPromotion(ctx context.Context, db sqlx.QueryerContext, query string, producer, id uuid.UUID) (domain.Promotion, error) {
	var row promotionRow
	if err := sqlx.GetContext(ctx, db, &row, query, producer, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = domain.ErrNotFound
		}
		return domain.Promotion{}, err
	}
	return row.decode()
}

func (r *Repository) Create(ctx context.Context, promotion domain.Promotion) error {
	return r.withTx(ctx, func(tx *sqlx.Tx) error {
		if err := lockProducer(ctx, tx, promotion.ProducerID); err != nil {
			return err
		}
		if err := r.validateEnrollments(ctx, tx, promotion); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO producer_promotions (id, producer_id, revision, updated_at)
			VALUES ($1, $2, $3, $4)`, promotion.ID, promotion.ProducerID, promotion.Revision, promotion.UpdatedAt)
		if err != nil {
			return err
		}
		return insertRevision(ctx, tx, promotion, domain.Create)
	})
}

func (r *Repository) Change(ctx context.Context, producer, id uuid.UUID, revision int64, action domain.Action, mutate func(*domain.Promotion) error) (domain.Promotion, error) {
	var changed domain.Promotion
	err := r.withTx(ctx, func(tx *sqlx.Tx) error {
		if err := lockProducer(ctx, tx, producer); err != nil {
			return err
		}
		// Lock the head before reading the revision. Under READ COMMITTED the
		// following query sees the newly committed revision after waiting.
		var currentRevision int64
		err := tx.GetContext(ctx, &currentRevision, `
			SELECT revision FROM producer_promotions WHERE producer_id = $1 AND id = $2 FOR UPDATE`, producer, id)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return err
		}
		if currentRevision != revision {
			return domain.ErrConflict
		}
		changed, err = readPromotion(ctx, tx, selectPromotion+" AND p.id = $2", producer, id)
		if err != nil {
			return err
		}
		if err := mutate(&changed); err != nil {
			return err
		}
		if action == domain.Edit || action == domain.Publish {
			if err := r.validateEnrollments(ctx, tx, changed); err != nil {
				return err
			}
		}
		if err := insertRevision(ctx, tx, changed, action); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE producer_promotions SET revision = $3, published_revision = $4, updated_at = $5
			WHERE producer_id = $1 AND id = $2`, producer, id, changed.Revision, changed.PublishedRevision, changed.UpdatedAt)
		return err
	})
	return changed, err
}

func insertRevision(ctx context.Context, tx *sqlx.Tx, promotion domain.Promotion, action domain.Action) error {
	rules, err := json.Marshal(promotion.Rules)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO producer_promotion_revisions (promotion_id, revision, actor_id, action, state, rules, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, promotion.ID, promotion.Revision, promotion.ProducerID, action, promotion.State, string(rules), promotion.UpdatedAt)
	return err
}

func (r *Repository) validateEnrollments(ctx context.Context, tx *sqlx.Tx, promotion domain.Promotion) error {
	beats := make([]uuid.UUID, 0, len(promotion.Rules.Enrollments))
	for _, enrollment := range promotion.Rules.Enrollments {
		beats = append(beats, enrollment.BeatID)
	}
	conflicts, err := findConflicts(ctx, tx, promotion, beats, promotion.UpdatedAt)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		return &domain.EnrollmentConflictError{Conflicts: conflicts}
	}
	_, err = r.resolveEnrollments(ctx, tx, promotion)
	return err
}

func (r *Repository) resolveEnrollments(ctx context.Context, tx *sqlx.Tx, promotion domain.Promotion) ([]domain.Enrollment, error) {
	enrollments := slices.Clone(promotion.Rules.Enrollments)
	resolved := make([]domain.Enrollment, 0, len(enrollments))
	slices.SortFunc(enrollments, func(a, b domain.Enrollment) int {
		return strings.Compare(a.BeatID.String(), b.BeatID.String())
	})
	for _, enrollment := range enrollments {
		options, err := r.options(ctx, tx, promotion.ProducerID, enrollment.BeatID, promotion.Rules.Currencies)
		if err != nil {
			return nil, err
		}
		if len(options) == 0 {
			return nil, fmt.Errorf("%w: enrolled beat is unavailable", domain.ErrInvalid)
		}
		if !enrollment.AllPaid {
			for _, option := range enrollment.OptionIDs {
				if !slices.Contains(options, option) {
					return nil, fmt.Errorf("%w: enrolled license option is unavailable", domain.ErrInvalid)
				}
			}
			options = enrollment.OptionIDs
		}
		resolved = append(resolved, domain.Enrollment{BeatID: enrollment.BeatID, OptionIDs: options})
	}
	return resolved, nil
}

func (r *Repository) withTx(ctx context.Context, operation func(*sqlx.Tx) error) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := operation(tx); err != nil {
		return err
	}
	return tx.Commit()
}
