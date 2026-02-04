// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/redact"
)

const (
	defaultRecentSearchLimit = 10
	maxRecentSearchLimit     = 50

	testStatusUpdateTimeout = 5 * time.Second
)

// IndexerResponse wraps an indexer with optional warnings for partial failures
type IndexerResponse struct {
	*models.TorznabIndexer
	Warnings []string `json:"warnings,omitempty"`
}

// JackettHandler handles Jackett/Torznab API endpoints
type JackettHandler struct {
	service      *jackett.Service
	indexerStore *models.TorznabIndexerStore
}

// NewJackettHandler creates a new Jackett handler
func NewJackettHandler(service *jackett.Service, indexerStore *models.TorznabIndexerStore) *JackettHandler {
	return &JackettHandler{
		service:      service,
		indexerStore: indexerStore,
	}
}

// Routes registers the Jackett routes
func (h *JackettHandler) Routes(r chi.Router) {
	r.Route("/torznab", func(r chi.Router) {
		// Indexer management
		r.Route("/indexers", func(r chi.Router) {
			r.Get("/", h.ListIndexers)
			r.Post("/", h.CreateIndexer)
			r.Post("/discover", h.DiscoverIndexers)
			r.Get("/health", h.GetAllHealth)
			r.Get("/{indexerID}", h.GetIndexer)
			r.Put("/{indexerID}", h.UpdateIndexer)
			r.Delete("/{indexerID}", h.DeleteIndexer)
			r.Post("/{indexerID}/test", h.TestIndexer)
			r.Post("/{indexerID}/caps/sync", h.SyncIndexerCaps)
			r.Get("/{indexerID}/health", h.GetIndexerHealth)
			r.Get("/{indexerID}/errors", h.GetIndexerErrors)
			r.Get("/{indexerID}/stats", h.GetIndexerStats)
		})

		// Cross-seed search - intelligent category detection
		r.Post("/cross-seed/search", h.CrossSeedSearch)

		// General Torznab search
		r.Get("/search/recent", h.ListRecentSearches)
		r.Post("/search", h.Search)

		r.Route("/search/cache", func(r chi.Router) {
			r.Get("/", h.GetSearchCacheStats)
			r.Put("/settings", h.UpdateSearchCacheSettings)
		})

		// Search history - completed searches
		r.Get("/search/history", h.GetSearchHistory)

		// Activity status
		r.Get("/activity", h.GetActivityStatus)
	})
}

// CrossSeedSearch godoc
// @Summary Search Jackett for cross-seeds with intelligent category detection
// @Description Searches Jackett indexers for potential cross-seeds. Automatically detects content type (TV shows, movies, daily shows, XXX, etc.) and applies appropriate Torznab categories based on the search parameters.
// @Tags torznab
// @Accept json
// @Produce json
// @Param request body jackett.TorznabSearchRequest true "Cross-seed search request"
// @Success 200 {object} jackett.SearchResponse
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/torznab/cross-seed/search [post]
func (h *JackettHandler) CrossSeedSearch(w http.ResponseWriter, r *http.Request) {
	var req jackett.TorznabSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode cross-seed search request")
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate request - require either query or advanced parameters
	hasAdvancedParams := req.IMDbID != "" || req.TVDbID != "" || req.TMDbID > 0 || req.TVMazeID > 0 ||
		req.Artist != "" || req.Album != "" || req.Year > 0 || req.Season != nil || req.Episode != nil

	if req.Query == "" && !hasAdvancedParams {
		RespondError(w, http.StatusBadRequest, "query or advanced parameters (imdb_id, tvdb_id, tmdb_id, tvmaze_id, artist, album, year, season, episode) are required")
		return
	}

	// Search for cross-seeds
	respCh := make(chan *jackett.SearchResponse, 1)
	errCh := make(chan error, 1)
	req.OnAllComplete = func(resp *jackett.SearchResponse, err error) {
		if err != nil {
			errCh <- err
		} else {
			respCh <- resp
		}
	}
	err := h.service.Search(r.Context(), &req)
	if err != nil {
		log.Error().
			Err(err).
			Str("query", req.Query).
			Msg("Failed to search Jackett for cross-seeds")
		RespondError(w, http.StatusInternalServerError, "Failed to search for cross-seeds")
		return
	}

	var response *jackett.SearchResponse
	select {
	case response = <-respCh:
		// continue
	case err := <-errCh:
		log.Error().
			Err(err).
			Str("query", req.Query).
			Msg("Failed to search Jackett for cross-seeds")
		RespondError(w, http.StatusInternalServerError, "Failed to search for cross-seeds")
		return
	case <-time.After(5 * time.Minute):
		log.Error().
			Str("query", req.Query).
			Msg("Cross-seed search timed out")
		RespondError(w, http.StatusInternalServerError, "Search timed out")
		return
	}

	RespondJSON(w, http.StatusOK, response)
}

// Search godoc
// @Summary General Torznab search
// @Description Performs a general Torznab search across Jackett indexers. Allows specifying categories, IMDb/TVDb IDs, and other search parameters.
// @Tags torznab
// @Accept json
// @Produce json
// @Param request body jackett.TorznabSearchRequest true "Torznab search request"
// @Success 200 {object} jackett.SearchResponse
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/torznab/search [post]
func (h *JackettHandler) Search(w http.ResponseWriter, r *http.Request) {
	var req jackett.TorznabSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode search request")
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate request - require either query or advanced parameters
	hasAdvancedParams := req.IMDbID != "" || req.TVDbID != "" || req.TMDbID > 0 || req.TVMazeID > 0 ||
		req.Artist != "" || req.Album != "" || req.Year > 0 || req.Season != nil || req.Episode != nil

	if req.Query == "" && !hasAdvancedParams {
		RespondError(w, http.StatusBadRequest, "query or advanced parameters (imdb_id, tvdb_id, tmdb_id, tvmaze_id, artist, album, year, season, episode) are required")
		return
	}

	// Perform search
	respCh := make(chan *jackett.SearchResponse, 1)
	errCh := make(chan error, 1)
	req.OnAllComplete = func(resp *jackett.SearchResponse, err error) {
		if err != nil {
			errCh <- err
		} else {
			respCh <- resp
		}
	}
	err := h.service.SearchGeneric(r.Context(), &req)
	if err != nil {
		log.Error().
			Err(err).
			Str("query", req.Query).
			Msg("Failed to search Jackett")
		RespondError(w, http.StatusInternalServerError, "Failed to search")
		return
	}

	var response *jackett.SearchResponse
	select {
	case response = <-respCh:
		// continue
	case err := <-errCh:
		log.Error().
			Err(err).
			Str("query", req.Query).
			Msg("Failed to search Jackett")
		RespondError(w, http.StatusInternalServerError, "Failed to search")
		return
	case <-time.After(5 * time.Minute):
		log.Error().
			Str("query", req.Query).
			Msg("Search timed out")
		RespondError(w, http.StatusInternalServerError, "Search timed out")
		return
	}

	RespondJSON(w, http.StatusOK, response)
}

// ListRecentSearches returns the latest cached search queries for autocomplete support.
func (h *JackettHandler) ListRecentSearches(w http.ResponseWriter, r *http.Request) {
	limit := defaultRecentSearchLimit
	if limitParam := strings.TrimSpace(r.URL.Query().Get("limit")); limitParam != "" {
		if parsed, err := strconv.Atoi(limitParam); err == nil {
			switch {
			case parsed <= 0:
				// keep default
			case parsed > maxRecentSearchLimit:
				limit = maxRecentSearchLimit
			default:
				limit = parsed
			}
		}
	}

	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	searches, err := h.service.GetRecentSearches(r.Context(), scope, limit)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load recent torznab searches")
		RespondError(w, http.StatusInternalServerError, "Failed to load recent searches")
		return
	}

	RespondJSON(w, http.StatusOK, searches)
}

// GetSearchCacheStats returns summary metrics for the search cache.
func (h *JackettHandler) GetSearchCacheStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.service.GetSearchCacheStats(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to load torznab search cache stats")
		RespondError(w, http.StatusInternalServerError, "Failed to load cache stats")
		return
	}

	RespondJSON(w, http.StatusOK, stats)
}

// UpdateSearchCacheSettings updates TTL configuration via the API.
func (h *JackettHandler) UpdateSearchCacheSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TTLMinutes int `json:"ttlMinutes"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.TTLMinutes < jackett.MinSearchCacheTTLMinutes {
		RespondError(w, http.StatusBadRequest, fmt.Sprintf("ttlMinutes must be at least %d", jackett.MinSearchCacheTTLMinutes))
		return
	}

	if _, err := h.service.UpdateSearchCacheSettings(r.Context(), req.TTLMinutes); err != nil {
		log.Error().Err(err).Msg("Failed to update torznab search cache settings")
		RespondError(w, http.StatusInternalServerError, "Failed to update cache settings")
		return
	}

	stats, err := h.service.GetSearchCacheStats(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Cache settings updated but failed to reload stats")
		RespondError(w, http.StatusInternalServerError, "Updated settings but failed to reload stats")
		return
	}

	RespondJSON(w, http.StatusOK, stats)
}

// ListIndexers godoc
// @Summary List all Torznab indexers
// @Description Retrieves all configured Torznab indexers
// @Tags torznab
// @Produce json
// @Success 200 {array} models.TorznabIndexer
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/torznab/indexers [get]
func (h *JackettHandler) ListIndexers(w http.ResponseWriter, r *http.Request) {
	indexers, err := h.indexerStore.List(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to list indexers")
		RespondError(w, http.StatusInternalServerError, "Failed to list indexers")
		return
	}

	RespondJSON(w, http.StatusOK, indexers)
}

// CreateIndexer godoc
// @Summary Create a new Torznab indexer
// @Description Creates a new Torznab indexer configuration
// @Tags torznab
// @Accept json
// @Produce json
// @Success 201 {object} models.TorznabIndexer
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/torznab/indexers [post]
func (h *JackettHandler) CreateIndexer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name           string                          `json:"name"`
		BaseURL        string                          `json:"base_url"`
		IndexerID      string                          `json:"indexer_id"`
		APIKey         string                          `json:"api_key"`
		Backend        string                          `json:"backend"`
		Enabled        *bool                           `json:"enabled"`
		Priority       *int                            `json:"priority"`
		TimeoutSeconds *int                            `json:"timeout_seconds"`
		Capabilities   []string                        `json:"capabilities,omitempty"`
		Categories     []models.TorznabIndexerCategory `json:"categories,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode create indexer request")
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	name := strings.TrimSpace(req.Name)
	baseURL := strings.TrimSpace(req.BaseURL)
	apiKey := strings.TrimSpace(req.APIKey)
	indexerID := strings.TrimSpace(req.IndexerID)

	// Validate required fields
	if name == "" {
		RespondError(w, http.StatusBadRequest, "name is required")
		return
	}
	if baseURL == "" {
		RespondError(w, http.StatusBadRequest, "base_url is required")
		return
	}
	if apiKey == "" {
		RespondError(w, http.StatusBadRequest, "api_key is required")
		return
	}

	backend, err := models.ParseTorznabBackend(req.Backend)
	if err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	if backend == models.TorznabBackendProwlarr && indexerID == "" {
		RespondError(w, http.StatusBadRequest, "indexer_id is required when backend is prowlarr")
		return
	}

	// Apply defaults
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	priority := 0
	if req.Priority != nil {
		priority = *req.Priority
	}
	timeoutSeconds := 30
	if req.TimeoutSeconds != nil {
		timeoutSeconds = *req.TimeoutSeconds
		if timeoutSeconds <= 0 {
			RespondError(w, http.StatusBadRequest, "timeout_seconds must be greater than 0")
			return
		}
	}

	indexer, err := h.indexerStore.CreateWithIndexerID(r.Context(), name, baseURL, indexerID, apiKey, enabled, priority, timeoutSeconds, backend)
	if err != nil {
		if errors.Is(err, models.ErrTorznabIndexerIDRequired) {
			RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
		log.Error().Err(err).Msg("Failed to create indexer")
		RespondError(w, http.StatusInternalServerError, "Failed to create indexer")
		return
	}

	// Track warnings for partial failures
	var warnings []string

	// If capabilities or categories were provided in the request, store them directly
	if len(req.Capabilities) > 0 || len(req.Categories) > 0 {
		if len(req.Capabilities) > 0 {
			if err := h.indexerStore.SetCapabilities(r.Context(), indexer.ID, req.Capabilities); err != nil {
				log.Warn().
					Err(err).
					Int("indexer_id", indexer.ID).
					Str("indexer", indexer.Name).
					Msg("Failed to store provided capabilities")
				warnings = append(warnings, "Failed to store capabilities - sync manually later")
			}
		}
		if len(req.Categories) > 0 {
			if err := h.indexerStore.SetCategories(r.Context(), indexer.ID, req.Categories); err != nil {
				log.Warn().
					Err(err).
					Int("indexer_id", indexer.ID).
					Str("indexer", indexer.Name).
					Msg("Failed to store provided categories")
				warnings = append(warnings, "Failed to store categories - sync manually later")
			}
		}
		// Reload indexer to include the stored capabilities and categories
		if updated, err := h.indexerStore.Get(r.Context(), indexer.ID); err != nil {
			log.Warn().
				Err(err).
				Int("indexer_id", indexer.ID).
				Str("indexer", indexer.Name).
				Msg("Failed to reload indexer after storing capabilities")
		} else {
			indexer = updated
		}
	} else if h.service != nil {
		// No capabilities/categories provided, try to fetch them from the service
		if updated, err := h.service.SyncIndexerCaps(r.Context(), indexer.ID); err != nil {
			log.Warn().
				Err(err).
				Int("indexer_id", indexer.ID).
				Str("indexer", indexer.Name).
				Msg("Failed to sync torznab caps after creation")
			warnings = append(warnings, "Failed to fetch capabilities - sync manually later")
		} else if updated != nil {
			indexer = updated
		}
	}

	RespondJSON(w, http.StatusCreated, IndexerResponse{TorznabIndexer: indexer, Warnings: warnings})
}

// GetIndexer godoc
// @Summary Get a Torznab indexer
// @Description Retrieves a specific Torznab indexer by ID
// @Tags torznab
// @Produce json
// @Param indexerID path int true "Indexer ID"
// @Success 200 {object} models.TorznabIndexer
// @Failure 404 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/torznab/indexers/{indexerID} [get]
func (h *JackettHandler) GetIndexer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "indexerID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid indexer ID")
		return
	}

	indexer, err := h.indexerStore.Get(r.Context(), id)
	if err != nil {
		if err == models.ErrTorznabIndexerNotFound {
			RespondError(w, http.StatusNotFound, "Indexer not found")
			return
		}
		log.Error().Err(err).Int("indexer_id", id).Msg("Failed to get indexer")
		RespondError(w, http.StatusInternalServerError, "Failed to get indexer")
		return
	}

	RespondJSON(w, http.StatusOK, indexer)
}

// UpdateIndexer godoc
// @Summary Update a Torznab indexer
// @Description Updates an existing Torznab indexer
// @Tags torznab
// @Accept json
// @Produce json
// @Param indexerID path int true "Indexer ID"
// @Success 200 {object} models.TorznabIndexer
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 404 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/torznab/indexers/{indexerID} [put]
func (h *JackettHandler) UpdateIndexer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "indexerID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid indexer ID")
		return
	}

	var req struct {
		Name           string                          `json:"name"`
		BaseURL        string                          `json:"base_url"`
		IndexerID      *string                         `json:"indexer_id"`
		APIKey         string                          `json:"api_key"`
		Backend        *string                         `json:"backend"`
		Enabled        *bool                           `json:"enabled"`
		Priority       *int                            `json:"priority"`
		TimeoutSeconds *int                            `json:"timeout_seconds"`
		Capabilities   []string                        `json:"capabilities,omitempty"`
		Categories     []models.TorznabIndexerCategory `json:"categories,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode update indexer request")
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	trimmedName := strings.TrimSpace(req.Name)
	trimmedBaseURL := strings.TrimSpace(req.BaseURL)
	trimmedAPIKey := strings.TrimSpace(req.APIKey)

	params := models.TorznabIndexerUpdateParams{
		Name:     trimmedName,
		BaseURL:  trimmedBaseURL,
		APIKey:   trimmedAPIKey,
		Enabled:  req.Enabled,
		Priority: req.Priority,
	}

	if req.TimeoutSeconds != nil {
		if *req.TimeoutSeconds <= 0 {
			RespondError(w, http.StatusBadRequest, "timeout_seconds must be greater than 0")
			return
		}
		params.TimeoutSeconds = req.TimeoutSeconds
	}

	if req.IndexerID != nil {
		trimmedIndexerID := strings.TrimSpace(*req.IndexerID)
		params.IndexerID = &trimmedIndexerID
	}

	if req.Backend != nil {
		backend, err := models.ParseTorznabBackend(*req.Backend)
		if err != nil {
			RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Backend = &backend
	}

	indexer, err := h.indexerStore.Update(r.Context(), id, params)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrTorznabIndexerNotFound):
			RespondError(w, http.StatusNotFound, "Indexer not found")
			return
		case errors.Is(err, models.ErrTorznabIndexerIDRequired):
			RespondError(w, http.StatusBadRequest, err.Error())
			return
		default:
			log.Error().Err(err).Int("indexer_id", id).Msg("Failed to update indexer")
			RespondError(w, http.StatusInternalServerError, "Failed to update indexer")
			return
		}
	}

	// Track warnings for partial failures
	var warnings []string

	// If capabilities or categories were provided in the request, store them directly
	if len(req.Capabilities) > 0 || len(req.Categories) > 0 {
		if len(req.Capabilities) > 0 {
			if err := h.indexerStore.SetCapabilities(r.Context(), indexer.ID, req.Capabilities); err != nil {
				log.Warn().
					Err(err).
					Int("indexer_id", indexer.ID).
					Str("indexer", indexer.Name).
					Msg("Failed to store provided capabilities")
				warnings = append(warnings, "Failed to store capabilities - sync manually later")
			}
		}
		if len(req.Categories) > 0 {
			if err := h.indexerStore.SetCategories(r.Context(), indexer.ID, req.Categories); err != nil {
				log.Warn().
					Err(err).
					Int("indexer_id", indexer.ID).
					Str("indexer", indexer.Name).
					Msg("Failed to store provided categories")
				warnings = append(warnings, "Failed to store categories - sync manually later")
			}
		}
		// Reload indexer to include the stored capabilities and categories
		if updated, err := h.indexerStore.Get(r.Context(), indexer.ID); err != nil {
			log.Warn().
				Err(err).
				Int("indexer_id", indexer.ID).
				Str("indexer", indexer.Name).
				Msg("Failed to reload indexer after storing capabilities")
		} else {
			indexer = updated
		}
	} else if h.service != nil {
		// No capabilities/categories provided, try to fetch them from the service
		if updated, err := h.service.SyncIndexerCaps(r.Context(), indexer.ID); err != nil {
			log.Warn().
				Err(err).
				Int("indexer_id", indexer.ID).
				Str("indexer", indexer.Name).
				Msg("Failed to sync torznab caps after update")
			warnings = append(warnings, "Failed to fetch capabilities - sync manually later")
		} else if updated != nil {
			indexer = updated
		}
	}

	if h.service != nil {
		if _, err := h.service.InvalidateSearchCache(r.Context(), []int{indexer.ID}); err != nil {
			log.Warn().
				Err(err).
				Int("indexer_id", indexer.ID).
				Msg("Failed to invalidate search cache after indexer update")
		}
	}

	RespondJSON(w, http.StatusOK, IndexerResponse{TorznabIndexer: indexer, Warnings: warnings})
}

// DeleteIndexer godoc
// @Summary Delete a Torznab indexer
// @Description Deletes a Torznab indexer
// @Tags torznab
// @Param indexerID path int true "Indexer ID"
// @Success 204
// @Failure 404 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/torznab/indexers/{indexerID} [delete]
func (h *JackettHandler) DeleteIndexer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "indexerID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid indexer ID")
		return
	}

	err = h.indexerStore.Delete(r.Context(), id)
	if err != nil {
		if err == models.ErrTorznabIndexerNotFound {
			RespondError(w, http.StatusNotFound, "Indexer not found")
			return
		}
		log.Error().Err(err).Int("indexer_id", id).Msg("Failed to delete indexer")
		RespondError(w, http.StatusInternalServerError, "Failed to delete indexer")
		return
	}

	if h.service != nil {
		if _, err := h.service.InvalidateSearchCache(r.Context(), []int{id}); err != nil {
			log.Debug().
				Err(err).
				Int("indexer_id", id).
				Msg("Failed to invalidate search cache after indexer deletion")
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// TestIndexer godoc
// @Summary Test a Torznab indexer connection
// @Description Tests the connection to a Torznab indexer
// @Tags torznab
// @Param indexerID path int true "Indexer ID"
// @Success 200
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/torznab/indexers/{indexerID}/test [post]
func (h *JackettHandler) TestIndexer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "indexerID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid indexer ID")
		return
	}

	indexer, err := h.indexerStore.Get(r.Context(), id)
	if err != nil {
		if err == models.ErrTorznabIndexerNotFound {
			RespondError(w, http.StatusNotFound, "Indexer not found")
			return
		}
		log.Error().Err(err).Int("indexer_id", id).Msg("Failed to get indexer")
		RespondError(w, http.StatusInternalServerError, "Failed to get indexer")
		return
	}

	log.Debug().
		Int("indexer_id", id).
		Str("indexer_name", indexer.Name).
		Msg("Testing torznab indexer connectivity")

	// Run a lightweight search via the service to validate connectivity
	// Use CacheModeBypass and SkipHistory to prevent test searches from cluttering search history
	testReq := &jackett.TorznabSearchRequest{
		Query:       "test",
		Limit:       1,
		IndexerIDs:  []int{id},
		CacheMode:   jackett.CacheModeBypass,
		SkipHistory: true,
		OnAllComplete: func(*jackett.SearchResponse, error) {
			// Ignore results for connectivity test
		},
	}

	// Use a detached context for test searches - the HTTP request lifecycle should not
	// cancel the scheduler task since SearchGeneric returns immediately after scheduling.
	// Note: We intentionally don't defer cancel() here because the search is async.
	// The context will be cleaned up when the timeout expires.
	testCtx, _ := context.WithTimeout(context.Background(), 30*time.Second)

	err = h.service.SearchGeneric(testCtx, testReq)

	// Update test status in database
	if err != nil {
		errorMsg := err.Error()
		if updateErr := h.updateTestStatusWithTimeout(id, "error", &errorMsg); updateErr != nil {
			h.logTestStatusUpdateError(updateErr, id, "error")
		}

		log.Error().Err(err).Int("indexer_id", id).Msg("Failed to test indexer connection")
		RespondError(w, http.StatusInternalServerError, "Failed to connect to indexer")
		return
	}

	// Test succeeded - update status
	if updateErr := h.updateTestStatusWithTimeout(id, "ok", nil); updateErr != nil {
		h.logTestStatusUpdateError(updateErr, id, "ok")
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *JackettHandler) updateTestStatusWithTimeout(id int, status string, errorMsg *string) error {
	ctx, cancel := context.WithTimeout(context.Background(), testStatusUpdateTimeout)
	defer cancel()
	return h.indexerStore.UpdateTestStatus(ctx, id, status, errorMsg)
}

func (h *JackettHandler) logTestStatusUpdateError(err error, id int, status string) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		log.Debug().
			Err(err).
			Int("indexer_id", id).
			Str("status", status).
			Msg("Test status update canceled/timed out")
		return
	}

	log.Error().Err(err).Int("indexer_id", id).Msg("Failed to update test status")
}

// SyncIndexerCaps godoc
// @Summary Refresh Torznab caps from the backend
// @Description Fetches the latest capabilities and categories from Jackett, Prowlarr, or a native Torznab endpoint and persists them.
// @Tags torznab
// @Param indexerID path int true "Indexer ID"
// @Success 200 {object} models.TorznabIndexer
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 404 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/torznab/indexers/{indexerID}/caps/sync [post]
func (h *JackettHandler) SyncIndexerCaps(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		RespondError(w, http.StatusServiceUnavailable, "Jackett service not configured")
		return
	}

	id, err := strconv.Atoi(chi.URLParam(r, "indexerID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid indexer ID")
		return
	}

	indexer, err := h.service.SyncIndexerCaps(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrTorznabIndexerNotFound):
			RespondError(w, http.StatusNotFound, "Indexer not found")
			return
		case errors.Is(err, jackett.ErrMissingIndexerIdentifier):
			RespondError(w, http.StatusBadRequest, err.Error())
			return
		default:
			log.Error().Err(err).Int("indexer_id", id).Msg("Failed to sync torznab caps")
			RespondError(w, http.StatusInternalServerError, "Failed to sync caps")
			return
		}
	}

	if _, err := h.service.InvalidateSearchCache(r.Context(), []int{indexer.ID}); err != nil {
		log.Debug().
			Err(err).
			Int("indexer_id", indexer.ID).
			Msg("Failed to invalidate search cache after caps sync")
	}

	RespondJSON(w, http.StatusOK, indexer)
}

// DiscoverIndexers godoc
// @Summary Discover indexers from a Jackett/Prowlarr instance
// @Description Discovers all configured indexers from a Jackett or Prowlarr instance using its API
// @Tags torznab
// @Accept json
// @Produce json
// @Success 200 {object} jackett.DiscoveryResult
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/torznab/indexers/discover [post]
func (h *JackettHandler) DiscoverIndexers(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BaseURL string `json:"base_url"`
		APIKey  string `json:"api_key"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode discover request")
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.BaseURL == "" {
		RespondError(w, http.StatusBadRequest, "base_url is required")
		return
	}
	if req.APIKey == "" {
		RespondError(w, http.StatusBadRequest, "api_key is required")
		return
	}

	result, err := jackett.DiscoverJackettIndexers(r.Context(), req.BaseURL, req.APIKey)
	if err != nil {
		log.Error().Str("base_url", redact.URLString(req.BaseURL)).Str("error", redact.String(err.Error())).Msg("Failed to discover indexers")
		RespondError(w, http.StatusInternalServerError, "Failed to discover indexers")
		return
	}

	RespondJSON(w, http.StatusOK, result)
}

// GetAllHealth godoc
// @Summary Get health status for all indexers
// @Description Retrieves health statistics for all Torznab indexers
// @Tags torznab
// @Produce json
// @Success 200 {array} models.TorznabIndexerHealth
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/torznab/indexers/health [get]
func (h *JackettHandler) GetAllHealth(w http.ResponseWriter, r *http.Request) {
	health, err := h.indexerStore.GetAllHealth(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to get all indexer health")
		RespondError(w, http.StatusInternalServerError, "Failed to get health status")
		return
	}

	RespondJSON(w, http.StatusOK, health)
}

// GetIndexerHealth godoc
// @Summary Get health status for an indexer
// @Description Retrieves health statistics for a specific Torznab indexer
// @Tags torznab
// @Produce json
// @Param indexerID path int true "Indexer ID"
// @Success 200 {object} models.TorznabIndexerHealth
// @Failure 404 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/torznab/indexers/{indexerID}/health [get]
func (h *JackettHandler) GetIndexerHealth(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "indexerID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid indexer ID")
		return
	}

	health, err := h.indexerStore.GetHealth(r.Context(), id)
	if err != nil {
		if err == models.ErrTorznabIndexerNotFound {
			RespondError(w, http.StatusNotFound, "Indexer not found")
			return
		}
		log.Error().Err(err).Int("indexer_id", id).Msg("Failed to get indexer health")
		RespondError(w, http.StatusInternalServerError, "Failed to get health status")
		return
	}

	RespondJSON(w, http.StatusOK, health)
}

// GetIndexerErrors godoc
// @Summary Get recent errors for an indexer
// @Description Retrieves recent error history for a specific Torznab indexer
// @Tags torznab
// @Produce json
// @Param indexerID path int true "Indexer ID"
// @Param limit query int false "Maximum number of errors to return" default(50)
// @Success 200 {array} models.TorznabIndexerError
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/torznab/indexers/{indexerID}/errors [get]
func (h *JackettHandler) GetIndexerErrors(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "indexerID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid indexer ID")
		return
	}

	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	errors, err := h.indexerStore.GetRecentErrors(r.Context(), id, limit)
	if err != nil {
		log.Error().Err(err).Int("indexer_id", id).Msg("Failed to get indexer errors")
		RespondError(w, http.StatusInternalServerError, "Failed to get errors")
		return
	}

	RespondJSON(w, http.StatusOK, errors)
}

// GetIndexerStats godoc
// @Summary Get latency statistics for an indexer
// @Description Retrieves aggregated latency statistics for a specific Torznab indexer
// @Tags torznab
// @Produce json
// @Param indexerID path int true "Indexer ID"
// @Success 200 {array} models.TorznabIndexerLatencyStats
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/torznab/indexers/{indexerID}/stats [get]
func (h *JackettHandler) GetIndexerStats(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "indexerID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid indexer ID")
		return
	}

	stats, err := h.indexerStore.GetLatencyStats(r.Context(), id)
	if err != nil {
		log.Error().Err(err).Int("indexer_id", id).Msg("Failed to get indexer stats")
		RespondError(w, http.StatusInternalServerError, "Failed to get statistics")
		return
	}

	RespondJSON(w, http.StatusOK, stats)
}

// GetSearchHistory godoc
// @Summary Get search history
// @Description Returns recent completed searches from the in-memory history buffer
// @Tags torznab
// @Produce json
// @Param limit query int false "Maximum number of entries to return (default: 50, max: 500)"
// @Success 200 {object} jackett.SearchHistoryResponseWithOutcome
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/torznab/search/history [get]
func (h *JackettHandler) GetSearchHistory(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
			if limit > 500 {
				limit = 500
			}
		}
	}

	history, err := h.service.GetSearchHistory(r.Context(), limit)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get search history")
		RespondError(w, http.StatusInternalServerError, "Failed to get search history")
		return
	}

	RespondJSON(w, http.StatusOK, history)
}

// GetActivityStatus godoc
// @Summary Get scheduler and indexer activity status
// @Description Returns current scheduler state including queued tasks, in-flight jobs, and rate-limited indexers
// @Tags torznab
// @Produce json
// @Success 200 {object} jackett.ActivityStatus
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/torznab/activity [get]
func (h *JackettHandler) GetActivityStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.service.GetActivityStatus(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to get activity status")
		RespondError(w, http.StatusInternalServerError, "Failed to get activity status")
		return
	}

	RespondJSON(w, http.StatusOK, status)
}
