// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

// QualityProfileHandler serves CRUD endpoints for quality profiles.
// Quality profiles are global (no instance scope) and referenced from automation conditions.
type QualityProfileHandler struct {
	store *models.QualityProfileStore
}

// NewQualityProfileHandler returns a ready-to-use handler.
func NewQualityProfileHandler(store *models.QualityProfileStore) *QualityProfileHandler {
	return &QualityProfileHandler{store: store}
}

// qualityProfilePayload is the request body for create/update operations.
type qualityProfilePayload struct {
	Name         string               `json:"name"`
	Description  string               `json:"description"`
	GroupFields  []string             `json:"groupFields"`
	RankingTiers []models.RankingTier `json:"rankingTiers"`
}

// List handles GET /api/quality-profiles
func (h *QualityProfileHandler) List(w http.ResponseWriter, r *http.Request) {
	profiles, err := h.store.List(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("quality profiles: failed to list")
		http.Error(w, "Failed to list quality profiles", http.StatusInternalServerError)
		return
	}
	if profiles == nil {
		profiles = []*models.QualityProfile{}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(profiles); err != nil {
		log.Error().Err(err).Msg("quality profiles: failed to encode list response")
	}
}

// Get handles GET /api/quality-profiles/{id}
func (h *QualityProfileHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || id <= 0 {
		http.Error(w, "Invalid quality profile ID", http.StatusBadRequest)
		return
	}
	profile, err := h.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Quality profile not found", http.StatusNotFound)
			return
		}
		log.Error().Err(err).Int("id", id).Msg("quality profiles: failed to get")
		http.Error(w, "Failed to get quality profile", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(profile); err != nil {
		log.Error().Err(err).Msg("quality profiles: failed to encode get response")
	}
}

// Create handles POST /api/quality-profiles
func (h *QualityProfileHandler) Create(w http.ResponseWriter, r *http.Request) {
	var payload qualityProfilePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	p := &models.QualityProfile{
		Name:         payload.Name,
		Description:  payload.Description,
		GroupFields:  payload.GroupFields,
		RankingTiers: payload.RankingTiers,
	}
	created, err := h.store.Create(r.Context(), p)
	if err != nil {
		log.Error().Err(err).Msg("quality profiles: failed to create")
		http.Error(w, "Failed to create quality profile: "+err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(created); err != nil {
		log.Error().Err(err).Msg("quality profiles: failed to encode create response")
	}
}

// Update handles PUT /api/quality-profiles/{id}
func (h *QualityProfileHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || id <= 0 {
		http.Error(w, "Invalid quality profile ID", http.StatusBadRequest)
		return
	}
	var payload qualityProfilePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	p := &models.QualityProfile{
		ID:           id,
		Name:         payload.Name,
		Description:  payload.Description,
		GroupFields:  payload.GroupFields,
		RankingTiers: payload.RankingTiers,
	}
	updated, err := h.store.Update(r.Context(), p)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Quality profile not found", http.StatusNotFound)
			return
		}
		log.Error().Err(err).Int("id", id).Msg("quality profiles: failed to update")
		http.Error(w, "Failed to update quality profile: "+err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(updated); err != nil {
		log.Error().Err(err).Msg("quality profiles: failed to encode update response")
	}
}

// Delete handles DELETE /api/quality-profiles/{id}
func (h *QualityProfileHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || id <= 0 {
		http.Error(w, "Invalid quality profile ID", http.StatusBadRequest)
		return
	}
	if err := h.store.Delete(r.Context(), id); err != nil {
		log.Error().Err(err).Int("id", id).Msg("quality profiles: failed to delete")
		http.Error(w, "Failed to delete quality profile", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
