// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

type DashboardSettingsHandler struct {
	store *models.DashboardSettingsStore
}

func NewDashboardSettingsHandler(store *models.DashboardSettingsStore) *DashboardSettingsHandler {
	return &DashboardSettingsHandler{
		store: store,
	}
}

func (h *DashboardSettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	settings, err := h.store.GetByUserID(r.Context(), 1)
	if err != nil {
		log.Error().Err(err).Msg("failed to get dashboard settings")
		RespondError(w, http.StatusInternalServerError, "Failed to load dashboard settings")
		return
	}

	RespondJSON(w, http.StatusOK, settings)
}

func (h *DashboardSettingsHandler) Update(w http.ResponseWriter, r *http.Request) {
	var input models.DashboardSettingsInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		log.Warn().Err(err).Msg("failed to decode dashboard settings request")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	settings, err := h.store.Update(r.Context(), 1, &input)
	if err != nil {
		log.Error().Err(err).Msg("failed to update dashboard settings")
		RespondError(w, http.StatusInternalServerError, "Failed to update dashboard settings")
		return
	}

	RespondJSON(w, http.StatusOK, settings)
}
