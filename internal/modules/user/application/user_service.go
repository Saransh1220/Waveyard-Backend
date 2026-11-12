package application

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	authDomain "github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	"github.com/saransh1220/blueprint-audio/internal/shared/money"
)

type UserService struct {
	repo authDomain.UserRepository
}

func NewUserService(repo authDomain.UserRepository) *UserService {
	return &UserService{repo: repo}
}

// UpdateProfile updates a user's profile information
func (s *UserService) UpdateProfile(ctx context.Context, userID uuid.UUID, req UpdateProfileRequest) error {
	if err := validateProfile(req); err != nil {
		return err
	}
	if req.StoreCurrency != nil {
		currency := authDomain.Currency(strings.ToUpper(strings.TrimSpace(*req.StoreCurrency)))
		if !currency.IsValid() {
			return authDomain.ErrInvalidStoreCurrency
		}
		normalized := string(currency)
		req.StoreCurrency = &normalized
	}

	if req.Handle != nil {
		handle := authDomain.NormalizeHandle(*req.Handle)
		if err := authDomain.ValidateHandle(handle); err != nil {
			return err
		}
		updater, ok := s.repo.(interface {
			UpdateHandle(context.Context, uuid.UUID, string) error
		})
		if !ok {
			return fmt.Errorf("handle changes are not supported")
		}
		if err := updater.UpdateHandle(ctx, userID, handle); err != nil {
			return err
		}
	}

	return s.repo.UpdateProfile(
		ctx,
		userID,
		req.Bio,
		req.AvatarURL,
		req.BannerURL,
		req.DisplayName,
		req.InstagramURL,
		req.TwitterURL,
		req.YoutubeURL,
		req.SpotifyURL,
		req.StoreCurrency,
	)
}

// GetPublicProfile retrieves a user's public profile information
func (s *UserService) GetPublicProfile(ctx context.Context, userID uuid.UUID) (*PublicUserResponse, error) {
	user, err := s.repo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}

	// Default empty StoreCurrency to USD for UI
	storeCurrency := string(user.StoreCurrency)
	if storeCurrency == "" {
		storeCurrency = string(authDomain.CurrencyUSD)
	}

	return &PublicUserResponse{
		ID:            user.ID.String(),
		Name:          user.Name,
		Handle:        user.Handle,
		DisplayName:   user.DisplayName,
		Role:          string(user.Role),
		Bio:           user.Bio,
		AvatarURL:     user.AvatarUrl,
		BannerURL:     user.BannerURL,
		InstagramURL:  user.InstagramURL,
		TwitterURL:    user.TwitterURL,
		YoutubeURL:    user.YoutubeURL,
		SpotifyURL:    user.SpotifyURL,
		StoreCurrency: storeCurrency,
		CreatedAt:     user.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}, nil
}

func validateProfile(req UpdateProfileRequest) error {
	if req.DisplayName != nil && len([]rune(strings.TrimSpace(*req.DisplayName))) > 50 {
		return fmt.Errorf("display name must be at most 50 characters")
	}
	if req.Bio != nil && len([]rune(*req.Bio)) > 500 {
		return fmt.Errorf("bio must be at most 500 characters")
	}
	for _, value := range []*string{req.InstagramURL, req.TwitterURL, req.YoutubeURL, req.SpotifyURL} {
		if value == nil || *value == "" {
			continue
		}
		if len(*value) > 255 {
			return fmt.Errorf("social url is too long")
		}
		u, err := url.ParseRequestURI(*value)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("social urls must be valid http(s) urls")
		}
	}
	return nil
}

// SetPreferredCurrency updates the user's preferred display currency (or resets to Automatic if nil/empty).
func (s *UserService) SetPreferredCurrency(ctx context.Context, userID uuid.UUID, currency *string) error {
	if currency == nil || strings.TrimSpace(*currency) == "" {
		return s.repo.SetPreferredCurrency(ctx, userID, nil)
	}
	def, ok := money.LookupCurrency(*currency)
	if !ok {
		return fmt.Errorf("unsupported currency: %s", *currency)
	}
	code := def.Code
	return s.repo.SetPreferredCurrency(ctx, userID, &code)
}

// GetPreferredCurrency retrieves the user's preferred display currency.
func (s *UserService) GetPreferredCurrency(ctx context.Context, userID uuid.UUID) (*string, error) {
	return s.repo.GetPreferredCurrency(ctx, userID)
}

// GetPublicProfileByHandle depends only on the optional public-directory capability.
func (s *UserService) GetPublicProfileByHandle(ctx context.Context, handle string) (*PublicUserResponse, error) {
	finder, ok := s.repo.(interface {
		GetByHandle(context.Context, string) (*authDomain.User, error)
	})
	if !ok {
		return nil, authDomain.ErrUserNotFound
	}
	user, err := finder.GetByHandle(ctx, authDomain.NormalizeHandle(handle))
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, authDomain.ErrUserNotFound
	}
	return s.GetPublicProfile(ctx, user.ID)
}
