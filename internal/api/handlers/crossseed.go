// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
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
	"github.com/autobrr/qui/internal/services/jackett"
)

// CrossSeedHandler handles cross-seed API endpoints
type CrossSeedHandler struct {
	service         *crossseed.Service
	completionStore *models.InstanceCrossSeedCompletionStore
	instanceStore   *models.InstanceStore
}

type automationSettingsRequest struct {
	Enabled                      bool     `json:"enabled"`
	RunIntervalMinutes           int      `json:"runIntervalMinutes"`
	StartPaused                  bool     `json:"startPaused"`
	Category                     *string  `json:"category"`
	IgnorePatterns               []string `json:"ignorePatterns"`
	TargetInstanceIDs            []int    `json:"targetInstanceIds"`
	TargetIndexerIDs             []int    `json:"targetIndexerIds"`
	MaxResultsPerRun             int      `json:"maxResultsPerRun"` // Deprecated: automation now processes full feeds and ignores this value
	FindIndividualEpisodes       bool     `json:"findIndividualEpisodes"`
	SizeMismatchTolerancePercent float64  `json:"sizeMismatchTolerancePercent"`
	UseCategoryFromIndexer       bool     `json:"useCategoryFromIndexer"`
	UseCrossCategorySuffix       bool     `json:"useCrossCategorySuffix"`
	RunExternalProgramID         *int     `json:"runExternalProgramId"`
	SkipRecheck                  bool     `json:"skipRecheck"`
}

type automationSettingsPatchRequest struct {
	Enabled            *bool          `json:"enabled,omitempty"`
	RunIntervalMinutes *int           `json:"runIntervalMinutes,omitempty"`
	StartPaused        *bool          `json:"startPaused,omitempty"`
	Category           optionalString `json:"category"`
	IgnorePatterns     *[]string      `json:"ignorePatterns,omitempty"`
	TargetInstanceIDs  *[]int         `json:"targetInstanceIds,omitempty"`
	TargetIndexerIDs   *[]int         `json:"targetIndexerIds,omitempty"`
	MaxResultsPerRun   *int           `json:"maxResultsPerRun,omitempty"` // Deprecated: automation now processes full feeds and ignores this value
	// RSS source filtering: filter which local torrents to search when checking RSS feeds
	RSSSourceCategories        *[]string `json:"rssSourceCategories,omitempty"`
	RSSSourceTags              *[]string `json:"rssSourceTags,omitempty"`
	RSSSourceExcludeCategories *[]string `json:"rssSourceExcludeCategories,omitempty"`
	RSSSourceExcludeTags       *[]string `json:"rssSourceExcludeTags,omitempty"`
	// Webhook source filtering: filter which local torrents to search when checking webhook requests
	WebhookSourceCategories        *[]string   `json:"webhookSourceCategories,omitempty"`
	WebhookSourceTags              *[]string   `json:"webhookSourceTags,omitempty"`
	WebhookSourceExcludeCategories *[]string   `json:"webhookSourceExcludeCategories,omitempty"`
	WebhookSourceExcludeTags       *[]string   `json:"webhookSourceExcludeTags,omitempty"`
	FindIndividualEpisodes         *bool       `json:"findIndividualEpisodes,omitempty"`
	SizeMismatchTolerancePercent   *float64    `json:"sizeMismatchTolerancePercent,omitempty"`
	UseCategoryFromIndexer         *bool       `json:"useCategoryFromIndexer,omitempty"`
	UseCrossCategorySuffix         *bool       `json:"useCrossCategorySuffix,omitempty"`
	RunExternalProgramID           optionalInt `json:"runExternalProgramId"`
	// Source-specific tagging
	RSSAutomationTags    *[]string `json:"rssAutomationTags,omitempty"`
	SeededSearchTags     *[]string `json:"seededSearchTags,omitempty"`
	CompletionSearchTags *[]string `json:"completionSearchTags,omitempty"`
	WebhookTags          *[]string `json:"webhookTags,omitempty"`
	InheritSourceTags    *bool     `json:"inheritSourceTags,omitempty"`
	// Skip auto-resume settings per source mode
	SkipAutoResumeRSS          *bool `json:"skipAutoResumeRss,omitempty"`
	SkipAutoResumeSeededSearch *bool `json:"skipAutoResumeSeededSearch,omitempty"`
	SkipAutoResumeCompletion   *bool `json:"skipAutoResumeCompletion,omitempty"`
	SkipAutoResumeWebhook      *bool `json:"skipAutoResumeWebhook,omitempty"`
	SkipRecheck                *bool `json:"skipRecheck,omitempty"`
}

type optionalString struct {
	Set   bool
	Value *string
}

func (o *optionalString) UnmarshalJSON(data []byte) error {
	o.Set = true
	if string(data) == "null" {
		o.Value = nil
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	o.Value = &value
	return nil
}

type optionalInt struct {
	Set   bool
	Value *int
}

func (o *optionalInt) UnmarshalJSON(data []byte) error {
	o.Set = true
	if string(data) == "null" {
		o.Value = nil
		return nil
	}
	var value int
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	o.Value = &value
	return nil
}

type searchSettingsPatchRequest struct {
	InstanceID      optionalInt `json:"instanceId"`
	Categories      *[]string   `json:"categories,omitempty"`
	Tags            *[]string   `json:"tags,omitempty"`
	IndexerIDs      *[]int      `json:"indexerIds,omitempty"`
	IntervalSeconds *int        `json:"intervalSeconds,omitempty"`
	CooldownMinutes *int        `json:"cooldownMinutes,omitempty"`
}

func (r searchSettingsPatchRequest) isEmpty() bool {
	return !r.InstanceID.Set &&
		r.Categories == nil &&
		r.Tags == nil &&
		r.IndexerIDs == nil &&
		r.IntervalSeconds == nil &&
		r.CooldownMinutes == nil
}

func (r automationSettingsPatchRequest) isEmpty() bool {
	return r.Enabled == nil &&
		r.RunIntervalMinutes == nil &&
		r.StartPaused == nil &&
		!r.Category.Set &&
		r.IgnorePatterns == nil &&
		r.TargetInstanceIDs == nil &&
		r.TargetIndexerIDs == nil &&
		r.MaxResultsPerRun == nil &&
		r.RSSSourceCategories == nil &&
		r.RSSSourceTags == nil &&
		r.RSSSourceExcludeCategories == nil &&
		r.RSSSourceExcludeTags == nil &&
		r.WebhookSourceCategories == nil &&
		r.WebhookSourceTags == nil &&
		r.WebhookSourceExcludeCategories == nil &&
		r.WebhookSourceExcludeTags == nil &&
		r.FindIndividualEpisodes == nil &&
		r.SizeMismatchTolerancePercent == nil &&
		r.UseCategoryFromIndexer == nil &&
		r.UseCrossCategorySuffix == nil &&
		!r.RunExternalProgramID.Set &&
		r.RSSAutomationTags == nil &&
		r.SeededSearchTags == nil &&
		r.CompletionSearchTags == nil &&
		r.WebhookTags == nil &&
		r.InheritSourceTags == nil &&
		r.SkipAutoResumeRSS == nil &&
		r.SkipAutoResumeSeededSearch == nil &&
		r.SkipAutoResumeCompletion == nil &&
		r.SkipAutoResumeWebhook == nil &&
		r.SkipRecheck == nil
}

func applyAutomationSettingsPatch(settings *models.CrossSeedAutomationSettings, patch automationSettingsPatchRequest) {
	if patch.Enabled != nil {
		settings.Enabled = *patch.Enabled
	}
	if patch.RunIntervalMinutes != nil {
		settings.RunIntervalMinutes = *patch.RunIntervalMinutes
	}
	if patch.StartPaused != nil {
		settings.StartPaused = *patch.StartPaused
	}
	if patch.Category.Set {
		if patch.Category.Value == nil {
			settings.Category = nil
		} else {
			trimmed := strings.TrimSpace(*patch.Category.Value)
			if trimmed == "" {
				settings.Category = nil
			} else {
				settings.Category = &trimmed
			}
		}
	}
	if patch.IgnorePatterns != nil {
		settings.IgnorePatterns = *patch.IgnorePatterns
	}
	if patch.TargetInstanceIDs != nil {
		settings.TargetInstanceIDs = *patch.TargetInstanceIDs
	}
	if patch.TargetIndexerIDs != nil {
		settings.TargetIndexerIDs = *patch.TargetIndexerIDs
	}
	if patch.MaxResultsPerRun != nil {
		settings.MaxResultsPerRun = *patch.MaxResultsPerRun
	}
	// RSS source filtering
	if patch.RSSSourceCategories != nil {
		settings.RSSSourceCategories = *patch.RSSSourceCategories
	}
	if patch.RSSSourceTags != nil {
		settings.RSSSourceTags = *patch.RSSSourceTags
	}
	if patch.RSSSourceExcludeCategories != nil {
		settings.RSSSourceExcludeCategories = *patch.RSSSourceExcludeCategories
	}
	if patch.RSSSourceExcludeTags != nil {
		settings.RSSSourceExcludeTags = *patch.RSSSourceExcludeTags
	}
	// Webhook source filtering
	if patch.WebhookSourceCategories != nil {
		settings.WebhookSourceCategories = *patch.WebhookSourceCategories
	}
	if patch.WebhookSourceTags != nil {
		settings.WebhookSourceTags = *patch.WebhookSourceTags
	}
	if patch.WebhookSourceExcludeCategories != nil {
		settings.WebhookSourceExcludeCategories = *patch.WebhookSourceExcludeCategories
	}
	if patch.WebhookSourceExcludeTags != nil {
		settings.WebhookSourceExcludeTags = *patch.WebhookSourceExcludeTags
	}
	if patch.FindIndividualEpisodes != nil {
		settings.FindIndividualEpisodes = *patch.FindIndividualEpisodes
	}
	if patch.SizeMismatchTolerancePercent != nil {
		settings.SizeMismatchTolerancePercent = *patch.SizeMismatchTolerancePercent
	}
	if patch.UseCategoryFromIndexer != nil {
		settings.UseCategoryFromIndexer = *patch.UseCategoryFromIndexer
	}
	if patch.UseCrossCategorySuffix != nil {
		settings.UseCrossCategorySuffix = *patch.UseCrossCategorySuffix
	}
	if patch.RunExternalProgramID.Set {
		settings.RunExternalProgramID = patch.RunExternalProgramID.Value
	}
	// Source-specific tagging
	if patch.RSSAutomationTags != nil {
		settings.RSSAutomationTags = *patch.RSSAutomationTags
	}
	if patch.SeededSearchTags != nil {
		settings.SeededSearchTags = *patch.SeededSearchTags
	}
	if patch.CompletionSearchTags != nil {
		settings.CompletionSearchTags = *patch.CompletionSearchTags
	}
	if patch.WebhookTags != nil {
		settings.WebhookTags = *patch.WebhookTags
	}
	if patch.InheritSourceTags != nil {
		settings.InheritSourceTags = *patch.InheritSourceTags
	}
	// Skip auto-resume settings
	if patch.SkipAutoResumeRSS != nil {
		settings.SkipAutoResumeRSS = *patch.SkipAutoResumeRSS
	}
	if patch.SkipAutoResumeSeededSearch != nil {
		settings.SkipAutoResumeSeededSearch = *patch.SkipAutoResumeSeededSearch
	}
	if patch.SkipAutoResumeCompletion != nil {
		settings.SkipAutoResumeCompletion = *patch.SkipAutoResumeCompletion
	}
	if patch.SkipAutoResumeWebhook != nil {
		settings.SkipAutoResumeWebhook = *patch.SkipAutoResumeWebhook
	}
	if patch.SkipRecheck != nil {
		settings.SkipRecheck = *patch.SkipRecheck
	}
}

type automationRunRequest struct {
	DryRun bool `json:"dryRun"`
}

type searchRunRequest struct {
	InstanceID      int      `json:"instanceId"`
	Categories      []string `json:"categories"`
	Tags            []string `json:"tags"`
	IntervalSeconds int      `json:"intervalSeconds"`
	IndexerIDs      []int    `json:"indexerIds"`
	CooldownMinutes int      `json:"cooldownMinutes"`

	// TODO: Surface remaining crossseed.SearchRunOptions fields (e.g. FindIndividualEpisodes,
	// StartPaused, and category/tag overrides) when the API needs to expose them per run.
}

// NewCrossSeedHandler creates a new cross-seed handler
func NewCrossSeedHandler(service *crossseed.Service, completionStore *models.InstanceCrossSeedCompletionStore, instanceStore *models.InstanceStore) *CrossSeedHandler {
	return &CrossSeedHandler{
		service:         service,
		completionStore: completionStore,
		instanceStore:   instanceStore,
	}
}

// Routes registers the cross-seed routes
func (h *CrossSeedHandler) Routes(r chi.Router) {
	// Register instance-scoped route at top level
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
		r.Patch("/settings", h.PatchAutomationSettings)
		r.Put("/settings", h.UpdateAutomationSettings)
		r.Get("/status", h.GetAutomationStatus)
		r.Get("/runs", h.ListAutomationRuns)
		r.Post("/run", h.TriggerAutomationRun)
		r.Route("/search", func(r chi.Router) {
			r.Get("/settings", h.GetSearchSettings)
			r.Patch("/settings", h.PatchSearchSettings)
			r.Get("/status", h.GetSearchRunStatus)
			r.Post("/run", h.StartSearchRun)
			r.Post("/run/cancel", h.CancelSearchRun)
			r.Get("/runs", h.ListSearchRunHistory)
		})
		r.Route("/completion", func(r chi.Router) {
			r.Get("/{instanceID}", h.GetInstanceCompletionSettings)
			r.Put("/{instanceID}", h.UpdateInstanceCompletionSettings)
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
		status := mapCrossSeedErrorStatus(err)
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
		status := mapCrossSeedErrorStatus(err)
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

	ctx := jackett.WithSearchPriority(r.Context(), jackett.RateLimitPriorityInteractive)
	response, err := h.service.SearchTorrentMatches(ctx, instanceID, hash, opts)
	if err != nil {
		status := mapCrossSeedErrorStatus(err)
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
// @Description Accepts a torrent file from autobrr, matches it against the requested instances (or all instances when instanceIds is omitted), and adds it with alignment wherever a match is found.
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

	response, err := h.service.AutobrrApply(context.WithoutCancel(r.Context()), &req)
	if err != nil {
		status := mapCrossSeedErrorStatus(err)
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

	response, err := h.service.ApplyTorrentSearchResults(context.WithoutCancel(r.Context()), instanceID, hash, &req)
	if err != nil {
		status := mapCrossSeedErrorStatus(err)
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

func mapCrossSeedErrorStatus(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, crossseed.ErrInvalidRequest),
		errors.Is(err, crossseed.ErrTorrentNotFound),
		errors.Is(err, crossseed.ErrTorrentNotComplete),
		errors.Is(err, crossseed.ErrNoIndexersConfigured):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
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

	// Validate mutual exclusivity: cannot use both indexer category and .cross suffix
	if req.UseCategoryFromIndexer && req.UseCrossCategorySuffix {
		RespondError(w, http.StatusBadRequest, "Cannot enable both 'Use indexer name as category' and 'Add .cross category suffix'. These settings are mutually exclusive.")
		return
	}

	settings := &models.CrossSeedAutomationSettings{
		Enabled:                      req.Enabled,
		RunIntervalMinutes:           req.RunIntervalMinutes,
		StartPaused:                  req.StartPaused,
		Category:                     category,
		IgnorePatterns:               req.IgnorePatterns,
		TargetInstanceIDs:            req.TargetInstanceIDs,
		TargetIndexerIDs:             req.TargetIndexerIDs,
		MaxResultsPerRun:             req.MaxResultsPerRun,
		FindIndividualEpisodes:       req.FindIndividualEpisodes,
		SizeMismatchTolerancePercent: req.SizeMismatchTolerancePercent,
		UseCategoryFromIndexer:       req.UseCategoryFromIndexer,
		UseCrossCategorySuffix:       req.UseCrossCategorySuffix,
		RunExternalProgramID:         req.RunExternalProgramID,
		SkipRecheck:                  req.SkipRecheck,
	}

	updated, err := h.service.UpdateAutomationSettings(r.Context(), settings)
	if err != nil {
		status := mapCrossSeedErrorStatus(err)
		log.Error().Err(err).Msg("Failed to update cross-seed automation settings")
		if status == http.StatusBadRequest {
			RespondError(w, status, err.Error())
		} else {
			RespondError(w, status, "Failed to update automation settings")
		}
		return
	}

	RespondJSON(w, http.StatusOK, updated)
}

// PatchAutomationSettings merges updates into the existing cross-seed configuration.
// PatchAutomationSettings godoc
// @Summary Patch cross-seed automation settings
// @Description Partially update automation, completion, or global cross-seed settings without overwriting unspecified fields
// @Tags cross-seed
// @Accept json
// @Produce json
// @Param request body automationSettingsPatchRequest true "Automation settings fields to update"
// @Success 200 {object} models.CrossSeedAutomationSettings
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/cross-seed/settings [patch]
func (h *CrossSeedHandler) PatchAutomationSettings(w http.ResponseWriter, r *http.Request) {
	var req automationSettingsPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.isEmpty() {
		RespondError(w, http.StatusBadRequest, "No fields provided to update")
		return
	}

	// Log what the API received for debugging source filter issues
	if req.RSSSourceCategories != nil || req.RSSSourceExcludeCategories != nil ||
		req.WebhookSourceCategories != nil || req.WebhookSourceExcludeCategories != nil {
		log.Debug().
			Interface("rssSourceCategories", req.RSSSourceCategories).
			Interface("rssSourceExcludeCategories", req.RSSSourceExcludeCategories).
			Interface("rssSourceTags", req.RSSSourceTags).
			Interface("rssSourceExcludeTags", req.RSSSourceExcludeTags).
			Interface("webhookSourceCategories", req.WebhookSourceCategories).
			Interface("webhookSourceExcludeCategories", req.WebhookSourceExcludeCategories).
			Msg("[API] Received source filter patch request")
	}

	current, err := h.service.GetAutomationSettings(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to load cross-seed automation settings for patch")
		RespondError(w, http.StatusInternalServerError, "Failed to load automation settings")
		return
	}

	merged := *current
	applyAutomationSettingsPatch(&merged, req)

	// Validate mutual exclusivity: cannot use both indexer category and .cross suffix
	if merged.UseCategoryFromIndexer && merged.UseCrossCategorySuffix {
		RespondError(w, http.StatusBadRequest, "Cannot enable both 'Use indexer name as category' and 'Add .cross category suffix'. These settings are mutually exclusive.")
		return
	}

	updated, err := h.service.UpdateAutomationSettings(r.Context(), &merged)
	if err != nil {
		status := mapCrossSeedErrorStatus(err)
		log.Error().Err(err).Msg("Failed to patch cross-seed automation settings")
		if status == http.StatusBadRequest {
			RespondError(w, status, err.Error())
		} else {
			RespondError(w, status, "Failed to update automation settings")
		}
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

	run, err := h.service.RunAutomation(context.WithoutCancel(r.Context()), crossseed.AutomationRunOptions{
		RequestedBy: "api",
		Mode:        models.CrossSeedRunModeManual,
		DryRun:      req.DryRun,
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
		if errors.Is(err, crossseed.ErrNoTargetInstancesConfigured) {
			RespondError(w, http.StatusBadRequest, "Select at least one target instance before running automation.")
			return
		}
		log.Error().Err(err).Msg("Failed to trigger cross-seed automation run")
		RespondError(w, http.StatusInternalServerError, "Failed to start automation run")
		return
	}

	RespondJSON(w, http.StatusAccepted, run)
}

// GetSearchSettings godoc
// @Summary Get seeded torrent search settings
// @Description Returns the persisted defaults used by Seeded Torrent Search runs.
// @Tags cross-seed
// @Produce json
// @Success 200 {object} models.CrossSeedSearchSettings
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/cross-seed/search/settings [get]
func (h *CrossSeedHandler) GetSearchSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.service.GetSearchSettings(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to load cross-seed search settings")
		RespondError(w, http.StatusInternalServerError, "Failed to load search settings")
		return
	}

	RespondJSON(w, http.StatusOK, settings)
}

// PatchSearchSettings godoc
// @Summary Update seeded torrent search settings
// @Description Persists default filters and timing for Seeded Torrent Search runs.
// @Tags cross-seed
// @Accept json
// @Produce json
// @Param request body searchSettingsPatchRequest true "Search settings patch"
// @Success 200 {object} models.CrossSeedSearchSettings
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/cross-seed/search/settings [patch]
func (h *CrossSeedHandler) PatchSearchSettings(w http.ResponseWriter, r *http.Request) {
	var patch searchSettingsPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if patch.isEmpty() {
		RespondError(w, http.StatusBadRequest, "No settings provided")
		return
	}

	updated, err := h.service.PatchSearchSettings(r.Context(), crossseed.SearchSettingsPatch{
		InstanceIDSet:   patch.InstanceID.Set,
		InstanceID:      patch.InstanceID.Value,
		Categories:      patch.Categories,
		Tags:            patch.Tags,
		IndexerIDs:      patch.IndexerIDs,
		IntervalSeconds: patch.IntervalSeconds,
		CooldownMinutes: patch.CooldownMinutes,
	})
	if err != nil {
		status := mapCrossSeedErrorStatus(err)
		log.Error().Err(err).Msg("Failed to update cross-seed search settings")
		RespondError(w, status, err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, updated)
}

// StartSearchRun godoc
// @Summary Start cross-seed search run
// @Description Launches an on-demand cross-seed library search for a specific instance with optional category, tag, and indexer filters.
// @Tags cross-seed
// @Accept json
// @Produce json
// @Param request body searchRunRequest true "Search run options"
// @Success 202 {object} models.CrossSeedSearchRun
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 409 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/cross-seed/search/run [post]
func (h *CrossSeedHandler) StartSearchRun(w http.ResponseWriter, r *http.Request) {
	var req searchRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.InstanceID <= 0 {
		RespondError(w, http.StatusBadRequest, "instanceId must be a positive integer")
		return
	}

	run, err := h.service.StartSearchRun(context.WithoutCancel(r.Context()), crossseed.SearchRunOptions{
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
		status := mapCrossSeedErrorStatus(err)
		log.Error().Err(err).Msg("Failed to start search run")
		RespondError(w, status, err.Error())
		return
	}

	RespondJSON(w, http.StatusAccepted, run)
}

// CancelSearchRun godoc
// @Summary Cancel cross-seed search run
// @Description Stops the currently running cross-seed library search, if any.
// @Tags cross-seed
// @Success 204
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/cross-seed/search/run/cancel [post]
func (h *CrossSeedHandler) CancelSearchRun(w http.ResponseWriter, r *http.Request) {
	h.service.CancelSearchRun()
	w.WriteHeader(http.StatusNoContent)
}

// GetSearchRunStatus godoc
// @Summary Get cross-seed search run status
// @Description Returns the state of the active or most recent cross-seed library search run.
// @Tags cross-seed
// @Produce json
// @Success 200 {object} crossseed.SearchRunStatus
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/cross-seed/search/status [get]
func (h *CrossSeedHandler) GetSearchRunStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.service.GetSearchRunStatus(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to get cross-seed search run status")
		RespondError(w, http.StatusInternalServerError, "Failed to get search run status")
		return
	}
	RespondJSON(w, http.StatusOK, status)
}

// ListSearchRunHistory godoc
// @Summary List cross-seed search run history
// @Description Lists historical cross-seed search runs for a specific instance.
// @Tags cross-seed
// @Produce json
// @Param instanceId query int true "Instance ID"
// @Param limit query int false "Page size (max 200)"
// @Param offset query int false "Result offset"
// @Success 200 {array} models.CrossSeedSearchRun
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/cross-seed/search/runs [get]
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
		log.Error().
			Err(err).
			Int("instanceID", instanceID).
			Int("limit", limit).
			Int("offset", offset).
			Msg("Failed to list cross-seed search runs")
		RespondError(w, http.StatusInternalServerError, "Failed to list search runs")
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
	if err != nil || instanceID <= 0 {
		RespondError(w, http.StatusBadRequest, "instanceID must be a positive integer")
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
// @Success 200 {object} crossseed.WebhookCheckResponse "Matches found (torrents complete, recommendation=download)"
// @Success 202 {object} crossseed.WebhookCheckResponse "Matches found but torrents still downloading (recommendation=download, retry until 200)"
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
		default:
			log.Error().Err(err).Msg("Failed to check webhook")
			RespondError(w, http.StatusInternalServerError, "Failed to check webhook")
			return
		}
	}

	RespondJSON(w, webhookResponseStatus(response), response)
}

func webhookResponseStatus(response *crossseed.WebhookCheckResponse) int {
	switch {
	case response == nil:
		return http.StatusInternalServerError
	case response.CanCrossSeed:
		return http.StatusOK
	case len(response.Matches) > 0:
		return http.StatusAccepted
	default:
		return http.StatusNotFound
	}
}

// instanceCompletionSettingsResponse is the API response for per-instance completion settings.
type instanceCompletionSettingsResponse struct {
	InstanceID        int      `json:"instanceId"`
	Enabled           bool     `json:"enabled"`
	Categories        []string `json:"categories"`
	Tags              []string `json:"tags"`
	ExcludeCategories []string `json:"excludeCategories"`
	ExcludeTags       []string `json:"excludeTags"`
}

// toInstanceCompletionSettingsResponse converts model to API response.
func toInstanceCompletionSettingsResponse(s *models.InstanceCrossSeedCompletionSettings) instanceCompletionSettingsResponse {
	return instanceCompletionSettingsResponse{
		InstanceID:        s.InstanceID,
		Enabled:           s.Enabled,
		Categories:        s.Categories,
		Tags:              s.Tags,
		ExcludeCategories: s.ExcludeCategories,
		ExcludeTags:       s.ExcludeTags,
	}
}

// instanceCompletionSettingsRequest is the API request for updating per-instance completion settings.
type instanceCompletionSettingsRequest struct {
	Enabled           bool     `json:"enabled"`
	Categories        []string `json:"categories"`
	Tags              []string `json:"tags"`
	ExcludeCategories []string `json:"excludeCategories"`
	ExcludeTags       []string `json:"excludeTags"`
}

// GetInstanceCompletionSettings returns the completion settings for a specific instance.
func (h *CrossSeedHandler) GetInstanceCompletionSettings(w http.ResponseWriter, r *http.Request) {
	instanceIDStr := chi.URLParam(r, "instanceID")
	instanceID, err := strconv.Atoi(instanceIDStr)
	if err != nil || instanceID <= 0 {
		RespondError(w, http.StatusBadRequest, "instanceID must be a positive integer")
		return
	}

	if h.completionStore == nil {
		log.Error().Int("instanceID", instanceID).Msg("Completion store not configured")
		RespondError(w, http.StatusServiceUnavailable, "Completion settings not available")
		return
	}

	// Validate instance exists
	if h.instanceStore != nil {
		_, err := h.instanceStore.Get(r.Context(), instanceID)
		if err != nil {
			if errors.Is(err, models.ErrInstanceNotFound) {
				log.Warn().Int("instanceID", instanceID).Msg("Instance not found for completion settings")
				RespondError(w, http.StatusNotFound, "Instance not found")
				return
			}
			log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to validate instance for completion settings")
			RespondError(w, http.StatusInternalServerError, "Failed to validate instance")
			return
		}
	}

	settings, err := h.completionStore.Get(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get instance completion settings")
		RespondError(w, http.StatusInternalServerError, "Failed to load completion settings")
		return
	}

	RespondJSON(w, http.StatusOK, toInstanceCompletionSettingsResponse(settings))
}

// UpdateInstanceCompletionSettings updates the completion settings for a specific instance.
func (h *CrossSeedHandler) UpdateInstanceCompletionSettings(w http.ResponseWriter, r *http.Request) {
	instanceIDStr := chi.URLParam(r, "instanceID")
	instanceID, err := strconv.Atoi(instanceIDStr)
	if err != nil || instanceID <= 0 {
		RespondError(w, http.StatusBadRequest, "instanceID must be a positive integer")
		return
	}

	if h.completionStore == nil {
		log.Error().Int("instanceID", instanceID).Msg("Completion store not configured")
		RespondError(w, http.StatusServiceUnavailable, "Completion settings not available")
		return
	}

	// Validate instance exists
	if h.instanceStore != nil {
		_, err := h.instanceStore.Get(r.Context(), instanceID)
		if err != nil {
			if errors.Is(err, models.ErrInstanceNotFound) {
				log.Warn().Int("instanceID", instanceID).Msg("Instance not found for completion settings")
				RespondError(w, http.StatusNotFound, "Instance not found")
				return
			}
			log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to validate instance for completion settings")
			RespondError(w, http.StatusInternalServerError, "Failed to validate instance")
			return
		}
	}

	var req instanceCompletionSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode instance completion settings request")
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	settings := &models.InstanceCrossSeedCompletionSettings{
		InstanceID:        instanceID,
		Enabled:           req.Enabled,
		Categories:        req.Categories,
		Tags:              req.Tags,
		ExcludeCategories: req.ExcludeCategories,
		ExcludeTags:       req.ExcludeTags,
	}

	saved, err := h.completionStore.Upsert(r.Context(), settings)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to save instance completion settings")
		RespondError(w, http.StatusInternalServerError, "Failed to save completion settings")
		return
	}

	RespondJSON(w, http.StatusOK, toInstanceCompletionSettingsResponse(saved))
}
