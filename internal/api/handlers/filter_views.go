// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

const maxFilterViewNameLength = 100

// filterViewUserID is the owner of every filter view. qui is single-user
// self-hosted and there is no user id in the request context; this mirrors
// DashboardSettingsHandler.
const filterViewUserID = 1

type FilterViewHandler struct {
	store *models.FilterViewStore
}

func NewFilterViewHandler(store *models.FilterViewStore) *FilterViewHandler {
	return &FilterViewHandler{store: store}
}

type filterViewPayload struct {
	Name    string          `json:"name"`
	Filters json.RawMessage `json:"filters"`
}

// decodeFilterViewPayload decodes and validates a create/update body, returning
// the trimmed name and the raw filters blob. It responds to the client and
// returns false when the body is unusable.
func decodeFilterViewPayload(w http.ResponseWriter, r *http.Request) (string, json.RawMessage, bool) {
	var payload filterViewPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Msg("failed to decode filter view request")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return "", nil, false
	}

	name := strings.TrimSpace(payload.Name)
	switch {
	case name == "":
		RespondError(w, http.StatusBadRequest, "Name is required")
		return "", nil, false
	case len([]rune(name)) > maxFilterViewNameLength:
		RespondError(w, http.StatusBadRequest, "Name is too long")
		return "", nil, false
	// json.Decoder already validated the syntax and leaves no surrounding
	// whitespace, so a leading '{' is enough to prove this is an object.
	case len(payload.Filters) == 0 || payload.Filters[0] != '{':
		RespondError(w, http.StatusBadRequest, "Filters must be a JSON object")
		return "", nil, false
	}

	return name, payload.Filters, true
}

func (h *FilterViewHandler) List(w http.ResponseWriter, r *http.Request) {
	views, err := h.store.List(r.Context(), filterViewUserID)
	if err != nil {
		log.Error().Err(err).Msg("failed to list filter views")
		RespondError(w, http.StatusInternalServerError, "Failed to load filter views")
		return
	}

	RespondJSON(w, http.StatusOK, views)
}

func (h *FilterViewHandler) Create(w http.ResponseWriter, r *http.Request) {
	name, filters, ok := decodeFilterViewPayload(w, r)
	if !ok {
		return
	}

	view, err := h.store.Create(r.Context(), filterViewUserID, name, filters)
	if err != nil {
		if errors.Is(err, models.ErrDuplicateFilterViewName) {
			RespondError(w, http.StatusConflict, "A view with this name already exists")
			return
		}
		log.Error().Err(err).Msg("failed to create filter view")
		RespondError(w, http.StatusInternalServerError, "Failed to create filter view")
		return
	}

	RespondJSON(w, http.StatusCreated, view)
}

func (h *FilterViewHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := filterViewID(w, r)
	if !ok {
		return
	}

	name, filters, ok := decodeFilterViewPayload(w, r)
	if !ok {
		return
	}

	view, err := h.store.Update(r.Context(), filterViewUserID, id, name, filters)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrDuplicateFilterViewName):
			RespondError(w, http.StatusConflict, "A view with this name already exists")
		case errors.Is(err, sql.ErrNoRows):
			RespondError(w, http.StatusNotFound, "Filter view not found")
		default:
			log.Error().Err(err).Int("id", id).Msg("failed to update filter view")
			RespondError(w, http.StatusInternalServerError, "Failed to update filter view")
		}
		return
	}

	RespondJSON(w, http.StatusOK, view)
}

func (h *FilterViewHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := filterViewID(w, r)
	if !ok {
		return
	}

	if err := h.store.Delete(r.Context(), filterViewUserID, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			RespondError(w, http.StatusNotFound, "Filter view not found")
			return
		}
		log.Error().Err(err).Int("id", id).Msg("failed to delete filter view")
		RespondError(w, http.StatusInternalServerError, "Failed to delete filter view")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func filterViewID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || id <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid view ID")
		return 0, false
	}
	return id, true
}
