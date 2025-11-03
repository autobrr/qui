// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/services/crossseed"
)

// CrossSeedHandler handles cross-seed API endpoints
type CrossSeedHandler struct {
	service *crossseed.Service
}

// NewCrossSeedHandler creates a new cross-seed handler
func NewCrossSeedHandler(service *crossseed.Service) *CrossSeedHandler {
	return &CrossSeedHandler{
		service: service,
	}
}

// Routes registers the cross-seed routes
func (h *CrossSeedHandler) Routes(r chi.Router) {
	r.Route("/cross-seed", func(r chi.Router) {
		r.Post("/find-candidates", h.FindCandidates)
		r.Post("/cross", h.CrossSeed)
	})
}

// FindCandidates godoc
// @Summary Find cross-seed candidates
// @Description Finds potential torrents to cross-seed across instances
// @Tags cross-seed
// @Accept json
// @Produce json
// @Param request body crossseed.FindCandidatesRequest true "Find candidates request"
// @Success 200 {object} crossseed.FindCandidatesResponse
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/cross-seed/find-candidates [post]
func (h *CrossSeedHandler) FindCandidates(w http.ResponseWriter, r *http.Request) {
	var req crossseed.FindCandidatesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode find candidates request")
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate request
	if req.SourceInstanceID == 0 {
		RespondError(w, http.StatusBadRequest, "source_instance_id is required")
		return
	}

	if req.TorrentHash == "" {
		RespondError(w, http.StatusBadRequest, "torrent_hash is required")
		return
	}

	// Find candidates
	response, err := h.service.FindCandidates(r.Context(), &req)
	if err != nil {
		log.Error().
			Err(err).
			Int("source_instance_id", req.SourceInstanceID).
			Str("torrent_hash", req.TorrentHash).
			Msg("Failed to find cross-seed candidates")
		RespondError(w, http.StatusInternalServerError, "Failed to find candidates")
		return
	}

	RespondJSON(w, http.StatusOK, response)
}

// CrossSeed godoc
// @Summary Cross-seed a torrent
// @Description Cross-seeds a torrent to target instances with matching files
// @Tags cross-seed
// @Accept json
// @Produce json
// @Param request body crossseed.CrossSeedRequest true "Cross-seed request"
// @Success 200 {object} crossseed.CrossSeedResponse
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/cross-seed/cross [post]
func (h *CrossSeedHandler) CrossSeed(w http.ResponseWriter, r *http.Request) {
	var req crossseed.CrossSeedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode cross-seed request")
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate request
	if req.TorrentData == "" {
		RespondError(w, http.StatusBadRequest, "torrent_data is required")
		return
	}

	// Perform cross-seed
	response, err := h.service.CrossSeed(r.Context(), &req)
	if err != nil {
		log.Error().
			Err(err).
			Msg("Failed to cross-seed torrent")
		RespondError(w, http.StatusInternalServerError, "Failed to cross-seed torrent")
		return
	}

	// Determine HTTP status based on results
	status := http.StatusOK
	if !response.Success {
		status = http.StatusPartialContent // 206 indicates partial success or all failures
	}

	RespondJSON(w, status, response)
}

// GetCrossSeedStatus godoc
// @Summary Get cross-seed status for an instance
// @Description Returns statistics about cross-seeded torrents on an instance
// @Tags cross-seed
// @Produce json
// @Param instanceID path int true "Instance ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/instances/{instanceID}/cross-seed/status [get]
func (h *CrossSeedHandler) GetCrossSeedStatus(w http.ResponseWriter, r *http.Request) {
	instanceIDStr := chi.URLParam(r, "instanceID")
	instanceID, err := strconv.Atoi(instanceIDStr)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// TODO: Implement cross-seed status retrieval
	// This could include:
	// - Count of torrents tagged with "qui-cross-seed"
	// - Total data cross-seeded
	// - Recent cross-seed operations

	status := map[string]interface{}{
		"instance_id":     instanceID,
		"cross_seeded":    0,
		"pending":         0,
		"last_cross_seed": nil,
	}

	RespondJSON(w, http.StatusOK, status)
}
