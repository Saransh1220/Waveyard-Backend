package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
)

type PgUserRepository struct {
	db *sqlx.DB
}

// NewUserRepository creates and returns a new PostgreSQL-based user repository.
// It takes a database connection and initializes a PgUserRepository instance
// that implements the domain.UserRepository interface.
func NewUserRepository(db *sqlx.DB) *PgUserRepository {
	return &PgUserRepository{db: db}
}

// CreateUser inserts a new user record into the database.
// It takes a context and a pointer to a domain.User struct.
// If the user's CreatedAt or UpdatedAt timestamps are zero values,
// they are automatically set to the current time before insertion.
// Returns an error if the database operation fails.
// Create implements domain.UserRepository
func (r *PgUserRepository) Create(ctx context.Context, user *domain.User) error {
	query := `INSERT INTO users (id, email, password_hash, name, display_name, role, system_role, status, email_verified, email_verified_at, store_currency, registration_source, created_at, updated_at) VALUES (:id, :email, :password_hash, :name, :display_name, :role, :system_role, :status, :email_verified, :email_verified_at, :store_currency, :registration_source, :created_at, :updated_at)`

	if user.CreatedAt.IsZero() {
		user.CreatedAt = time.Now()
	}
	if user.UpdatedAt.IsZero() {
		user.UpdatedAt = time.Now()
	}
	if user.SystemRole == "" {
		user.SystemRole = domain.SystemRoleUser
	}
	if user.Status == "" {
		user.Status = domain.UserStatusActive
	}
	if user.StoreCurrency == "" {
		user.StoreCurrency = domain.CurrencyUSD
	}
	if !user.StoreCurrency.IsValid() {
		return domain.ErrInvalidStoreCurrency
	}

	// NamedExecContext is a sqlx feature! It uses the structure fields directly.
	_, err := r.db.NamedExecContext(ctx, query, user)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok {
			if pqErr.Code == "23505" { // Unique violation
				return domain.ErrUserAlreadyExists
			}
		}
		return err
	}
	return nil
}

// GetUserByEmail retrieves a user from the database by their email address.
// It returns a pointer to the User domain object if found, or nil if no user exists with the given email.
// Returns an error if the database query fails for any reason other than a missing record.
// GetByEmail implements domain.UserRepository
func (r *PgUserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	user := &domain.User{}
	query := `SELECT * FROM users WHERE email = $1`

	err := r.db.GetContext(ctx, user, query, email)
	if err == sql.ErrNoRows {
		return nil, domain.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return user, nil

}

// GetUserById retrieves a user from the database by their unique identifier.
// It queries the users table for a user matching the provided UUID.
// Returns the user if found, nil if no user exists with the given ID,
// or an error if the database query fails.
// GetByID implements domain.UserRepository
func (r *PgUserRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	user := &domain.User{}
	query := `SELECT * FROM users WHERE id = $1`
	err := r.db.GetContext(ctx, user, query, id)
	if err == sql.ErrNoRows {
		return nil, domain.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (r *PgUserRepository) MarkEmailVerified(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE users SET email_verified = true, email_verified_at = NOW(), updated_at = NOW() WHERE id = $1`
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

func (r *PgUserRepository) UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string) error {
	query := `UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2`
	result, err := r.db.ExecContext(ctx, query, passwordHash, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

func (r *PgUserRepository) UpdateSystemRole(ctx context.Context, id uuid.UUID, role domain.SystemRole) error {
	query := `UPDATE users SET system_role = $1, updated_at = NOW() WHERE id = $2`
	result, err := r.db.ExecContext(ctx, query, role, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

func (r *PgUserRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.UserStatus) error {
	query := `UPDATE users SET status = $1, updated_at = NOW() WHERE id = $2`
	result, err := r.db.ExecContext(ctx, query, status, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

func (r *PgUserRepository) CountBySystemRole(ctx context.Context, role domain.SystemRole) (int, error) {
	var count int
	err := r.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM users WHERE system_role = $1`, role)
	return count, err
}

func (r *PgUserRepository) BootstrapSuperAdmin(ctx context.Context, email string) error {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return nil
	}
	result, err := r.db.ExecContext(ctx, `UPDATE users SET system_role = $1, updated_at = NOW() WHERE LOWER(email) = $2`, domain.SystemRoleSuperAdmin, email)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("no user found for email %s", email)
	}
	return nil
}

// FindByID implements domain.UserFinder for exposing to other modules
func (r *PgUserRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	return r.GetByID(ctx, id)
}

// Exists implements domain.UserFinder
func (r *PgUserRepository) Exists(ctx context.Context, id uuid.UUID) (bool, error) {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)`
	err := r.db.GetContext(ctx, &exists, query, id)
	return exists, err
}

// UpdateProfile updates a user's profile fields (bio, avatar, and social media URLs).
// Only the provided non-nil fields will be updated in the database.
// Returns an error if the database operation fails.
func (r *PgUserRepository) UpdateProfile(ctx context.Context, id uuid.UUID, bio *string, avatarUrl *string, bannerURL *string, displayName *string, instagramURL, twitterURL, youtubeURL, spotifyURL *string, storeCurrency *string) error {
	// Build dynamic query to only update provided fields
	setClauses := []string{}
	args := []interface{}{}
	argIndex := 1

	if bio != nil {
		setClauses = append(setClauses, fmt.Sprintf("bio = $%d", argIndex))
		args = append(args, bio)
		argIndex++
	}
	if displayName != nil {
		setClauses = append(setClauses, fmt.Sprintf("display_name = $%d", argIndex))
		args = append(args, displayName)
		argIndex++
	}
	if avatarUrl != nil {
		setClauses = append(setClauses, fmt.Sprintf("avatar_url = $%d", argIndex))
		args = append(args, avatarUrl)
		argIndex++
	}
	if bannerURL != nil {
		setClauses = append(setClauses, fmt.Sprintf("banner_url = $%d", argIndex))
		args = append(args, bannerURL)
		argIndex++
	}
	if storeCurrency != nil {
		currency := domain.Currency(strings.ToUpper(strings.TrimSpace(*storeCurrency)))
		if !currency.IsValid() {
			return domain.ErrInvalidStoreCurrency
		}
		setClauses = append(setClauses, fmt.Sprintf("store_currency = $%d", argIndex))
		args = append(args, string(currency))
		argIndex++
	}
	if instagramURL != nil {
		setClauses = append(setClauses, fmt.Sprintf("instagram_url = $%d", argIndex))
		args = append(args, instagramURL)
		argIndex++
	}
	if twitterURL != nil {
		setClauses = append(setClauses, fmt.Sprintf("twitter_url = $%d", argIndex))
		args = append(args, twitterURL)
		argIndex++
	}
	if youtubeURL != nil {
		setClauses = append(setClauses, fmt.Sprintf("youtube_url = $%d", argIndex))
		args = append(args, youtubeURL)
		argIndex++
	}
	if spotifyURL != nil {
		setClauses = append(setClauses, fmt.Sprintf("spotify_url = $%d", argIndex))
		args = append(args, spotifyURL)
		argIndex++
	}

	// Always update updated_at
	setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", argIndex))
	args = append(args, time.Now())
	argIndex++

	// Add WHERE clause with user ID
	args = append(args, id)

	// If no fields to update, return early
	if len(setClauses) == 1 { // Only updated_at
		return nil
	}

	query := fmt.Sprintf("UPDATE users SET %s WHERE id = $%d",
		strings.Join(setClauses, ", "),
		argIndex)

	_, err := r.db.ExecContext(ctx, query, args...)
	return err
}

// GetByHandle resolves a public handle. The unique handle wins; the legacy login name is
// only consulted when no handle matches, so links shared before handles existed keep working.
func (r *PgUserRepository) GetByHandle(ctx context.Context, handle string) (*domain.User, error) {
	var users []domain.User
	err := r.db.SelectContext(ctx, &users, `SELECT * FROM users WHERE lower(handle) = lower($1) AND status = 'active' LIMIT 1`, handle)
	if err != nil {
		return nil, err
	}
	if len(users) == 1 {
		return &users[0], nil
	}
	err = r.db.SelectContext(ctx, &users, `SELECT * FROM users WHERE lower(name) = lower($1) AND status = 'active' LIMIT 2`, handle)
	if err != nil {
		return nil, err
	}
	if len(users) != 1 {
		return nil, domain.ErrUserNotFound
	}
	return &users[0], nil
}

// UpdateHandle changes a user's public handle. The caller must have validated the format.
func (r *PgUserRepository) UpdateHandle(ctx context.Context, id uuid.UUID, handle string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE users SET handle = $1, updated_at = NOW() WHERE id = $2`, handle, id)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return domain.ErrHandleTaken
		}
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

// SetPreferredCurrency updates the user's preferred display currency (NULL for automatic).
func (r *PgUserRepository) SetPreferredCurrency(ctx context.Context, id uuid.UUID, currency *string) error {
	query := `UPDATE users SET preferred_currency = $1, updated_at = NOW() WHERE id = $2`
	result, err := r.db.ExecContext(ctx, query, currency, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

// GetPreferredCurrency retrieves the user's preferred display currency.
func (r *PgUserRepository) GetPreferredCurrency(ctx context.Context, id uuid.UUID) (*string, error) {
	var currency *string
	query := `SELECT preferred_currency FROM users WHERE id = $1`
	err := r.db.GetContext(ctx, &currency, query, id)
	if err == sql.ErrNoRows {
		return nil, domain.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return currency, nil
}
