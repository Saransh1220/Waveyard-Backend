package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	authDomain "github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/user/application"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// handleRepo adds the optional handle capabilities the service looks for.
type handleRepo struct {
	*mockUserRepo
	updated   string
	updateErr error
	byHandle  *authDomain.User
	findErr   error
}

func (r *handleRepo) UpdateHandle(_ context.Context, _ uuid.UUID, handle string) error {
	r.updated = handle
	return r.updateErr
}

func (r *handleRepo) GetByHandle(_ context.Context, _ string) (*authDomain.User, error) {
	return r.byHandle, r.findErr
}

func TestUserServiceUpdateProfileChangesHandle(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()
	base := new(mockUserRepo)
	repo := &handleRepo{mockUserRepo: base}
	svc := application.NewUserService(repo)

	handle := "  @Blaze "
	req := application.UpdateProfileRequest{Handle: &handle}
	base.On("UpdateProfile", ctx, id, req.Bio, req.AvatarURL, req.DisplayName, req.InstagramURL, req.TwitterURL, req.YoutubeURL, req.SpotifyURL, req.StoreCurrency).Return(nil).Once()

	require.NoError(t, svc.UpdateProfile(ctx, id, req))
	assert.Equal(t, "blaze", repo.updated, "handle is normalised before it is stored")
}

func TestUserServiceUpdateProfileRejectsBadOrTakenHandles(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()
	base := new(mockUserRepo)
	repo := &handleRepo{mockUserRepo: base, updateErr: authDomain.ErrHandleTaken}
	svc := application.NewUserService(repo)

	bad := "no spaces!"
	err := svc.UpdateProfile(ctx, id, application.UpdateProfileRequest{Handle: &bad})
	assert.ErrorIs(t, err, authDomain.ErrInvalidHandle)
	assert.Empty(t, repo.updated, "an invalid handle never reaches the repository")

	taken := "blaze"
	err = svc.UpdateProfile(ctx, id, application.UpdateProfileRequest{Handle: &taken})
	assert.ErrorIs(t, err, authDomain.ErrHandleTaken)
}

func TestUserServiceUpdateProfileHandleNeedsRepositorySupport(t *testing.T) {
	svc := application.NewUserService(new(mockUserRepo))
	handle := "blaze"
	err := svc.UpdateProfile(context.Background(), uuid.New(), application.UpdateProfileRequest{Handle: &handle})
	assert.Error(t, err)
}

func TestUserServicePublicProfileByHandle(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()
	base := new(mockUserRepo)
	user := &authDomain.User{ID: id, Name: "saransh", Handle: "blaze", Role: authDomain.RoleProducer}
	repo := &handleRepo{mockUserRepo: base, byHandle: user}
	base.On("GetByID", ctx, id).Return(user, nil)
	svc := application.NewUserService(repo)

	profile, err := svc.GetPublicProfileByHandle(ctx, "@Blaze")
	require.NoError(t, err)
	assert.Equal(t, "blaze", profile.Handle)
	assert.Equal(t, "saransh", profile.Name)

	repo.byHandle = nil
	_, err = svc.GetPublicProfileByHandle(ctx, "ghost")
	assert.ErrorIs(t, err, authDomain.ErrUserNotFound)

	repo.findErr = errors.New("db down")
	_, err = svc.GetPublicProfileByHandle(ctx, "ghost")
	assert.EqualError(t, err, "db down")

	// A repository without handle lookup behaves as "not found".
	_, err = application.NewUserService(new(mockUserRepo)).GetPublicProfileByHandle(ctx, "blaze")
	assert.ErrorIs(t, err, authDomain.ErrUserNotFound)
}
