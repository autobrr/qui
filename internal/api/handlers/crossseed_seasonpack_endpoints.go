// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/services/crossseed"
)

func (h *CrossSeedHandler) seasonPackEndpoints(r chi.Router) {
	r.Post("/check", h.SeasonPackCheck)
	r.Post("/apply", h.SeasonPackApply)
}

func (h *CrossSeedHandler) SeasonPackCheck(w http.ResponseWriter, r *http.Request) {
	var req crossseed.SeasonPackCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	resp, err := h.service.CheckSeasonPackWebhook(r.Context(), &req)
	if err != nil {
		log.Error().Err(err).Str("torrentName", req.TorrentName).Msg("Season pack check failed")
		RespondError(w, http.StatusInternalServerError, "Failed to check season pack")
		return
	}

	if resp.Ready {
		RespondJSON(w, http.StatusOK, resp)
		return
	}
	RespondJSON(w, http.StatusNotFound, resp)
}

func (h *CrossSeedHandler) SeasonPackApply(w http.ResponseWriter, r *http.Request) {
	var req crossseed.SeasonPackApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	resp, err := h.service.ApplySeasonPackWebhook(context.WithoutCancel(r.Context()), &req)
	if err != nil {
		log.Error().Err(err).Str("torrentName", req.TorrentName).Msg("Season pack apply failed")
		RespondError(w, http.StatusInternalServerError, "Failed to apply season pack")
		return
	}
	if resp == nil || !resp.Applied {
		reason := ""
		if resp != nil {
			reason = resp.Reason
		}
		log.Error().
			Str("torrentName", req.TorrentName).
			Str("reason", reason).
			Msg("Season pack apply returned failure response")
		RespondError(w, http.StatusInternalServerError, "Failed to apply season pack")
		return
	}

	RespondJSON(w, http.StatusOK, resp)
}
