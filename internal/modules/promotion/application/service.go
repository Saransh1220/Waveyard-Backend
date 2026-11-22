package application

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/promotion/domain"
)

type Repository interface {
	List(context.Context, uuid.UUID, int, int) ([]domain.Promotion, error)
	Get(context.Context, uuid.UUID, uuid.UUID) (domain.Promotion, error)
	Preview(context.Context, uuid.UUID, uuid.UUID) (domain.Preview, error)
	CheckEnrollment(context.Context, uuid.UUID, uuid.UUID, []uuid.UUID, time.Time) (domain.EnrollmentCheck, error)
	Create(context.Context, domain.Promotion) error
	Change(context.Context, uuid.UUID, uuid.UUID, int64, domain.Action, func(*domain.Promotion) error) (domain.Promotion, error)
}

type Service struct {
	repo Repository
	now  func() time.Time
}

func NewService(repo Repository, now func() time.Time) *Service {
	return &Service{repo: repo, now: now}
}

func (s *Service) List(ctx context.Context, producer uuid.UUID, limit, offset int) ([]domain.Promotion, error) {
	if producer == uuid.Nil || limit < 1 || limit > 100 || offset < 0 || offset > 10000 {
		return nil, domain.ErrInvalid
	}
	return s.repo.List(ctx, producer, limit, offset)
}

func (s *Service) Get(ctx context.Context, producer, id uuid.UUID) (domain.Promotion, error) {
	if producer == uuid.Nil || id == uuid.Nil {
		return domain.Promotion{}, domain.ErrInvalid
	}
	return s.repo.Get(ctx, producer, id)
}

func (s *Service) Preview(ctx context.Context, producer, id uuid.UUID) (domain.Preview, error) {
	if producer == uuid.Nil || id == uuid.Nil {
		return domain.Preview{}, domain.ErrInvalid
	}
	return s.repo.Preview(ctx, producer, id)
}

func (s *Service) CheckEnrollment(ctx context.Context, producer, id uuid.UUID, beats []uuid.UUID) (domain.EnrollmentCheck, error) {
	if producer == uuid.Nil || id == uuid.Nil || len(beats) > MaxEnrollmentCheck {
		return domain.EnrollmentCheck{}, domain.ErrInvalid
	}
	return s.repo.CheckEnrollment(ctx, producer, id, beats, s.now().UTC())
}

const MaxEnrollmentCheck = domain.MaxEnrollments + 50

func (s *Service) Create(ctx context.Context, producer uuid.UUID, rules domain.Rules) (domain.Promotion, error) {
	if producer == uuid.Nil {
		return domain.Promotion{}, domain.ErrInvalid
	}
	if err := rules.Validate(); err != nil {
		return domain.Promotion{}, err
	}
	promotion := domain.Promotion{ID: uuid.New(), ProducerID: producer, Revision: 1, State: domain.Draft, Rules: rules, UpdatedAt: s.now().UTC()}
	return promotion, s.repo.Create(ctx, promotion)
}

func (s *Service) Change(ctx context.Context, producer, id uuid.UUID, revision int64, action domain.Action, rules *domain.Rules) (domain.Promotion, error) {
	if producer == uuid.Nil || id == uuid.Nil || revision < 1 || (rules != nil) != (action == domain.Edit) {
		return domain.Promotion{}, domain.ErrInvalid
	}
	switch action {
	case domain.Edit, domain.Publish, domain.Pause, domain.Archive:
	default:
		return domain.Promotion{}, domain.ErrInvalid
	}
	return s.repo.Change(ctx, producer, id, revision, action, func(promotion *domain.Promotion) error {
		return promotion.Change(action, rules, s.now().UTC())
	})
}
