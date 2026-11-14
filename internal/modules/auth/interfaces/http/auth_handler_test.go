package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/application"
	"github.com/saransh1220/blueprint-audio/internal/modules/auth/domain"
	auth_http "github.com/saransh1220/blueprint-audio/internal/modules/auth/interfaces/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock AuthService
type MockAuthService struct {
	mock.Mock
}

func (m *MockAuthService) Register(ctx context.Context, req application.RegisterRequest) (*domain.User, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockAuthService) Login(ctx context.Context, req application.LoginRequest, isBrowser ...bool) (*application.TokenPair, error) {
	b := len(isBrowser) > 0 && isBrowser[0]
	var args mock.Arguments
	if b {
		args = m.Called(ctx, req, b)
	} else {
		for _, call := range m.ExpectedCalls {
			if call.Method == "Login" && len(call.Arguments) == 3 {
				args = m.Called(ctx, req, b)
				break
			}
		}
		if args == nil {
			args = m.Called(ctx, req)
		}
	}
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*application.TokenPair), args.Error(1)
}

func (m *MockAuthService) GetUser(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockAuthService) GoogleLogin(ctx context.Context, googleClientID string, req application.GoogleLoginRequest, isBrowser ...bool) (*application.TokenPair, error) {
	b := len(isBrowser) > 0 && isBrowser[0]
	var args mock.Arguments
	if b {
		args = m.Called(ctx, googleClientID, req, b)
	} else {
		for _, call := range m.ExpectedCalls {
			if call.Method == "GoogleLogin" && len(call.Arguments) == 4 {
				args = m.Called(ctx, googleClientID, req, b)
				break
			}
		}
		if args == nil {
			args = m.Called(ctx, googleClientID, req)
		}
	}
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*application.TokenPair), args.Error(1)
}

func (m *MockAuthService) RefreshSession(ctx context.Context, refreshToken string, isBrowser ...bool) (*application.TokenPair, error) {
	b := len(isBrowser) > 0 && isBrowser[0]
	var args mock.Arguments
	if b {
		args = m.Called(ctx, refreshToken, b)
	} else {
		for _, call := range m.ExpectedCalls {
			if call.Method == "RefreshSession" && len(call.Arguments) == 3 {
				args = m.Called(ctx, refreshToken, b)
				break
			}
		}
		if args == nil {
			args = m.Called(ctx, refreshToken)
		}
	}
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*application.TokenPair), args.Error(1)
}

func (m *MockAuthService) Logout(ctx context.Context, refreshToken string, accessFamilyID ...*uuid.UUID) error {
	var fam *uuid.UUID
	if len(accessFamilyID) > 0 {
		fam = accessFamilyID[0]
	}
	var args mock.Arguments
	if fam != nil {
		args = m.Called(ctx, refreshToken, fam)
	} else {
		for _, call := range m.ExpectedCalls {
			if call.Method == "Logout" && len(call.Arguments) == 3 {
				args = m.Called(ctx, refreshToken, fam)
				break
			}
		}
		if args == nil {
			args = m.Called(ctx, refreshToken)
		}
	}
	return args.Error(0)
}

func (m *MockAuthService) VerifyEmail(ctx context.Context, req application.VerifyEmailRequest) error {
	args := m.Called(ctx, req)
	return args.Error(0)
}

func (m *MockAuthService) ResendVerification(ctx context.Context, req application.ResendVerificationRequest) error {
	args := m.Called(ctx, req)
	return args.Error(0)
}

func (m *MockAuthService) ForgotPassword(ctx context.Context, req application.ForgotPasswordRequest) error {
	args := m.Called(ctx, req)
	return args.Error(0)
}

func (m *MockAuthService) ResetPassword(ctx context.Context, req application.ResetPasswordRequest) error {
	args := m.Called(ctx, req)
	return args.Error(0)
}

// Mock FileService
type MockFileService struct {
	mock.Mock
}

func (m *MockFileService) GetKeyFromUrl(fileUrl string) (string, error) {
	args := m.Called(fileUrl)
	return args.String(0), args.Error(1)
}

func (m *MockFileService) GetPresignedURL(ctx context.Context, objectName string, expiry time.Duration) (string, error) {
	args := m.Called(ctx, objectName, expiry)
	return args.String(0), args.Error(1)
}

func TestRegisterHandler_Success(t *testing.T) {
	mockService := new(MockAuthService)
	mockFileService := new(MockFileService)
	h := auth_http.NewAuthHandler(mockService, mockFileService, "test-client-id", time.Hour*720, true)

	reqBody := application.RegisterRequest{
		Email:    "test@example.com",
		Password: "password123",
		Name:     "Test User",
		Role:     "artist",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	expectedUser := &domain.User{
		ID:    uuid.New(),
		Email: reqBody.Email,
		Role:  domain.RoleArtist,
	}
	mockService.On("Register", mock.Anything, reqBody).Return(expectedUser, nil)

	h.Register(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	mockService.AssertExpectations(t)
}

func TestRegisterHandler_Conflict(t *testing.T) {
	mockService := new(MockAuthService)
	mockFileService := new(MockFileService)
	h := auth_http.NewAuthHandler(mockService, mockFileService, "test-client-id", time.Hour*720, true)

	reqBody := application.RegisterRequest{
		Email: "existing@example.com",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	mockService.On("Register", mock.Anything, mock.Anything).Return(nil, domain.ErrUserAlreadyExists)

	h.Register(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestRegisterHandler_BadRequest(t *testing.T) {
	mockService := new(MockAuthService)
	mockFileService := new(MockFileService)
	h := auth_http.NewAuthHandler(mockService, mockFileService, "test-client-id", time.Hour*720, true)

	reqBody := application.RegisterRequest{Email: ""}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	mockService.On("Register", mock.Anything, mock.Anything).Return(nil, errors.New("Display name required"))

	h.Register(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
