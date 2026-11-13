package user_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	authDomain "github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	fileApp "github.com/saransh1220/blueprint-audio/internal/modules/filestorage/application"
	"github.com/saransh1220/blueprint-audio/internal/modules/user"
	"github.com/stretchr/testify/assert"
)

type repoStub struct{}

func (r *repoStub) Create(ctx context.Context, user *authDomain.User) error { return nil }
func (r *repoStub) GetByEmail(ctx context.Context, email string) (*authDomain.User, error) {
	return nil, nil
}
func (r *repoStub) GetByID(ctx context.Context, id uuid.UUID) (*authDomain.User, error) {
	return &authDomain.User{ID: id}, nil
}
func (r *repoStub) MarkEmailVerified(ctx context.Context, id uuid.UUID) error { return nil }
func (r *repoStub) UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string) error {
	return nil
}
func (r *repoStub) UpdateProfile(ctx context.Context, id uuid.UUID, bio *string, avatarUrl *string, bannerURL *string, displayName *string, instagramURL, twitterURL, youtubeURL, spotifyURL *string, storeCurrency *string) error {
	return nil
}
func (r *repoStub) UpdateSystemRole(ctx context.Context, id uuid.UUID, role authDomain.SystemRole) error {
	return nil
}
func (r *repoStub) UpdateStatus(ctx context.Context, id uuid.UUID, status authDomain.UserStatus) error {
	return nil
}
func (r *repoStub) CountBySystemRole(ctx context.Context, role authDomain.SystemRole) (int, error) {
	return 0, nil
}
func (r *repoStub) BootstrapSuperAdmin(ctx context.Context, email string) error { return nil }
func (r *repoStub) SetPreferredCurrency(ctx context.Context, id uuid.UUID, currency *string) error {
	return nil
}
func (r *repoStub) GetPreferredCurrency(ctx context.Context, id uuid.UUID) (*string, error) {
	return nil, nil
}

func TestNewModule(t *testing.T) {
	var fs *fileApp.FileService
	m := user.NewModule(&repoStub{}, fs)
	assert.NotNil(t, m)
	assert.NotNil(t, m.Service())
	assert.NotNil(t, m.HTTPHandler())
	assert.Equal(t, m, m.WithSecureCookie(true))
}
