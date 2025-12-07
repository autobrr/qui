// Copyright (c) 2025, s0up and the autobrr contributors.
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

type TrackerCustomizationHandler struct {
	store *models.TrackerCustomizationStore
}

func NewTrackerCustomizationHandler(store *models.TrackerCustomizationStore) *TrackerCustomizationHandler {
	return &TrackerCustomizationHandler{store: store}
}

type TrackerCustomizationPayload struct {
	DisplayName string   `json:"displayName"`
	Domains     []string `json:"domains"`
}

func (p *TrackerCustomizationPayload) toModel(id int) *models.TrackerCustomization {
	return &models.TrackerCustomization{
		ID:          id,
		DisplayName: strings.TrimSpace(p.DisplayName),
		Domains:     normalizeDomains(p.Domains),
	}
}

func (h *TrackerCustomizationHandler) List(w http.ResponseWriter, r *http.Request) {
	customizations, err := h.store.List(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("failed to list tracker customizations")
		RespondError(w, http.StatusInternalServerError, "Failed to load tracker customizations")
		return
	}

	RespondJSON(w, http.StatusOK, customizations)
}

func (h *TrackerCustomizationHandler) Create(w http.ResponseWriter, r *http.Request) {
	var payload TrackerCustomizationPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if strings.TrimSpace(payload.DisplayName) == "" {
		RespondError(w, http.StatusBadRequest, "Display name is required")
		return
	}

	if len(normalizeDomains(payload.Domains)) == 0 {
		RespondError(w, http.StatusBadRequest, "At least one domain is required")
		return
	}

	customization, err := h.store.Create(r.Context(), payload.toModel(0))
	if err != nil {
		log.Error().Err(err).Msg("failed to create tracker customization")
		RespondError(w, http.StatusInternalServerError, "Failed to create tracker customization")
		return
	}

	RespondJSON(w, http.StatusCreated, customization)
}

func (h *TrackerCustomizationHandler) Update(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid customization ID")
		return
	}

	var payload TrackerCustomizationPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if strings.TrimSpace(payload.DisplayName) == "" {
		RespondError(w, http.StatusBadRequest, "Display name is required")
		return
	}

	if len(normalizeDomains(payload.Domains)) == 0 {
		RespondError(w, http.StatusBadRequest, "At least one domain is required")
		return
	}

	customization, err := h.store.Update(r.Context(), payload.toModel(id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			RespondError(w, http.StatusNotFound, "Tracker customization not found")
			return
		}
		log.Error().Err(err).Int("id", id).Msg("failed to update tracker customization")
		RespondError(w, http.StatusInternalServerError, "Failed to update tracker customization")
		return
	}

	RespondJSON(w, http.StatusOK, customization)
}

func (h *TrackerCustomizationHandler) Delete(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid customization ID")
		return
	}

	if err := h.store.Delete(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			RespondError(w, http.StatusNotFound, "Tracker customization not found")
			return
		}
		log.Error().Err(err).Int("id", id).Msg("failed to delete tracker customization")
		RespondError(w, http.StatusInternalServerError, "Failed to delete tracker customization")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func normalizeDomains(domains []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, d := range domains {
		trimmed := strings.TrimSpace(d)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if _, exists := seen[lower]; exists {
			continue
		}
		seen[lower] = struct{}{}
		out = append(out, lower)
	}
	return out
}
