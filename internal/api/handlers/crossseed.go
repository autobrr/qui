// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed"
)

// CrossSeedHandler handles cross-seed API endpoints
type CrossSeedHandler struct {
	service *crossseed.Service
}

type automationSettingsRequest struct {
	Enabled                      bool     `json:"enabled"`
	RunIntervalMinutes           int      `json:"runIntervalMinutes"`
	StartPaused                  bool     `json:"startPaused"`
	Category                     *string  `json:"category"`
	Tags                         []string `json:"tags"`
	IgnorePatterns               []string `json:"ignorePatterns"`
	TargetInstanceIDs            []int    `json:"targetInstanceIds"`
	TargetIndexerIDs             []int    `json:"targetIndexerIds"`
	MaxResultsPerRun             int      `json:"maxResultsPerRun"`
	FindIndividualEpisodes       bool     `json:"findIndividualEpisodes"`
	SizeMismatchTolerancePercent float64  `json:"sizeMismatchTolerancePercent"`
}

type automationRunRequest struct {
	Limit  int  `json:"limit"`
	DryRun bool `json:"dryRun"`
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
		r.Route("/torrents", func(r chi.Router) {
			r.Post("/{instanceID}/{hash}/search", h.SearchTorrentMatches)
			r.Post("/{instanceID}/{hash}/apply", h.ApplyTorrentSearchResults)
		})
		r.Get("/settings", h.GetAutomationSettings)
		r.Put("/settings", h.UpdateAutomationSettings)
		r.Get("/status", h.GetAutomationStatus)
		r.Get("/runs", h.ListAutomationRuns)
		r.Post("/run", h.TriggerAutomationRun)
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
	if req.TorrentName == "" {
		RespondError(w, http.StatusBadRequest, "torrent_name is required")
		return
	}

	// Find candidates
	response, err := h.service.FindCandidates(r.Context(), &req)
	if err != nil {
		log.Error().
			Err(err).
			Str("torrent_name", req.TorrentName).
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

// SearchTorrentMatches godoc
// @Summary Search Torznab indexers for cross-seed matches for a specific torrent
// @Description Uses the seeded torrent's metadata to find compatible releases on the configured Torznab indexers.
// @Tags cross-seed
// @Accept json
// @Produce json
// @Param instanceID path int true "Instance ID"
// @Param hash path string true "Torrent hash"
// @Param request body crossseed.TorrentSearchOptions false "Optional search configuration"
// @Success 200 {object} crossseed.TorrentSearchResponse
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/cross-seed/torrents/{instanceID}/{hash}/search [post]
func (h *CrossSeedHandler) SearchTorrentMatches(w http.ResponseWriter, r *http.Request) {
	instanceIDStr := chi.URLParam(r, "instanceID")
	instanceID, err := strconv.Atoi(instanceIDStr)
	if err != nil || instanceID <= 0 {
		RespondError(w, http.StatusBadRequest, "instanceID must be a positive integer")
		return
	}

	hash := strings.TrimSpace(chi.URLParam(r, "hash"))
	if hash == "" {
		RespondError(w, http.StatusBadRequest, "hash is required")
		return
	}

	var opts crossseed.TorrentSearchOptions
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&opts); err != nil && !errors.Is(err, io.EOF) {
			log.Error().
				Err(err).
				Int("instanceID", instanceID).
				Str("hash", hash).
				Msg("Failed to decode torrent search request")
			RespondError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
	}

	response, err := h.service.SearchTorrentMatches(r.Context(), instanceID, hash, opts)
	if err != nil {
		status := http.StatusInternalServerError
		if shouldReturnBadRequest(err) {
			status = http.StatusBadRequest
		}
		log.Error().
			Err(err).
			Int("instanceID", instanceID).
			Str("hash", hash).
			Msg("Failed to search cross-seed matches")
		RespondError(w, status, err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, response)
}

// ApplyTorrentSearchResults godoc
// @Summary Add torrents discovered via cross-seed search
// @Description Downloads the selected releases and reuses the cross-seed pipeline to add them to the specified instance.
// @Tags cross-seed
// @Accept json
// @Produce json
// @Param instanceID path int true "Instance ID"
// @Param hash path string true "Torrent hash"
// @Param request body crossseed.ApplyTorrentSearchRequest true "Selections to add"
// @Success 200 {object} crossseed.ApplyTorrentSearchResponse
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/cross-seed/torrents/{instanceID}/{hash}/apply [post]
func (h *CrossSeedHandler) ApplyTorrentSearchResults(w http.ResponseWriter, r *http.Request) {
	instanceIDStr := chi.URLParam(r, "instanceID")
	instanceID, err := strconv.Atoi(instanceIDStr)
	if err != nil || instanceID <= 0 {
		RespondError(w, http.StatusBadRequest, "instanceID must be a positive integer")
		return
	}

	hash := strings.TrimSpace(chi.URLParam(r, "hash"))
	if hash == "" {
		RespondError(w, http.StatusBadRequest, "hash is required")
		return
	}

	var req crossseed.ApplyTorrentSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().
			Err(err).
			Int("instanceID", instanceID).
			Str("hash", hash).
			Msg("Failed to decode cross-seed apply request")
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if len(req.Selections) == 0 {
		RespondError(w, http.StatusBadRequest, "selections are required")
		return
	}

	response, err := h.service.ApplyTorrentSearchResults(r.Context(), instanceID, hash, &req)
	if err != nil {
		status := http.StatusInternalServerError
		if shouldReturnBadRequest(err) {
			status = http.StatusBadRequest
		}
		log.Error().
			Err(err).
			Int("instanceID", instanceID).
			Str("hash", hash).
			Msg("Failed to apply cross-seed search results")
		RespondError(w, status, err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, response)
}

func shouldReturnBadRequest(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "not fully downloaded") ||
		strings.Contains(msg, "invalid")
}

// GetAutomationSettings returns scheduler configuration.
// GetAutomationSettings godoc
// @Summary Get cross-seed automation settings
// @Description Returns current automation configuration for cross-seeding
// @Tags cross-seed
// @Produce json
// @Success 200 {object} models.CrossSeedAutomationSettings
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/cross-seed/settings [get]
func (h *CrossSeedHandler) GetAutomationSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.service.GetAutomationSettings(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to load cross-seed automation settings")
		RespondError(w, http.StatusInternalServerError, "Failed to load automation settings")
		return
	}

	RespondJSON(w, http.StatusOK, settings)
}

// UpdateAutomationSettings updates scheduler configuration.
// UpdateAutomationSettings godoc
// @Summary Update cross-seed automation settings
// @Description Updates the automation scheduler configuration for cross-seeding
// @Tags cross-seed
// @Accept json
// @Produce json
// @Param request body automationSettingsRequest true "Automation settings"
// @Success 200 {object} models.CrossSeedAutomationSettings
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/cross-seed/settings [put]
func (h *CrossSeedHandler) UpdateAutomationSettings(w http.ResponseWriter, r *http.Request) {
	var req automationSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	category := req.Category
	if category != nil {
		trimmed := strings.TrimSpace(*category)
		if trimmed == "" {
			category = nil
		} else {
			category = &trimmed
		}
	}

	settings := &models.CrossSeedAutomationSettings{
		Enabled:                      req.Enabled,
		RunIntervalMinutes:           req.RunIntervalMinutes,
		StartPaused:                  req.StartPaused,
		Category:                     category,
		Tags:                         req.Tags,
		IgnorePatterns:               req.IgnorePatterns,
		TargetInstanceIDs:            req.TargetInstanceIDs,
		TargetIndexerIDs:             req.TargetIndexerIDs,
		MaxResultsPerRun:             req.MaxResultsPerRun,
		FindIndividualEpisodes:       req.FindIndividualEpisodes,
		SizeMismatchTolerancePercent: req.SizeMismatchTolerancePercent,
	}

	updated, err := h.service.UpdateAutomationSettings(r.Context(), settings)
	if err != nil {
		log.Error().Err(err).Msg("Failed to update cross-seed automation settings")
		RespondError(w, http.StatusInternalServerError, "Failed to update automation settings")
		return
	}

	RespondJSON(w, http.StatusOK, updated)
}

// GetAutomationStatus returns scheduler state and latest run metadata.
// GetAutomationStatus godoc
// @Summary Get cross-seed automation status
// @Description Returns current scheduler state and last automation run details
// @Tags cross-seed
// @Produce json
// @Success 200 {object} crossseed.AutomationStatus
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/cross-seed/status [get]
func (h *CrossSeedHandler) GetAutomationStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.service.GetAutomationStatus(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to load cross-seed automation status")
		RespondError(w, http.StatusInternalServerError, "Failed to load automation status")
		return
	}

	RespondJSON(w, http.StatusOK, status)
}

// ListAutomationRuns returns automation history.
// ListAutomationRuns godoc
// @Summary List cross-seed automation runs
// @Description Returns paginated automation run history
// @Tags cross-seed
// @Produce json
// @Param limit query int false "Limit"
// @Param offset query int false "Offset"
// @Success 200 {array} models.CrossSeedRun
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/cross-seed/runs [get]
func (h *CrossSeedHandler) ListAutomationRuns(w http.ResponseWriter, r *http.Request) {
	limit := 25
	offset := 0

	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	runs, err := h.service.ListAutomationRuns(r.Context(), limit, offset)
	if err != nil {
		log.Error().Err(err).Msg("Failed to list cross-seed automation runs")
		RespondError(w, http.StatusInternalServerError, "Failed to list automation runs")
		return
	}

	RespondJSON(w, http.StatusOK, runs)
}

// TriggerAutomationRun queues a manual automation pass.
// TriggerAutomationRun godoc
// @Summary Trigger cross-seed automation run
// @Description Starts an on-demand automation pass
// @Tags cross-seed
// @Accept json
// @Produce json
// @Param request body automationRunRequest false "Automation run options"
// @Success 202 {object} models.CrossSeedRun
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 409 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/cross-seed/run [post]
func (h *CrossSeedHandler) TriggerAutomationRun(w http.ResponseWriter, r *http.Request) {
	var req automationRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	run, err := h.service.RunAutomation(r.Context(), crossseed.AutomationRunOptions{
		RequestedBy: "api",
		Mode:        models.CrossSeedRunModeManual,
		DryRun:      req.DryRun,
		Limit:       req.Limit,
	})
	if err != nil {
		if errors.Is(err, crossseed.ErrAutomationRunning) {
			RespondError(w, http.StatusConflict, "Automation already running")
			return
		}
		log.Error().Err(err).Msg("Failed to trigger cross-seed automation run")
		RespondError(w, http.StatusInternalServerError, "Failed to start automation run")
		return
	}

	RespondJSON(w, http.StatusAccepted, run)
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
