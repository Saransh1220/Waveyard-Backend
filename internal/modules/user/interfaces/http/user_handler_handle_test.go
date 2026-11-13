package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/gateway/middleware"
	authDomain "github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/user/application"
	user_http "github.com/saransh1220/blueprint-audio/internal/modules/user/interfaces/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// handleService adds the optional lookup-by-handle capability the handler checks for.
type handleService struct {
	*mockUserService
	profile *application.PublicUserResponse
	err     error
}

func (s *handleService) GetPublicProfileByHandle(_ context.Context, _ string) (*application.PublicUserResponse, error) {
	return s.profile, s.err
}

func TestUserHandler_UpdateProfile_HandleTakenIsConflict(t *testing.T) {
	svc := new(mockUserService)
	h := user_http.NewUserHandler(svc, new(mockFileService))
	userID := uuid.New()

	handle := "blaze"
	payload, _ := json.Marshal(application.UpdateProfileRequest{Handle: &handle})
	req := httptest.NewRequest(http.MethodPatch, "/users/profile", bytes.NewBuffer(payload))
	req = req.WithContext(context.WithValue(req.Context(), middleware.ContextKeyUserId, userID))
	svc.On("UpdateProfile", mock.Anything, userID, mock.Anything).Return(authDomain.ErrHandleTaken).Once()

	w := httptest.NewRecorder()
	h.UpdateProfile(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestUserHandler_GetPublicProfileByHandle(t *testing.T) {
	newRequest := func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/producers/blaze", nil)
		req.SetPathValue("handle", "blaze")
		return req
	}

	// Found
	svc := &handleService{
		mockUserService: new(mockUserService),
		profile:         &application.PublicUserResponse{ID: uuid.NewString(), Name: "saransh", Handle: "blaze"},
	}
	h := user_http.NewUserHandler(svc, new(mockFileService))
	w := httptest.NewRecorder()
	h.GetPublicProfileByHandle(w, newRequest())
	assert.Equal(t, http.StatusOK, w.Code)
	var got application.PublicUserResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "blaze", got.Handle)

	// Unknown handle
	svc.profile, svc.err = nil, authDomain.ErrUserNotFound
	w = httptest.NewRecorder()
	h.GetPublicProfileByHandle(w, newRequest())
	assert.Equal(t, http.StatusNotFound, w.Code)

	// Unexpected failure
	svc.err = errors.New("db down")
	w = httptest.NewRecorder()
	h.GetPublicProfileByHandle(w, newRequest())
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// A service without handle lookup is "not found"
	plain := user_http.NewUserHandler(new(mockUserService), new(mockFileService))
	w = httptest.NewRecorder()
	plain.GetPublicProfileByHandle(w, newRequest())
	assert.Equal(t, http.StatusNotFound, w.Code)
}
