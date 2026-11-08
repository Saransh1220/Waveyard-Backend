package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/gateway/middleware"
	"github.com/saransh1220/blueprint-audio/internal/modules/legal/domain"
	"github.com/stretchr/testify/assert"
)

func TestLegalHandler_PublicEndpoints_EdgeCases(t *testing.T) {
	t.Run("GetDocument missing key", func(t *testing.T) {
		h := NewLegalHandler(&mockLegalService{})
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/legal", nil)
		h.GetDocument(w, r)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("GetDocument not found and generic error", func(t *testing.T) {
		h404 := NewLegalHandler(&mockLegalService{err: domain.ErrDocumentNotFound})
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/legal/unknown", nil)
		r.SetPathValue("key", "unknown")
		h404.GetDocument(w, r)
		assert.Equal(t, http.StatusNotFound, w.Code)

		h500 := NewLegalHandler(&mockLegalService{err: errors.New("db down")})
		w = httptest.NewRecorder()
		r = httptest.NewRequest(http.MethodGet, "/legal/terms", nil)
		r.SetPathValue("key", "terms")
		h500.GetDocument(w, r)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("GetDocumentVersions missing key and error", func(t *testing.T) {
		h := NewLegalHandler(&mockLegalService{})
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/legal/versions", nil)
		h.GetDocumentVersions(w, r)
		assert.Equal(t, http.StatusBadRequest, w.Code)

		hErr := NewLegalHandler(&mockLegalService{err: errors.New("db down")})
		w = httptest.NewRecorder()
		r = httptest.NewRequest(http.MethodGet, "/legal/terms/versions", nil)
		r.SetPathValue("key", "terms")
		hErr.GetDocumentVersions(w, r)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("GetDocumentVersion missing key or version, 404, 500", func(t *testing.T) {
		h := NewLegalHandler(&mockLegalService{})
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/legal/terms/versions/", nil)
		h.GetDocumentVersion(w, r)
		assert.Equal(t, http.StatusBadRequest, w.Code)

		h404 := NewLegalHandler(&mockLegalService{err: domain.ErrVersionNotFound})
		w = httptest.NewRecorder()
		r = httptest.NewRequest(http.MethodGet, "/legal/terms/versions/v1", nil)
		r.SetPathValue("key", "terms")
		r.SetPathValue("version", "v1")
		h404.GetDocumentVersion(w, r)
		assert.Equal(t, http.StatusNotFound, w.Code)

		h500 := NewLegalHandler(&mockLegalService{err: errors.New("boom")})
		w = httptest.NewRecorder()
		r = httptest.NewRequest(http.MethodGet, "/legal/terms/versions/v1", nil)
		r.SetPathValue("key", "terms")
		r.SetPathValue("version", "v1")
		h500.GetDocumentVersion(w, r)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("GetRequirements unauthorized and error", func(t *testing.T) {
		h := NewLegalHandler(&mockLegalService{})
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/legal/requirements", nil)
		h.GetRequirements(w, r)
		assert.Equal(t, http.StatusUnauthorized, w.Code)

		hErr := NewLegalHandler(&mockLegalService{err: errors.New("err")})
		w = httptest.NewRecorder()
		r = httptest.NewRequest(http.MethodGet, "/legal/requirements", nil)
		r = r.WithContext(context.WithValue(r.Context(), middleware.ContextKeyUserId, uuid.New()))
		hErr.GetRequirements(w, r)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("PublishVersion bad json, unauthorized, error", func(t *testing.T) {
		h := NewLegalHandler(&mockLegalService{})

		// unauthorized
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/legal/publish", nil)
		h.PublishVersion(w, r)
		assert.Equal(t, http.StatusUnauthorized, w.Code)

		// bad json
		w = httptest.NewRecorder()
		r = authedLegalReq(http.MethodPost, "/legal/publish", nil)
		r.Body = http.NoBody
		h.PublishVersion(w, r)
		assert.Equal(t, http.StatusBadRequest, w.Code)

		// service error
		hErr := NewLegalHandler(&mockLegalService{err: errors.New("failed")})
		w = httptest.NewRecorder()
		r = authedLegalReq(http.MethodPost, "/legal/publish", map[string]any{"version_id": uuid.New()})
		hErr.PublishVersion(w, r)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("CreateDraft unauthorized, bad json and service error", func(t *testing.T) {
		h := NewLegalHandler(&mockLegalService{})
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/legal/draft", nil)
		h.CreateDraft(w, r)
		assert.Equal(t, http.StatusUnauthorized, w.Code)

		w = httptest.NewRecorder()
		r = authedLegalReq(http.MethodPost, "/legal/draft", nil)
		r.Body = http.NoBody
		h.CreateDraft(w, r)
		assert.Equal(t, http.StatusBadRequest, w.Code)

		hErr := NewLegalHandler(&mockLegalService{err: errors.New("draft err")})
		w = httptest.NewRecorder()
		r = authedLegalReq(http.MethodPost, "/legal/terms/draft", domain.DocumentVersion{Title: "Draft"})
		r.SetPathValue("key", "terms")
		hErr.CreateDraft(w, r)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}
