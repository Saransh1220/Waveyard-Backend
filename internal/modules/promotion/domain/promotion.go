package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/shared/money"
)

var (
	ErrInvalid  = errors.New("invalid promotion")
	ErrNotFound = errors.New("promotion not found")
	ErrConflict = errors.New("promotion changed or action is unavailable")
)

const (
	MaxEnrollments = 100
	MaxOptions     = 32
	MaxFixedMinor  = 1<<31 - 1
)

type State string

const (
	Draft     State = "draft"
	Published State = "published"
	Paused    State = "paused"
	Archived  State = "archived"
)

type Action string

const (
	Create  Action = "create"
	Edit    Action = "edit"
	Publish Action = "publish"
	Pause   Action = "pause"
	Archive Action = "archive"
)

type Enrollment struct {
	BeatID    uuid.UUID   `json:"beat_id"`
	AllPaid   bool        `json:"all_paid"`
	OptionIDs []uuid.UUID `json:"option_ids,omitempty"`
}

type ChangeRequest struct {
	Revision int64  `json:"revision"`
	Action   Action `json:"action"`
	Rules    *Rules `json:"rules,omitempty"`
}

type Preview struct {
	Revision int64        `json:"revision"`
	Eligible []Enrollment `json:"eligible"`
}

type EnrollmentConflict struct {
	BeatID     uuid.UUID `json:"beat_id" db:"beat_id"`
	CouponID   uuid.UUID `json:"coupon_id" db:"coupon_id"`
	CouponName string    `json:"coupon_name" db:"coupon_name"`
}

type EnrollmentConflictError struct {
	Conflicts []EnrollmentConflict
}

func (e *EnrollmentConflictError) Error() string {
	return "One or more beats are already in a published coupon with overlapping dates."
}

type EnrollmentCheck struct {
	Conflicts          []EnrollmentConflict `json:"conflicts"`
	UnavailableBeatIDs []uuid.UUID          `json:"unavailable_beat_ids"`
}

type EnrollmentCheckRequest struct {
	BeatIDs []uuid.UUID `json:"beat_ids"`
}

type Rules struct {
	Name                string         `json:"name"`
	PercentBPS          int            `json:"percent_bps,omitempty"`
	FixedMinor          map[string]int `json:"fixed_minor,omitempty"`
	Currencies          []string       `json:"currencies"`
	StartsAt            *time.Time     `json:"starts_at,omitempty"`
	EndsAt              *time.Time     `json:"ends_at,omitempty"`
	Priority            int            `json:"priority"`
	AllowBundleStacking bool           `json:"allow_bundle_stacking"`
	Enrollments         []Enrollment   `json:"enrollments"`
}

type Promotion struct {
	ID                uuid.UUID `json:"id" db:"id"`
	ProducerID        uuid.UUID `json:"producer_id" db:"producer_id"`
	Revision          int64     `json:"revision" db:"revision"`
	PublishedRevision *int64    `json:"published_revision,omitempty" db:"published_revision"`
	State             State     `json:"state" db:"state"`
	Rules             Rules     `json:"rules" db:"-"`
	UpdatedAt         time.Time `json:"updated_at" db:"updated_at"`
}

func (r *Rules) Validate() error {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" || !utf8.ValidString(r.Name) || utf8.RuneCountInString(r.Name) > 100 || strings.ContainsFunc(r.Name, unicode.IsControl) {
		return fmt.Errorf("%w: name must contain 1–100 printable characters", ErrInvalid)
	}
	if r.Priority < 0 || r.Priority > 1000 {
		return fmt.Errorf("%w: priority must be between 0 and 1000", ErrInvalid)
	}
	if r.StartsAt != nil && r.EndsAt != nil && !r.StartsAt.Before(*r.EndsAt) {
		return fmt.Errorf("%w: start must be before end", ErrInvalid)
	}
	if len(r.Currencies) == 0 || len(r.Currencies) > len(money.SupportedCurrencies()) {
		return fmt.Errorf("%w: select supported currencies", ErrInvalid)
	}
	seenCurrencies := make(map[string]bool, len(r.Currencies))
	for _, code := range r.Currencies {
		def, ok := money.LookupCurrency(code)
		if !ok || def.Code != code || seenCurrencies[code] {
			return fmt.Errorf("%w: invalid or duplicate currency", ErrInvalid)
		}
		seenCurrencies[code] = true
	}
	if r.PercentBPS != 0 {
		if r.PercentBPS < 1 || r.PercentBPS >= 10000 || len(r.FixedMinor) != 0 {
			return fmt.Errorf("%w: coupon percentage must be greater than zero and below 100%%", ErrInvalid)
		}
	} else {
		if len(r.FixedMinor) != len(seenCurrencies) {
			return fmt.Errorf("%w: enter a fixed amount for each selected currency", ErrInvalid)
		}
		for code := range seenCurrencies {
			if amount := r.FixedMinor[code]; amount <= 0 || amount > MaxFixedMinor {
				return fmt.Errorf("%w: fixed amount for %s is out of range", ErrInvalid, code)
			}
		}
	}
	if len(r.Enrollments) > MaxEnrollments {
		return fmt.Errorf("%w: too many enrolled beats", ErrInvalid)
	}
	if r.Enrollments == nil {
		r.Enrollments = []Enrollment{}
	}
	seenBeats := make(map[uuid.UUID]bool, len(r.Enrollments))
	for _, enrollment := range r.Enrollments {
		if enrollment.BeatID == uuid.Nil || seenBeats[enrollment.BeatID] ||
			enrollment.AllPaid != (len(enrollment.OptionIDs) == 0) || len(enrollment.OptionIDs) > MaxOptions {
			return fmt.Errorf("%w: invalid beat or license enrollment", ErrInvalid)
		}
		seenBeats[enrollment.BeatID] = true
		seenOptions := make(map[uuid.UUID]bool, len(enrollment.OptionIDs))
		for _, id := range enrollment.OptionIDs {
			if id == uuid.Nil || seenOptions[id] {
				return fmt.Errorf("%w: invalid or duplicate license option", ErrInvalid)
			}
			seenOptions[id] = true
		}
	}
	return nil
}

// Change advances the draft revision. An edit keeps the previous published
// revision live until the producer publishes again, pauses, or archives.
func (p *Promotion) Change(action Action, rules *Rules, now time.Time) error {
	if p.State == Archived {
		return ErrConflict
	}
	switch action {
	case Edit:
		if rules == nil {
			return ErrInvalid
		}
		if err := rules.Validate(); err != nil {
			return err
		}
		p.Rules, p.State = *rules, Draft
	case Publish:
		if len(p.Rules.Enrollments) == 0 || (p.Rules.EndsAt != nil && !now.Before(*p.Rules.EndsAt)) {
			return fmt.Errorf("%w: publish requires enrolled beats and an unexpired schedule", ErrInvalid)
		}
		p.State = Published
		next := p.Revision + 1
		p.PublishedRevision = &next
	case Pause:
		if p.PublishedRevision == nil {
			return ErrConflict
		}
		p.State, p.PublishedRevision = Paused, nil
	case Archive:
		p.State, p.PublishedRevision = Archived, nil
	default:
		return ErrInvalid
	}
	p.Revision++
	p.UpdatedAt = now
	return nil
}
