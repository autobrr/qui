// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/activity"
)

const maxClientSettingsBodySize = 1 << 20

type ClientSettingsHandler struct {
	store    *models.ClientSettingsStore
	activity activity.Publisher
}

func NewClientSettingsHandler(store *models.ClientSettingsStore, publisher activity.Publisher) *ClientSettingsHandler {
	return &ClientSettingsHandler{store: store, activity: publisher}
}

func (h *ClientSettingsHandler) GetClientSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.store.GetAll(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to load client settings")
		RespondError(w, http.StatusInternalServerError, "Failed to load client settings")
		return
	}
	RespondJSON(w, http.StatusOK, settings)
}

func (h *ClientSettingsHandler) UpdateClientSettings(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxClientSettingsBodySize)

	var settings map[string]string
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if len(settings) == 0 {
		RespondError(w, http.StatusBadRequest, "No settings provided")
		return
	}
	for key := range settings {
		if key == "" {
			RespondError(w, http.StatusBadRequest, "Invalid setting key")
			return
		}
	}

	if err := h.store.SetMany(r.Context(), settings); err != nil {
		log.Error().Err(err).Msg("Failed to save client settings")
		RespondError(w, http.StatusInternalServerError, "Failed to save client settings")
		return
	}
	h.activity.Publish(activity.Event{Kind: activity.KindClientSettings})
	RespondJSON(w, http.StatusOK, settings)
}
