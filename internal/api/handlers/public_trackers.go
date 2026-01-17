// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/publictrackers"
)

type PublicTrackersHandler struct {
	service *publictrackers.Service
}

func NewPublicTrackersHandler(service *publictrackers.Service) *PublicTrackersHandler {
	return &PublicTrackersHandler{
		service: service,
	}
}

// GetSettings returns the current public tracker settings
func (h *PublicTrackersHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.service.GetSettings(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("failed to get public tracker settings")
		RespondError(w, http.StatusInternalServerError, "Failed to load public tracker settings")
		return
	}

	RespondJSON(w, http.StatusOK, settings)
}

// UpdateSettings updates the public tracker settings
func (h *PublicTrackersHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var input models.PublicTrackerSettingsInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		log.Warn().Err(err).Msg("failed to decode public tracker settings request")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	settings, err := h.service.UpdateSettings(r.Context(), &input)
	if err != nil {
		log.Error().Err(err).Msg("failed to update public tracker settings")
		RespondError(w, http.StatusInternalServerError, "Failed to update public tracker settings")
		return
	}

	RespondJSON(w, http.StatusOK, settings)
}

// RefreshTrackerList fetches the tracker list from the configured URL
func (h *PublicTrackersHandler) RefreshTrackerList(w http.ResponseWriter, r *http.Request) {
	settings, err := h.service.RefreshTrackerList(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("failed to refresh public tracker list")
		RespondError(w, http.StatusInternalServerError, "Failed to refresh tracker list: "+err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, settings)
}

// ExecuteActionRequest represents the request body for executing a public tracker action
type ExecuteActionRequest struct {
	Hashes    []string                 `json:"hashes"`
	PruneMode publictrackers.PruneMode `json:"pruneMode"`
}

// ExecuteAction performs a public tracker action on the specified torrents
func (h *PublicTrackersHandler) ExecuteAction(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil || instanceID <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	var req ExecuteActionRequest
	if decodeErr := json.NewDecoder(r.Body).Decode(&req); decodeErr != nil {
		log.Warn().Err(decodeErr).Msg("failed to decode public tracker action request")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if len(req.Hashes) == 0 {
		RespondError(w, http.StatusBadRequest, "No torrent hashes provided")
		return
	}

	// Validate prune mode
	switch req.PruneMode {
	case publictrackers.PruneModeAll, publictrackers.PruneModeDead, publictrackers.PruneModeNone:
		// Valid
	default:
		RespondError(w, http.StatusBadRequest, "Invalid prune mode. Must be 'all', 'dead', or 'none'")
		return
	}

	result, err := h.service.ExecuteAction(r.Context(), instanceID, req.Hashes, req.PruneMode)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("failed to execute public tracker action")
		RespondError(w, http.StatusInternalServerError, "Failed to execute action: "+err.Error())
		return
	}

	log.Info().
		Int("instanceID", instanceID).
		Int("totalTorrents", result.TotalTorrents).
		Int("processedCount", result.ProcessedCount).
		Int("skippedPrivate", result.SkippedPrivate).
		Int("trackersAdded", result.TrackersAdded).
		Int("trackersRemoved", result.TrackersRemoved).
		Msg("public tracker action completed")

	RespondJSON(w, http.StatusOK, result)
}
