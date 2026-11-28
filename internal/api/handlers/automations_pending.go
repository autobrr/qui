// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/services/automations"
)

func (h *AutomationHandler) ListPendingDeletes(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}
	if h.service == nil {
		RespondJSON(w, r, http.StatusOK, automations.PendingDeleteList{Rules: []automations.PendingDeleteGroup{}})
		return
	}
	list, err := h.service.ListPendingDeletes(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("failed to list the torrents that wait for a delete")
		RespondError(w, http.StatusInternalServerError, "Failed to load the torrents that wait for a delete")
		return
	}
	RespondJSON(w, r, http.StatusOK, list)
}
