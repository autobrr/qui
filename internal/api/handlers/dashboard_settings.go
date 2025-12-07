// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

type DashboardSettingsHandler struct {
	store          *models.DashboardSettingsStore
	sessionManager *scs.SessionManager
}

func NewDashboardSettingsHandler(store *models.DashboardSettingsStore, sessionManager *scs.SessionManager) *DashboardSettingsHandler {
	return &DashboardSettingsHandler{
		store:          store,
		sessionManager: sessionManager,
	}
}

func (h *DashboardSettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID := h.sessionManager.GetInt(r.Context(), "user_id")
	if userID == 0 {
		RespondError(w, http.StatusUnauthorized, "User not authenticated")
		return
	}

	settings, err := h.store.GetByUserID(r.Context(), userID)
	if err != nil {
		log.Error().Err(err).Int("userID", userID).Msg("failed to get dashboard settings")
		RespondError(w, http.StatusInternalServerError, "Failed to load dashboard settings")
		return
	}

	RespondJSON(w, http.StatusOK, settings)
}

func (h *DashboardSettingsHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID := h.sessionManager.GetInt(r.Context(), "user_id")
	if userID == 0 {
		RespondError(w, http.StatusUnauthorized, "User not authenticated")
		return
	}

	var input models.DashboardSettingsInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	settings, err := h.store.Update(r.Context(), userID, &input)
	if err != nil {
		log.Error().Err(err).Int("userID", userID).Msg("failed to update dashboard settings")
		RespondError(w, http.StatusInternalServerError, "Failed to update dashboard settings")
		return
	}

	RespondJSON(w, http.StatusOK, settings)
}
