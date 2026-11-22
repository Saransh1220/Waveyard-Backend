package http

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/saransh1220/blueprint-audio/internal/gateway/middleware"
	"github.com/saransh1220/blueprint-audio/internal/modules/promotion/application"
	"github.com/saransh1220/blueprint-audio/internal/modules/promotion/domain"
	"github.com/saransh1220/blueprint-audio/internal/shared/utils"
)

type Handler struct {
	service *application.Service
}

func NewHandler(service *application.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	producer, ok := requestProducer(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	limit, offset := 25, 0
	var err error
	if query.Has("limit") {
		limit, err = strconv.Atoi(query.Get("limit"))
	}
	if err == nil && query.Has("offset") {
		offset, err = strconv.Atoi(query.Get("offset"))
	}
	if err != nil {
		writeError(w, domain.ErrInvalid)
		return
	}
	promotions, err := h.service.List(r.Context(), producer, limit, offset)
	if err != nil {
		writeError(w, err)
		return
	}
	utils.WriteJSON(w, http.StatusOK, map[string]any{"data": promotions})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	producer, ok := requestProducer(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, domain.ErrInvalid)
		return
	}
	promotion, err := h.service.Get(r.Context(), producer, id)
	if err != nil {
		writeError(w, err)
		return
	}
	utils.WriteJSON(w, http.StatusOK, promotion)
}

func (h *Handler) Preview(w http.ResponseWriter, r *http.Request) {
	producer, ok := requestProducer(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, domain.ErrInvalid)
		return
	}
	preview, err := h.service.Preview(r.Context(), producer, id)
	if err != nil {
		writeError(w, err)
		return
	}
	utils.WriteJSON(w, http.StatusOK, preview)
}

func (h *Handler) CheckEnrollment(w http.ResponseWriter, r *http.Request) {
	producer, ok := requestProducer(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, domain.ErrInvalid)
		return
	}
	var body domain.EnrollmentCheckRequest
	if !decode(w, r, &body) {
		return
	}
	check, err := h.service.CheckEnrollment(r.Context(), producer, id, body.BeatIDs)
	if err != nil {
		writeError(w, err)
		return
	}
	utils.WriteJSON(w, http.StatusOK, check)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	producer, ok := requestProducer(w, r)
	if !ok {
		return
	}
	var rules domain.Rules
	if !decode(w, r, &rules) {
		return
	}
	promotion, err := h.service.Create(r.Context(), producer, rules)
	if err != nil {
		writeError(w, err)
		return
	}
	utils.WriteJSON(w, http.StatusCreated, promotion)
}

func (h *Handler) Change(w http.ResponseWriter, r *http.Request) {
	producer, ok := requestProducer(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, domain.ErrInvalid)
		return
	}
	var body domain.ChangeRequest
	if !decode(w, r, &body) {
		return
	}
	promotion, err := h.service.Change(r.Context(), producer, id, body.Revision, body.Action, body.Rules)
	if err != nil {
		writeError(w, err)
		return
	}
	utils.WriteJSON(w, http.StatusOK, promotion)
}

func requestProducer(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	w.Header().Set("Cache-Control", "no-store")
	producer, ok := r.Context().Value(middleware.ContextKeyUserId).(uuid.UUID)
	if !ok || producer == uuid.Nil {
		utils.WriteError(w, http.StatusUnauthorized, "authentication required", nil)
		return uuid.Nil, false
	}
	return producer, true
}

func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 160*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(target)
	if err == nil {
		err = decoder.Decode(new(any))
		if err == io.EOF {
			return true
		}
	}
	var oversized *http.MaxBytesError
	if errors.As(err, &oversized) {
		utils.WriteError(w, http.StatusRequestEntityTooLarge, "promotion request is too large", nil)
	} else {
		utils.WriteError(w, http.StatusBadRequest, "invalid promotion request body", nil)
	}
	return false
}

func writeError(w http.ResponseWriter, err error) {
	var conflict *domain.EnrollmentConflictError
	switch {
	case errors.As(err, &conflict):
		utils.WriteJSON(w, http.StatusConflict, map[string]any{
			"error": conflict.Error(), "code": "ENROLLMENT_CONFLICT", "conflicts": conflict.Conflicts,
		})
	case errors.Is(err, domain.ErrInvalid):
		utils.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, domain.ErrNotFound):
		utils.WriteError(w, http.StatusNotFound, domain.ErrNotFound.Error(), nil)
	case errors.Is(err, domain.ErrConflict):
		utils.WriteError(w, http.StatusConflict, domain.ErrConflict.Error(), nil)
	default:
		slog.Error("promotion request failed", "error", err)
		utils.WriteError(w, http.StatusInternalServerError, "promotion request failed", nil)
	}
}
