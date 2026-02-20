// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

type LogExclusionsHandler struct {
	store *models.LogExclusionsStore
}

func NewLogExclusionsHandler(store *models.LogExclusionsStore) *LogExclusionsHandler {
	return &LogExclusionsHandler{
		store: store,
	}
}

func (h *LogExclusionsHandler) Get(w http.ResponseWriter, r *http.Request) {
	exclusions, err := h.store.Get(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("failed to get log exclusions")
		RespondError(w, http.StatusInternalServerError, "Failed to load log exclusions")
		return
	}

	RespondJSON(w, http.StatusOK, exclusions)
}

func (h *LogExclusionsHandler) Update(w http.ResponseWriter, r *http.Request) {
	var input models.LogExclusionsInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		log.Warn().Err(err).Msg("failed to decode log exclusions request")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	exclusions, err := h.store.Update(r.Context(), &input)
	if err != nil {
		log.Error().Err(err).Msg("failed to update log exclusions")
		RespondError(w, http.StatusInternalServerError, "Failed to update log exclusions")
		return
	}

	RespondJSON(w, http.StatusOK, exclusions)
}
