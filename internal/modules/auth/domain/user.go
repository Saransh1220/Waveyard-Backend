package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type UserRole string
type SystemRole string
type UserStatus string
type Currency string

const (
	RoleArtist   UserRole = "artist"
	RoleProducer UserRole = "producer"
)

const (
	SystemRoleUser       SystemRole = "user"
	SystemRoleSuperAdmin SystemRole = "super_admin"
)

const (
	UserStatusActive    UserStatus = "active"
	UserStatusSuspended UserStatus = "suspended"
)

const (
	CurrencyUSD Currency = "USD"
	CurrencyINR Currency = "INR"
)

func (c Currency) IsValid() bool {
	return c == CurrencyUSD || c == CurrencyINR
}

// User represents a user in the system
type User struct {
	ID                 uuid.UUID  `json:"id" db:"id"`
	Email              string     `json:"email" db:"email"`
	PasswordHash       string     `json:"-" db:"password_hash"`
	Name               string     `json:"name" db:"name"`
	Handle             string     `json:"handle" db:"handle"`
	DisplayName        *string    `json:"display_name" db:"display_name"`
	Role               UserRole   `json:"role" db:"role"`
	SystemRole         SystemRole `json:"system_role" db:"system_role"`
	Status             UserStatus `json:"status" db:"status"`
	EmailVerified      bool       `json:"email_verified" db:"email_verified"`
	EmailVerifiedAt    *time.Time `json:"email_verified_at,omitempty" db:"email_verified_at"`
	Bio                *string    `json:"bio" db:"bio"`
	AvatarUrl          *string    `json:"avatar_url" db:"avatar_url"`
	BannerURL          *string    `json:"banner_url" db:"banner_url"`
	InstagramURL       *string    `json:"instagram_url" db:"instagram_url"`
	TwitterURL         *string    `json:"twitter_url" db:"twitter_url"`
	YoutubeURL         *string    `json:"youtube_url" db:"youtube_url"`
	SpotifyURL         *string    `json:"spotify_url" db:"spotify_url"`
	StoreCurrency      Currency   `json:"store_currency" db:"store_currency"`
	PreferredCurrency  *string    `json:"preferred_currency,omitempty" db:"preferred_currency"`
	RegistrationSource string     `json:"registration_source" db:"registration_source"` // "waveyard" | "sonically" | "blueprint" | "google"
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at" db:"updated_at"`
}

// UserRepository defines the contract for user data access
type UserRepository interface {
	Create(ctx context.Context, user *User) error
	GetByEmail(ctx context.Context, email string) (*User, error)
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	MarkEmailVerified(ctx context.Context, id uuid.UUID) error
	UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string) error
	UpdateProfile(ctx context.Context, id uuid.UUID, bio *string, avatarUrl *string, bannerURL *string, displayName *string, instagramURL, twitterURL, youtubeURL, spotifyURL *string, storeCurrency *string) error
	UpdateSystemRole(ctx context.Context, id uuid.UUID, role SystemRole) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status UserStatus) error
	CountBySystemRole(ctx context.Context, role SystemRole) (int, error)
	BootstrapSuperAdmin(ctx context.Context, email string) error
	SetPreferredCurrency(ctx context.Context, id uuid.UUID, currency *string) error
	GetPreferredCurrency(ctx context.Context, id uuid.UUID) (*string, error)
}

// UserFinder provides user lookup capabilities for other modules
// This is the interface exposed to other modules (Payment, Analytics)
type UserFinder interface {
	FindByID(ctx context.Context, id uuid.UUID) (*User, error)
	Exists(ctx context.Context, id uuid.UUID) (bool, error)
}
