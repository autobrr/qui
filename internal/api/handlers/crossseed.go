// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
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
	UseCategoryFromIndexer       bool     `json:"useCategoryFromIndexer"`
	RunExternalProgramID         *int     `json:"runExternalProgramId"`
}

type automationRunRequest struct {
	Limit  int  `json:"limit"`
	DryRun bool `json:"dryRun"`
}

type searchRunRequest struct {
	InstanceID      int      `json:"instanceId"`
	Categories      []string `json:"categories"`
	Tags            []string `json:"tags"`
	IntervalSeconds int      `json:"intervalSeconds"`
	IndexerIDs      []int    `json:"indexerIds"`
	CooldownMinutes int      `json:"cooldownMinutes"`
}

// NewCrossSeedHandler creates a new cross-seed handler
func NewCrossSeedHandler(service *crossseed.Service) *CrossSeedHandler {
	return &CrossSeedHandler{
		service: service,
	}
}

// Routes registers the cross-seed routes
func (h *CrossSeedHandler) Routes(r chi.Router) {
	// Register instance-scoped route at top level ## placeholder based on docs ##
	r.Get("/instances/{instanceID}/cross-seed/status", h.GetCrossSeedStatus)

	r.Route("/cross-seed", func(r chi.Router) {
		r.Post("/apply", h.AutobrrApply)
		r.Route("/torrents", func(r chi.Router) {
			r.Get("/{instanceID}/{hash}/analyze", h.AnalyzeTorrentForSearch)
			r.Get("/{instanceID}/{hash}/async-status", h.GetAsyncFilteringStatus)
			r.Post("/{instanceID}/{hash}/search", h.SearchTorrentMatches)
			r.Post("/{instanceID}/{hash}/apply", h.ApplyTorrentSearchResults)
		})
		r.Get("/settings", h.GetAutomationSettings)
		r.Put("/settings", h.UpdateAutomationSettings)
		r.Get("/status", h.GetAutomationStatus)
		r.Get("/runs", h.ListAutomationRuns)
		r.Post("/run", h.TriggerAutomationRun)
		r.Route("/search", func(r chi.Router) {
			r.Get("/status", h.GetSearchRunStatus)
			r.Post("/run", h.StartSearchRun)
			r.Post("/run/cancel", h.CancelSearchRun)
			r.Get("/runs", h.ListSearchRunHistory)
		})
		r.Route("/webhook", func(r chi.Router) {
			r.Post("/check", h.WebhookCheck)
		})
	})
}

// AnalyzeTorrentForSearch godoc
// @Summary Analyze torrent for cross-seed search metadata
// @Description Returns metadata about how a torrent would be searched (content type, search type, required categories/capabilities) without performing the actual search
// @Tags cross-seed
// @Produce json
// @Param instanceID path int true "Instance ID"
// @Param hash path string true "Torrent hash"
// @Success 200 {object} crossseed.TorrentInfo
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/cross-seed/torrents/{instanceID}/{hash}/analyze [get]
func (h *CrossSeedHandler) AnalyzeTorrentForSearch(w http.ResponseWriter, r *http.Request) {
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

	torrentInfo, err := h.service.AnalyzeTorrentForSearch(r.Context(), instanceID, hash)
	if err != nil {
		status := http.StatusInternalServerError
		if shouldReturnBadRequest(err) {
			status = http.StatusBadRequest
		}
		log.Error().
			Err(err).
			Int("instanceID", instanceID).
			Str("hash", hash).
			Msg("Failed to analyze torrent for search")
		RespondError(w, status, err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, torrentInfo)
}

// GetAsyncFilteringStatus godoc
// @Summary Get async filtering status for a torrent
// @Description Returns the current status of async indexer filtering for a torrent, including whether content filtering has completed
// @Tags cross-seed
// @Produce json
// @Param instanceID path int true "Instance ID"
// @Param hash path string true "Torrent hash"
// @Success 200 {object} crossseed.AsyncIndexerFilteringState
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/cross-seed/torrents/{instanceID}/{hash}/async-status [get]
func (h *CrossSeedHandler) GetAsyncFilteringStatus(w http.ResponseWriter, r *http.Request) {
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

	filteringState, err := h.service.GetAsyncFilteringStatus(r.Context(), instanceID, hash)
	if err != nil {
		status := http.StatusInternalServerError
		if shouldReturnBadRequest(err) {
			status = http.StatusBadRequest
		}
		log.Error().
			Err(err).
			Int("instanceID", instanceID).
			Str("hash", hash).
			Msg("Failed to get async filtering status")
		RespondError(w, status, err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, filteringState)
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

// AutobrrApply godoc
// @Summary Add a cross-seed torrent provided by autobrr
// @Description Accepts a torrent file from autobrr, matches it against the specified instance, and adds it with alignment if a match is found.
// @Tags cross-seed
// @Accept json
// @Produce json
// @Param request body crossseed.AutobrrApplyRequest true "Autobrr apply request"
// @Success 200 {object} crossseed.CrossSeedResponse
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/cross-seed/apply [post]
func (h *CrossSeedHandler) AutobrrApply(w http.ResponseWriter, r *http.Request) {
	var req crossseed.AutobrrApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode autobrr apply request")
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	response, err := h.service.AutobrrApply(r.Context(), &req)
	if err != nil {
		status := http.StatusInternalServerError
		if shouldReturnBadRequest(err) {
			status = http.StatusBadRequest
		}
		log.Error().Err(err).Msg("Failed to apply autobrr torrent")
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
		UseCategoryFromIndexer:       req.UseCategoryFromIndexer,
		RunExternalProgramID:         req.RunExternalProgramID,
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
		if errors.Is(err, crossseed.ErrAutomationCooldownActive) {
			RespondError(w, http.StatusTooManyRequests, err.Error())
			return
		}
		if errors.Is(err, crossseed.ErrNoIndexersConfigured) {
			RespondError(w, http.StatusBadRequest, "No Torznab indexers configured. Add at least one enabled indexer before running automation.")
			return
		}
		log.Error().Err(err).Msg("Failed to trigger cross-seed automation run")
		RespondError(w, http.StatusInternalServerError, "Failed to start automation run")
		return
	}

	RespondJSON(w, http.StatusAccepted, run)
}

// StartSearchRun starts a scoped search automation run.
func (h *CrossSeedHandler) StartSearchRun(w http.ResponseWriter, r *http.Request) {
	var req searchRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.InstanceID <= 0 {
		RespondError(w, http.StatusBadRequest, "instanceId is required")
		return
	}

	run, err := h.service.StartSearchRun(r.Context(), crossseed.SearchRunOptions{
		InstanceID:      req.InstanceID,
		Categories:      req.Categories,
		Tags:            req.Tags,
		IntervalSeconds: req.IntervalSeconds,
		IndexerIDs:      req.IndexerIDs,
		CooldownMinutes: req.CooldownMinutes,
		RequestedBy:     "api",
	})
	if err != nil {
		if errors.Is(err, crossseed.ErrSearchRunActive) {
			RespondError(w, http.StatusConflict, "Search run already active")
			return
		}
		if errors.Is(err, crossseed.ErrNoIndexersConfigured) {
			RespondError(w, http.StatusBadRequest, "No Torznab indexers configured. Add at least one enabled indexer before running seeded torrent search.")
			return
		}
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	RespondJSON(w, http.StatusAccepted, run)
}

// CancelSearchRun stops the active search run if present.
func (h *CrossSeedHandler) CancelSearchRun(w http.ResponseWriter, r *http.Request) {
	h.service.CancelSearchRun()
	w.WriteHeader(http.StatusNoContent)
}

// GetSearchRunStatus returns current search automation status.
func (h *CrossSeedHandler) GetSearchRunStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.service.GetSearchRunStatus(r.Context())
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, status)
}

// ListSearchRunHistory returns stored search run history for an instance.
func (h *CrossSeedHandler) ListSearchRunHistory(w http.ResponseWriter, r *http.Request) {
	instanceStr := r.URL.Query().Get("instanceId")
	if strings.TrimSpace(instanceStr) == "" {
		RespondError(w, http.StatusBadRequest, "instanceId query parameter is required")
		return
	}
	instanceID, err := strconv.Atoi(instanceStr)
	if err != nil || instanceID <= 0 {
		RespondError(w, http.StatusBadRequest, "instanceId must be a positive integer")
		return
	}

	limit := 25
	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	runs, err := h.service.ListSearchRuns(r.Context(), instanceID, limit, offset)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, runs)
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
// @Failure 501 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/instances/{instanceID}/cross-seed/status [get]
func (h *CrossSeedHandler) GetCrossSeedStatus(w http.ResponseWriter, r *http.Request) {
	instanceIDStr := chi.URLParam(r, "instanceID")
	instanceID, err := strconv.Atoi(instanceIDStr)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Metrics have not been implemented yet; make this explicit to clients instead of returning misleading data.
	RespondError(w, http.StatusNotImplemented, fmt.Sprintf("cross-seed status for instance %d is not implemented yet", instanceID))
}

// WebhookCheck godoc
// @Summary Check if a release can be cross-seeded (autobrr webhook)
// @Description Accepts release metadata from autobrr and checks if matching torrents exist across instances
// @Tags cross-seed
// @Accept json
// @Produce json
// @Param request body crossseed.WebhookCheckRequest true "Release metadata from autobrr"
// @Success 200 {object} crossseed.WebhookCheckResponse "Matches found (recommendation=download)"
// @Failure 404 {object} crossseed.WebhookCheckResponse "No matches found (recommendation=skip)"
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/cross-seed/webhook/check [post]
func (h *CrossSeedHandler) WebhookCheck(w http.ResponseWriter, r *http.Request) {
	var req crossseed.WebhookCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode webhook check request")
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	response, err := h.service.CheckWebhook(r.Context(), &req)
	if err != nil {
		switch {
		case errors.Is(err, crossseed.ErrInvalidWebhookRequest):
			log.Warn().Err(err).Msg("Invalid webhook payload")
			RespondError(w, http.StatusBadRequest, err.Error())
			return
		case errors.Is(err, crossseed.ErrWebhookInstanceNotFound):
			log.Warn().Err(err).Msg("Webhook instance not found")
			RespondError(w, http.StatusNotFound, err.Error())
			return
		default:
			log.Error().Err(err).Msg("Failed to check webhook")
			RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if !response.CanCrossSeed {
		RespondJSON(w, http.StatusNotFound, response)
		return
	}

	RespondJSON(w, http.StatusOK, response)
}
