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
	Name      string          `json:"name"`
	Filters   json.RawMessage `json:"filters"`
	SortOrder int             `json:"sortOrder"`
}

// parse validates the payload and returns the trimmed name plus the raw filters
// blob. The blob is only checked for being a JSON object; its shape belongs to
// the frontend. A non-empty third return value is the client-facing rejection
// reason.
func (p *filterViewPayload) parse() (string, json.RawMessage, string) {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return "", nil, "Name is required"
	}
	if len([]rune(name)) > maxFilterViewNameLength {
		return "", nil, "Name is too long"
	}

	// json.Decoder already validated the syntax, so a leading '{' is enough to
	// prove this is an object.
	filters := json.RawMessage(strings.TrimSpace(string(p.Filters)))
	if len(filters) == 0 || filters[0] != '{' {
		return "", nil, "Filters must be a JSON object"
	}

	return name, filters, ""
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
	var payload filterViewPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Msg("failed to decode filter view request")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	name, filters, invalid := payload.parse()
	if invalid != "" {
		RespondError(w, http.StatusBadRequest, invalid)
		return
	}

	view, err := h.store.Create(r.Context(), filterViewUserID, name, filters, payload.SortOrder)
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

	var payload filterViewPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Msg("failed to decode filter view request")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	name, filters, invalid := payload.parse()
	if invalid != "" {
		RespondError(w, http.StatusBadRequest, invalid)
		return
	}

	view, err := h.store.Update(r.Context(), filterViewUserID, id, name, filters, payload.SortOrder)
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
