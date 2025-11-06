// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"slices"

	"github.com/moistari/rls"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/releases"
)

// IndexerStore defines the interface for indexer storage operations
type IndexerStore interface {
	Get(ctx context.Context, id int) (*models.TorznabIndexer, error)
	List(ctx context.Context) ([]*models.TorznabIndexer, error)
	ListEnabled(ctx context.Context) ([]*models.TorznabIndexer, error)
	GetDecryptedAPIKey(indexer *models.TorznabIndexer) (string, error)
	SetCapabilities(ctx context.Context, indexerID int, capabilities []string) error
	SetCategories(ctx context.Context, indexerID int, categories []models.TorznabIndexerCategory) error
	RecordLatency(ctx context.Context, indexerID int, operationType string, latencyMs int, success bool) error
	RecordError(ctx context.Context, indexerID int, errorMessage, errorCode string) error
	CountRequests(ctx context.Context, indexerID int, window time.Duration) (int, error)
	UpdateRequestLimits(ctx context.Context, indexerID int, hourly, daily *int) error
}

// Service provides Jackett integration for Torznab searching
type Service struct {
	indexerStore  IndexerStore
	releaseParser *releases.Parser
	rateLimiter   *RateLimiter
}

// ErrMissingIndexerIdentifier signals that the Torznab backend requires an indexer ID to fetch caps.
var ErrMissingIndexerIdentifier = errors.New("torznab indexer identifier is required for caps sync")

const (
	defaultRateLimitCooldown = 30 * time.Minute
)

// searchContext carries additional metadata about the current Torznab search.
type searchContext struct {
	categories  []int
	contentType contentType
	searchMode  string
}

// NewService creates a new Jackett service
func NewService(indexerStore IndexerStore) *Service {
	return &Service{
		indexerStore:  indexerStore,
		releaseParser: releases.NewDefaultParser(),
		rateLimiter:   NewRateLimiter(defaultMinRequestInterval),
	}
}

// GetIndexerName resolves a Torznab indexer ID to its configured name.
func (s *Service) GetIndexerName(ctx context.Context, id int) string {
	if id <= 0 {
		return ""
	}

	indexer, err := s.indexerStore.Get(ctx, id)
	if err != nil {
		log.Debug().
			Err(err).
			Int("indexer_id", id).
			Msg("Failed to resolve indexer name")
		return ""
	}
	if indexer == nil {
		return ""
	}

	return indexer.Name
}

// Search searches enabled Torznab indexers with intelligent category detection
func (s *Service) Search(ctx context.Context, req *TorznabSearchRequest) (*SearchResponse, error) {
	if req.Query == "" {
		return nil, fmt.Errorf("query is required")
	}

	var detectedType contentType
	// Auto-detect content type only if categories not provided
	if len(req.Categories) == 0 {
		detectedType = s.detectContentType(req)
		req.Categories = getCategoriesForContentType(detectedType)

		log.Debug().
			Str("query", req.Query).
			Int("content_type", int(detectedType)).
			Ints("categories", req.Categories).
			Msg("Auto-detected content type and categories")
	} else {
		// When categories are provided, try to infer content type from categories
		detectedType = detectContentTypeFromCategories(req.Categories)
		if detectedType == contentTypeUnknown {
			// Fallback to query-based detection
			detectedType = s.detectContentType(req)
		}
		log.Debug().
			Str("query", req.Query).
			Ints("categories", req.Categories).
			Int("inferred_content_type", int(detectedType)).
			Msg("Using provided categories with inferred content type")
	}

	// Get enabled indexers
	indexers, err := s.indexerStore.ListEnabled(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get enabled indexers: %w", err)
	}

	if len(indexers) == 0 {
		return &SearchResponse{
			Results: []SearchResult{},
			Total:   0,
		}, nil
	}

	// Build search parameters
	searchMode := searchModeForContentType(detectedType)
	params := s.buildSearchParams(req, searchMode)
	meta := &searchContext{
		categories:  append([]int(nil), req.Categories...),
		contentType: detectedType,
		searchMode:  searchMode,
	}

	// Search all enabled indexers
	allResults := s.searchMultipleIndexers(ctx, indexers, params, meta)

	// Convert results
	searchResults := s.convertResults(allResults)

	// Apply limit and offset
	total := len(searchResults)
	if req.Offset > 0 {
		if req.Offset >= len(searchResults) {
			searchResults = []SearchResult{}
		} else {
			searchResults = searchResults[req.Offset:]
		}
	}
	if req.Limit > 0 && len(searchResults) > req.Limit {
		searchResults = searchResults[:req.Limit]
	}

	return &SearchResponse{
		Results: searchResults,
		Total:   total,
	}, nil
}

// SearchGeneric performs a general Torznab search across specified or all enabled indexers
func (s *Service) SearchGeneric(ctx context.Context, req *TorznabSearchRequest) (*SearchResponse, error) {
	if req.Query == "" {
		return nil, fmt.Errorf("query is required")
	}

	var detectedType contentType
	if len(req.Categories) == 0 {
		detectedType = s.detectContentType(req)
		req.Categories = getCategoriesForContentType(detectedType)

		log.Debug().
			Str("query", req.Query).
			Int("content_type", int(detectedType)).
			Ints("categories", req.Categories).
			Msg("Auto-detected content type and categories for general search")
	} else {
		// When categories are provided, try to infer content type from categories
		detectedType = detectContentTypeFromCategories(req.Categories)
		if detectedType == contentTypeUnknown {
			// Fallback to query-based detection
			detectedType = s.detectContentType(req)
		}
		log.Debug().
			Str("query", req.Query).
			Ints("categories", req.Categories).
			Int("inferred_content_type", int(detectedType)).
			Msg("Using provided categories with inferred content type for general search")
	}

	searchMode := searchModeForContentType(detectedType)
	params := s.buildSearchParams(req, searchMode)
	meta := &searchContext{
		categories:  append([]int(nil), req.Categories...),
		contentType: detectedType,
		searchMode:  searchMode,
	}

	var indexersToSearch []*models.TorznabIndexer
	var err error

	// If specific indexer IDs requested, get those
	if len(req.IndexerIDs) > 0 {
		for _, id := range req.IndexerIDs {
			indexer, err := s.indexerStore.Get(ctx, id)
			if err != nil {
				log.Warn().
					Err(err).
					Int("indexer_id", id).
					Msg("Failed to get indexer")
				continue
			}
			if indexer.Enabled {
				indexersToSearch = append(indexersToSearch, indexer)
			}
		}
	} else {
		// Search all enabled indexers
		indexersToSearch, err = s.indexerStore.ListEnabled(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get enabled indexers: %w", err)
		}
	}

	if len(indexersToSearch) == 0 {
		return &SearchResponse{
			Results: []SearchResult{},
			Total:   0,
		}, nil
	}

	// Search all indexers
	allResults := s.searchMultipleIndexers(ctx, indexersToSearch, params, meta)

	// Convert and sort results
	searchResults := s.convertResults(allResults)

	return &SearchResponse{
		Results: searchResults,
		Total:   len(searchResults),
	}, nil
}

// GetIndexers retrieves all configured Torznab indexers
func (s *Service) GetIndexers(ctx context.Context) (*IndexersResponse, error) {
	indexers, err := s.indexerStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list indexers: %w", err)
	}

	indexerInfos := make([]IndexerInfo, 0, len(indexers))
	for _, idx := range indexers {
		indexerInfos = append(indexerInfos, IndexerInfo{
			ID:          strconv.Itoa(idx.ID),
			Name:        idx.Name,
			Description: idx.BaseURL,
			Type:        "torznab",
			Configured:  idx.Enabled,
			Categories:  []CategoryInfo{}, // Would need to query caps endpoint for each
		})
	}

	return &IndexersResponse{
		Indexers: indexerInfos,
	}, nil
}

// Recent fetches the latest releases across selected indexers without a search query.
func (s *Service) Recent(ctx context.Context, limit int, indexerIDs []int) (*SearchResponse, error) {
	if limit <= 0 {
		limit = 50
	}

	params := url.Values{}
	params.Set("t", "search")
	params.Set("limit", strconv.Itoa(limit))

	indexersToSearch, err := s.resolveIndexerSelection(ctx, indexerIDs)
	if err != nil {
		return nil, err
	}

	if len(indexersToSearch) == 0 {
		return &SearchResponse{
			Results: []SearchResult{},
			Total:   0,
		}, nil
	}

	results := s.searchMultipleIndexers(ctx, indexersToSearch, params, nil)
	searchResults := s.convertResults(results)

	if len(searchResults) > limit {
		searchResults = searchResults[:limit]
	}

	return &SearchResponse{
		Results: searchResults,
		Total:   len(searchResults),
	}, nil
}

// DownloadTorrent fetches the raw torrent bytes for a specific indexer result.
func (s *Service) DownloadTorrent(ctx context.Context, indexerID int, downloadURL string) ([]byte, error) {
	if indexerID <= 0 {
		return nil, fmt.Errorf("indexer ID must be positive")
	}

	indexer, err := s.indexerStore.Get(ctx, indexerID)
	if err != nil {
		return nil, fmt.Errorf("failed to load indexer %d: %w", indexerID, err)
	}

	apiKey, err := s.indexerStore.GetDecryptedAPIKey(indexer)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt API key for indexer %d: %w", indexerID, err)
	}

	client := NewClient(indexer.BaseURL, apiKey, indexer.Backend, indexer.TimeoutSeconds)
	data, err := client.Download(ctx, downloadURL)
	if err != nil {
		return nil, fmt.Errorf("torrent download failed: %w", err)
	}

	return data, nil
}

// SyncIndexerCaps fetches and persists Torznab capabilities and categories for an indexer.
func (s *Service) SyncIndexerCaps(ctx context.Context, indexerID int) (*models.TorznabIndexer, error) {
	if indexerID <= 0 {
		return nil, fmt.Errorf("indexer ID must be positive")
	}

	indexer, err := s.indexerStore.Get(ctx, indexerID)
	if err != nil {
		return nil, fmt.Errorf("load torznab indexer: %w", err)
	}

	apiKey, err := s.indexerStore.GetDecryptedAPIKey(indexer)
	if err != nil {
		return nil, fmt.Errorf("decrypt torznab api key: %w", err)
	}

	client := NewClient(indexer.BaseURL, apiKey, indexer.Backend, indexer.TimeoutSeconds)

	identifier, err := resolveCapsIdentifier(indexer)
	if err != nil {
		return nil, err
	}

	caps, err := client.FetchCaps(ctx, identifier)
	if err != nil {
		return nil, fmt.Errorf("fetch torznab caps: %w", err)
	}
	if caps == nil {
		return nil, fmt.Errorf("torznab caps response was empty")
	}

	if err := s.indexerStore.SetCapabilities(ctx, indexer.ID, caps.Capabilities); err != nil {
		return nil, fmt.Errorf("persist torznab capabilities: %w", err)
	}
	if err := s.indexerStore.SetCategories(ctx, indexer.ID, caps.Categories); err != nil {
		return nil, fmt.Errorf("persist torznab categories: %w", err)
	}

	updated, err := s.indexerStore.Get(ctx, indexer.ID)
	if err != nil {
		return nil, fmt.Errorf("reload torznab indexer: %w", err)
	}

	return updated, nil
}

// MapCategoriesToIndexerCapabilities maps requested categories to categories supported by the specific indexer
func (s *Service) MapCategoriesToIndexerCapabilities(ctx context.Context, indexer *models.TorznabIndexer, requestedCategories []int) []int {
	if len(requestedCategories) == 0 {
		return requestedCategories
	}

	// If indexer has no categories stored yet, return requested categories as-is
	if len(indexer.Categories) == 0 {
		return requestedCategories
	}

	// Build a map of available categories for this indexer
	availableCategories := make(map[int]struct{})
	parentCategories := make(map[int]struct{})

	for _, cat := range indexer.Categories {
		availableCategories[cat.CategoryID] = struct{}{}
		if cat.ParentCategory != nil {
			parentCategories[*cat.ParentCategory] = struct{}{}
		}
	}

	// Map requested categories to what this indexer supports
	mappedCategories := make([]int, 0, len(requestedCategories))

	for _, requestedCat := range requestedCategories {
		// Check if indexer directly supports this category
		if _, exists := availableCategories[requestedCat]; exists {
			mappedCategories = append(mappedCategories, requestedCat)
			continue
		}

		// Check if this is a parent category that the indexer supports
		if _, exists := parentCategories[requestedCat]; exists {
			mappedCategories = append(mappedCategories, requestedCat)
			continue
		}

		// Try to find a compatible category by checking parent categories
		parent := deriveParentCategory(requestedCat)
		if parent != requestedCat {
			if _, exists := availableCategories[parent]; exists {
				mappedCategories = append(mappedCategories, parent)
				continue
			}
			if _, exists := parentCategories[parent]; exists {
				mappedCategories = append(mappedCategories, parent)
				continue
			}
		}
	}

	// If no categories mapped, return the original requested categories
	// This allows the indexer restriction logic to handle the filtering
	if len(mappedCategories) == 0 {
		return requestedCategories
	}

	return mappedCategories
}

// searchMultipleIndexers searches multiple indexers in parallel and aggregates results
func (s *Service) searchMultipleIndexers(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) []Result {
	type indexerResult struct {
		results []Result
		err     error
	}

	resultsChan := make(chan indexerResult, len(indexers))

	for _, indexer := range indexers {
		go func(idx *models.TorznabIndexer) {
			// Get decrypted API key
			apiKey, err := s.indexerStore.GetDecryptedAPIKey(idx)
			if err != nil {
				log.Warn().
					Err(err).
					Int("indexer_id", idx.ID).
					Str("indexer", idx.Name).
					Msg("Failed to decrypt API key")
				resultsChan <- indexerResult{nil, err}
				return
			}

			// Create client for this indexer
			client := NewClient(idx.BaseURL, apiKey, idx.Backend, idx.TimeoutSeconds)

			// Convert url.Values to map[string]string (take first value for each key)
			paramsMap := make(map[string]string)
			for key, values := range params {
				if len(values) > 0 {
					paramsMap[key] = values[0]
				}
			}

			var searchFn func() ([]Result, error)
			switch idx.Backend {
			case models.TorznabBackendNative:
				if s.applyIndexerRestrictions(ctx, client, idx, "", meta, paramsMap) {
					resultsChan <- indexerResult{nil, nil}
					return
				}

				log.Debug().
					Int("indexer_id", idx.ID).
					Str("indexer_name", idx.Name).
					Str("base_url", idx.BaseURL).
					Str("backend", string(idx.Backend)).
					Msg("Searching native Torznab endpoint")
				searchFn = func() ([]Result, error) {
					return client.SearchDirect(paramsMap)
				}
			case models.TorznabBackendProwlarr:
				indexerID := strings.TrimSpace(idx.IndexerID)
				if indexerID == "" {
					log.Warn().
						Int("indexer_id", idx.ID).
						Str("indexer", idx.Name).
						Str("backend", string(idx.Backend)).
						Msg("Skipping prowlarr indexer without numeric identifier")
					resultsChan <- indexerResult{nil, fmt.Errorf("missing prowlarr indexer identifier")}
					return
				}

				if s.applyIndexerRestrictions(ctx, client, idx, indexerID, meta, paramsMap) {
					resultsChan <- indexerResult{nil, nil}
					return
				}

				log.Debug().
					Int("indexer_id", idx.ID).
					Str("indexer_name", idx.Name).
					Str("backend", string(idx.Backend)).
					Str("torznab_indexer_id", indexerID).
					Msg("Searching Prowlarr indexer")
				searchFn = func() ([]Result, error) {
					return client.Search(indexerID, paramsMap)
				}
			default:
				// Jackett/Prowlarr aggregator - use stored indexer_id
				indexerID := idx.IndexerID
				if indexerID == "" {
					indexerID = extractIndexerIDFromURL(idx.BaseURL, idx.Name)
				}
				if strings.TrimSpace(indexerID) == "" {
					log.Warn().
						Int("indexer_id", idx.ID).
						Str("indexer", idx.Name).
						Str("backend", string(idx.Backend)).
						Msg("Skipping indexer without resolved identifier")
					resultsChan <- indexerResult{nil, fmt.Errorf("missing indexer identifier")}
					return
				}

				if s.applyIndexerRestrictions(ctx, client, idx, indexerID, meta, paramsMap) {
					resultsChan <- indexerResult{nil, nil}
					return
				}

				log.Debug().
					Int("indexer_id", idx.ID).
					Str("indexer_name", idx.Name).
					Str("backend", string(idx.Backend)).
					Str("torznab_indexer_id", indexerID).
					Msg("Searching Torznab aggregator indexer")
				searchFn = func() ([]Result, error) {
					return client.Search(indexerID, paramsMap)
				}
			}
			if searchFn == nil {
				resultsChan <- indexerResult{nil, fmt.Errorf("no search function for indexer")}
				return
			}

			if err := s.rateLimiter.BeforeRequest(ctx, idx); err != nil {
				resultsChan <- indexerResult{nil, err}
				return
			}

			start := time.Now()
			results, err := searchFn()
			latencyMs := int(time.Since(start).Milliseconds())
			if recErr := s.indexerStore.RecordLatency(ctx, idx.ID, "search", latencyMs, err == nil); recErr != nil {
				log.Debug().Err(recErr).Int("indexer_id", idx.ID).Msg("Failed to record torznab latency")
			}

			if err != nil {
				if cooldown, reason := detectRateLimit(err); reason {
					s.handleRateLimit(ctx, idx, cooldown, err)
				}
				log.Warn().
					Err(err).
					Int("indexer_id", idx.ID).
					Str("indexer", idx.Name).
					Msg("Failed to search indexer")
				resultsChan <- indexerResult{nil, err}
				return
			}

			for i := range results {
				results[i].IndexerID = idx.ID
				if idx.Backend == models.TorznabBackendProwlarr {
					results[i].Tracker = idx.Name
				} else if strings.TrimSpace(results[i].Tracker) == "" {
					results[i].Tracker = idx.Name
				}
			}

			resultsChan <- indexerResult{results, nil}
		}(indexer)
	}

	// Collect all results
	var allResults []Result
	for i := 0; i < len(indexers); i++ {
		result := <-resultsChan
		if result.err == nil {
			allResults = append(allResults, result.results...)
		}
	}

	return allResults
}

func (s *Service) applyIndexerRestrictions(ctx context.Context, client *Client, idx *models.TorznabIndexer, identifier string, meta *searchContext, params map[string]string) bool {
	requiredCaps := requiredCapabilities(meta)
	requested := requestedCategories(meta, params)

	needCaps := len(requiredCaps) > 0 && len(idx.Capabilities) == 0
	needCategories := len(requested) > 0 && len(idx.Categories) == 0
	if needCaps || needCategories {
		s.ensureIndexerMetadata(ctx, client, idx, identifier, needCaps, needCategories)
	}

	// Check capabilities first - use enhanced capability checking if we have search parameters
	if len(requiredCaps) > 0 && len(idx.Capabilities) > 0 {
		// Try to build a TorznabSearchRequest from params for enhanced checking
		var searchReq *TorznabSearchRequest
		if meta != nil {
			searchReq = &TorznabSearchRequest{}
			if query, exists := params["q"]; exists {
				searchReq.Query = query
			}
			if imdbid, exists := params["imdbid"]; exists {
				searchReq.IMDbID = imdbid
			}
			if tvdbid, exists := params["tvdbid"]; exists {
				searchReq.TVDbID = tvdbid
			}
			if yearStr, exists := params["year"]; exists {
				if year, err := strconv.Atoi(yearStr); err == nil {
					searchReq.Year = year
				}
			}
			if seasonStr, exists := params["season"]; exists {
				if season, err := strconv.Atoi(seasonStr); err == nil {
					searchReq.Season = &season
				}
			}
			if epStr, exists := params["ep"]; exists {
				if episode, err := strconv.Atoi(epStr); err == nil {
					searchReq.Episode = &episode
				}
			}
		}

		// Get preferred capabilities based on search parameters
		var capsToCheck []string
		if searchReq != nil {
			capsToCheck = getPreferredCapabilities(searchReq, meta.searchMode)
		} else {
			capsToCheck = requiredCaps
		}

		// Use enhanced capability checking if we have preferred capabilities
		var hasRequiredCaps bool
		var usingEnhanced bool
		if len(capsToCheck) > len(requiredCaps) {
			hasRequiredCaps = supportsPreferredCapabilities(idx.Capabilities, capsToCheck)
			usingEnhanced = true
		} else {
			hasRequiredCaps = supportsAnyCapability(idx.Capabilities, requiredCaps)
		}

		if !hasRequiredCaps {
			log.Info().
				Int("indexer_id", idx.ID).
				Str("indexer", idx.Name).
				Strs("required_caps", requiredCaps).
				Strs("preferred_caps", capsToCheck).
				Strs("indexer_caps", idx.Capabilities).
				Bool("enhanced_checking", usingEnhanced).
				Msg("Skipping torznab indexer due to missing capabilities")
			return true
		} else if usingEnhanced {
			log.Debug().
				Int("indexer_id", idx.ID).
				Str("indexer", idx.Name).
				Strs("required_caps", requiredCaps).
				Strs("preferred_caps", capsToCheck).
				Strs("indexer_caps", idx.Capabilities).
				Msg("Using enhanced capability checking for indexer")
		}
	}

	// If no categories requested, continue with search
	if len(requested) == 0 {
		return false
	}

	// If indexer has no categories stored, continue (will use requested categories as-is)
	if len(idx.Categories) == 0 {
		return false
	}

	// Map requested categories to what this indexer actually supports
	mappedCategories := s.MapCategoriesToIndexerCapabilities(ctx, idx, requested)

	// Filter mapped categories through indexer's supported categories
	filtered, ok := filterCategoriesForIndexer(idx.Categories, mappedCategories)
	if !ok {
		log.Info().
			Int("indexer_id", idx.ID).
			Str("indexer", idx.Name).
			Ints("requested_categories", requested).
			Ints("mapped_categories", mappedCategories).
			Msg("Skipping torznab indexer due to unsupported categories")
		return true
	}

	// Update the params with the filtered categories
	params["cat"] = formatCategoryList(filtered)

	log.Debug().
		Int("indexer_id", idx.ID).
		Str("indexer", idx.Name).
		Ints("requested_categories", requested).
		Ints("mapped_categories", mappedCategories).
		Ints("filtered_categories", filtered).
		Msg("Applied category mapping and filtering for indexer")

	// Handle conditional parameter addition based on indexer capabilities
	s.applyCapabilitySpecificParams(idx, meta, params)

	return false
}

func (s *Service) applyCapabilitySpecificParams(idx *models.TorznabIndexer, meta *searchContext, params map[string]string) {
	if meta == nil || len(idx.Capabilities) == 0 {
		return
	}

	// Handle year parameter - only add if indexer supports parameter-specific year capability
	if yearStr, hasYear := params["_year"]; hasYear {
		delete(params, "_year") // Remove temporary storage

		var yearCapability string
		switch meta.searchMode {
		case "movie":
			yearCapability = "movie-search-year"
		case "tvsearch":
			yearCapability = "tv-search-year"
		}

		if yearCapability != "" && supportsAnyCapability(idx.Capabilities, []string{yearCapability}) {
			params["year"] = yearStr
			log.Debug().
				Int("indexer_id", idx.ID).
				Str("indexer", idx.Name).
				Str("search_mode", meta.searchMode).
				Str("year", yearStr).
				Str("capability", yearCapability).
				Msg("Adding year parameter - indexer supports capability-specific year search")
		} else {
			log.Debug().
				Int("indexer_id", idx.ID).
				Str("indexer", idx.Name).
				Str("search_mode", meta.searchMode).
				Str("year", yearStr).
				Str("missing_capability", yearCapability).
				Strs("indexer_caps", idx.Capabilities).
				Msg("Skipping year parameter - indexer does not support capability-specific year search")
		}
	}
}

func (s *Service) ensureIndexerMetadata(ctx context.Context, client *Client, idx *models.TorznabIndexer, identifier string, ensureCaps bool, ensureCategories bool) {
	if !ensureCaps && !ensureCategories {
		return
	}

	caps, err := client.FetchCaps(ctx, identifier)
	if err != nil {
		log.Debug().
			Err(err).
			Int("indexer_id", idx.ID).
			Str("indexer", idx.Name).
			Msg("Failed to fetch caps for torznab indexer")
		return
	}

	if ensureCaps && len(caps.Capabilities) > 0 {
		if err := s.indexerStore.SetCapabilities(ctx, idx.ID, caps.Capabilities); err != nil {
			log.Warn().
				Err(err).
				Int("indexer_id", idx.ID).
				Msg("Failed to persist torznab capabilities")
		} else {
			idx.Capabilities = caps.Capabilities
			log.Debug().
				Int("indexer_id", idx.ID).
				Str("indexer", idx.Name).
				Strs("capabilities", caps.Capabilities).
				Msg("Successfully fetched and stored indexer capabilities")
		}
	}

	if ensureCategories && len(caps.Categories) > 0 {
		if err := s.indexerStore.SetCategories(ctx, idx.ID, caps.Categories); err != nil {
			log.Warn().
				Err(err).
				Int("indexer_id", idx.ID).
				Msg("Failed to persist torznab categories")
		} else {
			idx.Categories = caps.Categories
		}
	}
}

func requestedCategories(meta *searchContext, params map[string]string) []int {
	if meta != nil && len(meta.categories) > 0 {
		return meta.categories
	}
	if catStr, ok := params["cat"]; ok {
		return parseCategoryList(catStr)
	}
	return nil
}

func parseCategoryList(value string) []int {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	categories := make([]int, 0, len(parts))
	for _, part := range parts {
		if id, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
			categories = append(categories, id)
		}
	}
	return categories
}

func formatCategoryList(categories []int) string {
	if len(categories) == 0 {
		return ""
	}
	parts := make([]string, len(categories))
	for i, cat := range categories {
		parts[i] = strconv.Itoa(cat)
	}
	return strings.Join(parts, ",")
}

func filterCategoriesForIndexer(indexerCats []models.TorznabIndexerCategory, requested []int) ([]int, bool) {
	if len(requested) == 0 {
		return nil, true
	}

	allowed := make(map[int]struct{}, len(indexerCats))
	parentsWithChildren := make(map[int]struct{})
	for _, cat := range indexerCats {
		allowed[cat.CategoryID] = struct{}{}
		if cat.ParentCategory != nil {
			parentsWithChildren[*cat.ParentCategory] = struct{}{}
		}
	}

	filtered := make([]int, 0, len(requested))
	for _, cat := range requested {
		if _, ok := allowed[cat]; ok {
			filtered = append(filtered, cat)
			continue
		}
		if _, ok := parentsWithChildren[cat]; ok {
			filtered = append(filtered, cat)
			continue
		}
		parent := deriveParentCategory(cat)
		if parent != cat {
			if _, ok := allowed[parent]; ok {
				filtered = append(filtered, cat)
				continue
			}
		}
	}

	if len(filtered) == 0 {
		return nil, false
	}

	return filtered, true
}

func deriveParentCategory(cat int) int {
	if cat < 1000 {
		return cat
	}
	return (cat / 100) * 100
}

// buildSearchParams builds URL parameters from a TorznabSearchRequest
func (s *Service) buildSearchParams(req *TorznabSearchRequest, searchMode string) url.Values {
	params := url.Values{}
	mode := strings.TrimSpace(searchMode)
	if mode == "" {
		mode = "search"
	}
	params.Set("t", mode)
	params.Set("q", req.Query)

	if len(req.Categories) > 0 {
		catStr := make([]string, len(req.Categories))
		for i, cat := range req.Categories {
			catStr[i] = strconv.Itoa(cat)
		}
		params.Set("cat", strings.Join(catStr, ","))
	}

	// Always add basic parameters - these are widely supported
	if req.IMDbID != "" {
		// Strip "tt" prefix if present
		cleanIMDbID := strings.TrimPrefix(req.IMDbID, "tt")
		params.Set("imdbid", cleanIMDbID)
		log.Debug().
			Str("search_mode", mode).
			Str("imdb_id", cleanIMDbID).
			Msg("Adding IMDb ID parameter to torznab search")
	}

	if req.TVDbID != "" {
		params.Set("tvdbid", req.TVDbID)
		log.Debug().
			Str("search_mode", mode).
			Str("tvdb_id", req.TVDbID).
			Msg("Adding TVDb ID parameter to torznab search")
	}

	if req.Season != nil {
		params.Set("season", strconv.Itoa(*req.Season))
		log.Debug().
			Str("search_mode", mode).
			Int("season", *req.Season).
			Msg("Adding season parameter to torznab search")
	}

	if req.Episode != nil {
		params.Set("ep", strconv.Itoa(*req.Episode))
		log.Debug().
			Str("search_mode", mode).
			Int("episode", *req.Episode).
			Msg("Adding episode parameter to torznab search")
	}

	// Store year in params but don't set it yet - will be handled per-indexer
	if req.Year > 0 {
		params.Set("_year", strconv.Itoa(req.Year)) // Temporary storage
	}

	if req.Limit > 0 {
		params.Set("limit", strconv.Itoa(req.Limit))
	}

	if req.Offset > 0 {
		params.Set("offset", strconv.Itoa(req.Offset))
	}

	return params
}

func searchModeForContentType(ct contentType) string {
	switch ct {
	case contentTypeMovie:
		return "movie"
	case contentTypeTVShow, contentTypeTVDaily:
		return "tvsearch"
	case contentTypeMusic:
		return "music"
	case contentTypeAudiobook:
		return "audio"
	case contentTypeBook, contentTypeComic, contentTypeMagazine:
		return "book"
	default:
		return "search"
	}
}

var retryAfterRegex = regexp.MustCompile(`retry[- ]?after[:= ]*(\d+)`)

var rateLimitTokens = []string{"429", "rate limit", "too many requests"}

func detectRateLimit(err error) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}
	msg := strings.ToLower(err.Error())
	matched := false
	for _, token := range rateLimitTokens {
		if strings.Contains(msg, token) {
			matched = true
			break
		}
	}
	if !matched {
		return 0, false
	}
	if dur := extractRetryAfter(msg); dur > 0 {
		return dur, true
	}
	return defaultRateLimitCooldown, true
}

func extractRetryAfter(msg string) time.Duration {
	matches := retryAfterRegex.FindStringSubmatch(msg)
	if len(matches) == 2 {
		if seconds, err := strconv.Atoi(matches[1]); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return 0
}

func (s *Service) handleRateLimit(ctx context.Context, idx *models.TorznabIndexer, cooldown time.Duration, cause error) {
	if idx == nil {
		return
	}
	if cooldown <= 0 {
		cooldown = defaultRateLimitCooldown
	}
	resumeAt := time.Now().Add(cooldown)
	s.rateLimiter.SetCooldown(idx.ID, resumeAt)

	message := fmt.Sprintf("Rate limit triggered for %s, pausing until %s", idx.Name, resumeAt.Format(time.RFC3339))
	if err := s.indexerStore.RecordError(ctx, idx.ID, message, "rate_limit"); err != nil {
		log.Debug().Err(err).Int("indexer_id", idx.ID).Msg("Failed to record rate-limit error")
	}

	s.adaptRequestLimits(ctx, idx)
}

func (s *Service) adaptRequestLimits(ctx context.Context, idx *models.TorznabIndexer) {
	if idx == nil {
		return
	}
	if hourCount, err := s.indexerStore.CountRequests(ctx, idx.ID, time.Hour); err == nil {
		if limit, ok := inferredLimitFromCount(hourCount); ok {
			if idx.HourlyRequestLimit == nil || limit < *idx.HourlyRequestLimit {
				if err := s.indexerStore.UpdateRequestLimits(ctx, idx.ID, &limit, nil); err != nil {
					log.Debug().Err(err).Int("indexer_id", idx.ID).Msg("Failed to persist hourly request limit")
				} else {
					idx.HourlyRequestLimit = &limit
					log.Info().
						Int("indexer_id", idx.ID).
						Str("indexer", idx.Name).
						Int("hourly_limit", limit).
						Msg("Updated inferred hourly request limit")
				}
			}
		}
	} else {
		log.Debug().Err(err).Int("indexer_id", idx.ID).Msg("Failed to count hourly requests for rate limit")
	}

	if dayCount, err := s.indexerStore.CountRequests(ctx, idx.ID, 24*time.Hour); err == nil {
		if limit, ok := inferredLimitFromCount(dayCount); ok {
			if idx.DailyRequestLimit == nil || limit < *idx.DailyRequestLimit {
				if err := s.indexerStore.UpdateRequestLimits(ctx, idx.ID, nil, &limit); err != nil {
					log.Debug().Err(err).Int("indexer_id", idx.ID).Msg("Failed to persist daily request limit")
				} else {
					idx.DailyRequestLimit = &limit
					log.Info().
						Int("indexer_id", idx.ID).
						Str("indexer", idx.Name).
						Int("daily_limit", limit).
						Msg("Updated inferred daily request limit")
				}
			}
		}
	} else {
		log.Debug().Err(err).Int("indexer_id", idx.ID).Msg("Failed to count daily requests for rate limit")
	}
}

func inferredLimitFromCount(count int) (int, bool) {
	if count <= 0 {
		return 0, false
	}
	limit := count - 1
	if limit <= 0 {
		limit = 1
	}
	return limit, true
}

func requiredCapabilities(meta *searchContext) []string {
	if meta == nil {
		return nil
	}
	switch meta.searchMode {
	case "tvsearch":
		return []string{"tv-search"}
	case "movie":
		return []string{"movie-search"}
	case "music":
		return []string{"music-search", "audio-search"}
	case "audio":
		return []string{"audio-search", "music-search"}
	case "book":
		return []string{"book-search"}
	default:
		return nil
	}
}

// getPreferredCapabilities returns enhanced capabilities to look for based on search parameters
func getPreferredCapabilities(req *TorznabSearchRequest, searchMode string) []string {
	var preferred []string

	// Base capability requirement
	required := requiredCapabilities(&searchContext{searchMode: searchMode})
	preferred = append(preferred, required...)

	// Add parameter-specific preferences
	switch searchMode {
	case "movie":
		if req.IMDbID != "" {
			preferred = append(preferred, "movie-search-imdbid")
		}
		if req.TVDbID != "" { // Some indexers use TMDB for movies
			preferred = append(preferred, "movie-search-tmdbid")
		}
		if req.Year > 0 {
			preferred = append(preferred, "movie-search-year")
		}
	case "tvsearch":
		if req.TVDbID != "" {
			preferred = append(preferred, "tv-search-tvdbid")
		}
		if req.Season != nil && *req.Season > 0 {
			preferred = append(preferred, "tv-search-season")
		}
		if req.Episode != nil && *req.Episode > 0 {
			preferred = append(preferred, "tv-search-ep")
		}
		if req.Year > 0 {
			preferred = append(preferred, "tv-search-year")
		}
	}

	return preferred
}

func supportsAnyCapability(current []string, required []string) bool {
	if len(required) == 0 {
		return true
	}
	for _, candidate := range required {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate == "" {
			continue
		}
		if slices.ContainsFunc(current, func(cap string) bool {
			return strings.EqualFold(strings.TrimSpace(cap), candidate)
		}) {
			return true
		}
	}
	return false
}

// supportsPreferredCapabilities checks if indexer supports preferred capabilities with fallback to basic requirements
func supportsPreferredCapabilities(current []string, preferred []string) bool {
	if len(preferred) <= 1 {
		return supportsAnyCapability(current, preferred)
	}

	// Check if indexer supports any parameter-specific capabilities
	paramSpecific := make([]string, 0)
	basic := make([]string, 0)

	for _, cap := range preferred {
		if strings.Contains(cap, "-") && len(strings.Split(cap, "-")) > 2 {
			// This is a parameter-specific capability like "movie-search-imdbid"
			paramSpecific = append(paramSpecific, cap)
		} else {
			// This is a basic capability like "movie-search"
			basic = append(basic, cap)
		}
	}

	// If indexer supports any parameter-specific capabilities, that's preferred
	if len(paramSpecific) > 0 && supportsAnyCapability(current, paramSpecific) {
		return true
	}

	// Otherwise, fall back to basic capability requirements
	return supportsAnyCapability(current, basic)
}

// convertResults converts Jackett results to our SearchResult format
func (s *Service) convertResults(results []Result) []SearchResult {
	searchResults := make([]SearchResult, 0, len(results))

	for _, r := range results {
		// Parse release info to extract source, collection, and group
		var source, collection, group string
		if r.Title != "" {
			parsed := s.releaseParser.Parse(r.Title)
			source = parsed.Source
			collection = parsed.Collection
			group = parsed.Group
		}

		result := SearchResult{
			Indexer:              r.Tracker,
			IndexerID:            r.IndexerID,
			Title:                r.Title,
			DownloadURL:          r.Link,
			InfoURL:              r.Details,
			Size:                 r.Size,
			Seeders:              r.Seeders,
			Leechers:             r.Peers - r.Seeders, // Peers includes seeders
			CategoryID:           s.parseCategoryID(r.Category),
			CategoryName:         r.Category,
			PublishDate:          r.PublishDate,
			DownloadVolumeFactor: r.DownloadVolumeFactor,
			UploadVolumeFactor:   r.UploadVolumeFactor,
			GUID:                 r.GUID,
			IMDbID:               r.Imdb,
			TVDbID:               s.parseTVDbID(r),
			Source:               source,
			Collection:           collection,
			Group:                group,
		}
		searchResults = append(searchResults, result)
	}

	// Sort by seeders (descending) and then by size
	sort.Slice(searchResults, func(i, j int) bool {
		if searchResults[i].Seeders != searchResults[j].Seeders {
			return searchResults[i].Seeders > searchResults[j].Seeders
		}
		return searchResults[i].Size > searchResults[j].Size
	})

	return searchResults
}

// parseCategoryID attempts to extract the category ID from category string
func (s *Service) parseCategoryID(category string) int {
	// Categories often come as "5000" or "TV" or "TV > HD"
	parts := strings.Split(category, " ")
	if len(parts) > 0 {
		if id, err := strconv.Atoi(parts[0]); err == nil {
			return id
		}
	}

	// Try to map category names to IDs
	categoryMap := map[string]int{
		"movies": CategoryMovies,
		"tv":     CategoryTV,
		"xxx":    CategoryXXX,
		"audio":  CategoryAudio,
		"pc":     CategoryPC,
		"books":  CategoryBooks,
	}

	categoryLower := strings.ToLower(category)
	for name, id := range categoryMap {
		if strings.Contains(categoryLower, name) {
			return id
		}
	}

	return 0
}

// parseTVDbID extracts TVDb ID from result if available
func (s *Service) parseTVDbID(r Result) string {
	// TVDb ID might be in various places depending on indexer
	// This is a placeholder - would need to check actual Jackett response structure
	return ""
}

func (s *Service) resolveIndexerSelection(ctx context.Context, indexerIDs []int) ([]*models.TorznabIndexer, error) {
	if len(indexerIDs) == 0 {
		indexers, err := s.indexerStore.ListEnabled(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list enabled indexers: %w", err)
		}
		return indexers, nil
	}

	var selected []*models.TorznabIndexer
	for _, id := range indexerIDs {
		indexer, err := s.indexerStore.Get(ctx, id)
		if err != nil {
			log.Warn().
				Err(err).
				Int("indexer_id", id).
				Msg("Failed to load requested indexer")
			continue
		}
		if !indexer.Enabled {
			continue
		}
		selected = append(selected, indexer)
	}

	return selected, nil
}

// extractIndexerIDFromURL extracts the indexer ID from a Jackett URL
// e.g., http://jackett:9117/api/v2.0/indexers/aither/ -> aither
// If URL doesn't contain an indexer path, returns the indexer name as fallback
func extractIndexerIDFromURL(baseURL, indexerName string) string {
	// Parse the URL to find the indexer ID
	parts := strings.Split(strings.TrimSuffix(baseURL, "/"), "/")

	// Look for "indexers" in the path and get the next segment
	for i, part := range parts {
		if (part == "indexers" || part == "indexer") && i+1 < len(parts) {
			return parts[i+1]
		}
	}

	// If no indexer ID found in URL, return the indexer name
	// This handles cases where BaseURL is just the Jackett base URL
	return strings.ToLower(strings.ReplaceAll(indexerName, " ", ""))
}

// GetOptimalCategoriesForIndexers returns categories optimized for the given indexers based on their capabilities
func (s *Service) GetOptimalCategoriesForIndexers(ctx context.Context, requestedCategories []int, indexerIDs []int) []int {
	if len(requestedCategories) == 0 || len(indexerIDs) == 0 {
		return requestedCategories
	}

	// Get all specified indexers
	var indexers []*models.TorznabIndexer
	for _, id := range indexerIDs {
		indexer, err := s.indexerStore.Get(ctx, id)
		if err != nil {
			log.Debug().Err(err).Int("indexer_id", id).Msg("Failed to get indexer for category mapping")
			continue
		}
		if indexer.Enabled {
			indexers = append(indexers, indexer)
		}
	}

	if len(indexers) == 0 {
		return requestedCategories
	}

	// Find the intersection of categories supported by all indexers
	commonCategories := make(map[int]int) // category -> count of indexers supporting it

	for _, indexer := range indexers {
		mappedCategories := s.MapCategoriesToIndexerCapabilities(ctx, indexer, requestedCategories)
		for _, cat := range mappedCategories {
			commonCategories[cat]++
		}
	}

	// Return categories that are supported by most indexers
	threshold := len(indexers) / 2 // At least half of the indexers should support it
	if threshold < 1 {
		threshold = 1
	}

	optimalCategories := make([]int, 0, len(requestedCategories))
	for _, requestedCat := range requestedCategories {
		if count, exists := commonCategories[requestedCat]; exists && count >= threshold {
			optimalCategories = append(optimalCategories, requestedCat)
		}
	}

	// If no optimal categories found, return original requested categories
	if len(optimalCategories) == 0 {
		return requestedCategories
	}

	return optimalCategories
}

func resolveCapsIdentifier(indexer *models.TorznabIndexer) (string, error) {
	switch indexer.Backend {
	case models.TorznabBackendProwlarr:
		if trimmed := strings.TrimSpace(indexer.IndexerID); trimmed != "" {
			return trimmed, nil
		}
		return "", fmt.Errorf("prowlarr indexer identifier is required for caps sync: %w", ErrMissingIndexerIdentifier)
	case models.TorznabBackendNative:
		return "", nil
	default:
		identifier := strings.TrimSpace(indexer.IndexerID)
		if identifier == "" {
			identifier = extractIndexerIDFromURL(indexer.BaseURL, indexer.Name)
		}
		if trimmed := strings.TrimSpace(identifier); trimmed != "" {
			return trimmed, nil
		}
		return "", fmt.Errorf("jackett indexer identifier is required for caps sync: %w", ErrMissingIndexerIdentifier)
	}
}

// contentType represents the type of content being searched (internal use only)
type contentType int

const (
	contentTypeUnknown contentType = iota
	contentTypeMovie
	contentTypeTVShow
	contentTypeTVDaily
	contentTypeXXX
	contentTypeMusic
	contentTypeAudiobook
	contentTypeBook
	contentTypeComic
	contentTypeMagazine
	contentTypeEducation
	contentTypeApp
	contentTypeGame
)

// detectContentType attempts to detect the content type from search parameters
func (s *Service) detectContentType(req *TorznabSearchRequest) contentType {
	query := strings.TrimSpace(req.Query)
	queryLower := strings.ReplaceAll(strings.ToLower(query), ".", " ")

	if strings.Contains(queryLower, "xxx") {
		return contentTypeXXX
	}

	// Structured hints take precedence.
	if req.Episode != nil && *req.Episode > 0 {
		return contentTypeTVShow
	}
	if req.Season != nil && *req.Season > 0 {
		return contentTypeTVShow
	}
	if req.TVDbID != "" {
		return contentTypeTVShow
	}
	if req.IMDbID != "" {
		return contentTypeMovie
	}

	release := s.releaseParser.Parse(query)
	switch release.Type {
	case rls.Movie:
		return contentTypeMovie
	case rls.Episode, rls.Series:
		return contentTypeTVShow
	case rls.Music:
		return contentTypeMusic
	case rls.Audiobook:
		return contentTypeAudiobook
	case rls.Book:
		return contentTypeBook
	case rls.Comic:
		return contentTypeComic
	case rls.Magazine:
		return contentTypeMagazine
	case rls.Education:
		return contentTypeEducation
	case rls.App:
		return contentTypeApp
	case rls.Game:
		return contentTypeGame
	}

	if release.Type == rls.Unknown {
		if release.Series > 0 || release.Episode > 0 {
			return contentTypeTVShow
		}
		if release.Year > 0 {
			return contentTypeMovie
		}
	}

	return contentTypeUnknown
}

// detectContentTypeFromCategories attempts to detect content type from provided categories
func detectContentTypeFromCategories(categories []int) contentType {
	if len(categories) == 0 {
		return contentTypeUnknown
	}

	// Check if categories contain specific content type indicators
	hasMovieCategories := false
	hasTVCategories := false
	hasAudioCategories := false
	hasBookCategories := false
	hasXXXCategories := false
	hasPCCategories := false

	for _, cat := range categories {
		switch {
		case cat >= CategoryMovies && cat < 3000: // 2000-2999 range
			hasMovieCategories = true
		case cat >= CategoryAudio && cat < 4000: // 3000-3999 range
			hasAudioCategories = true
		case cat >= CategoryPC && cat < 5000: // 4000-4999 range
			hasPCCategories = true
		case cat >= CategoryTV && cat < 6000: // 5000-5999 range
			hasTVCategories = true
		case cat >= CategoryXXX && cat < 7000: // 6000-6999 range
			hasXXXCategories = true
		case cat >= CategoryBooks && cat < 8000: // 7000-7999 range
			hasBookCategories = true
		}
	}

	// Return the most specific content type detected - prioritize audio/music first
	if hasAudioCategories {
		return contentTypeMusic // Default to music for audio categories
	}
	if hasMovieCategories {
		return contentTypeMovie
	}
	if hasTVCategories {
		return contentTypeTVShow
	}
	if hasBookCategories {
		return contentTypeBook
	}
	if hasXXXCategories {
		return contentTypeXXX
	}
	if hasPCCategories {
		return contentTypeApp // Default to app for PC categories
	}

	return contentTypeUnknown
}

// getCategoriesForContentType returns the appropriate Torznab categories for a content type
func getCategoriesForContentType(ct contentType) []int {
	switch ct {
	case contentTypeMovie:
		return []int{CategoryMovies, CategoryMoviesSD, CategoryMoviesHD, CategoryMovies4K}
	case contentTypeTVShow, contentTypeTVDaily:
		return []int{CategoryTV, CategoryTVSD, CategoryTVHD, CategoryTV4K}
	case contentTypeXXX:
		return []int{CategoryXXX, CategoryXXXDVD, CategoryXXXx264, CategoryXXXPack}
	case contentTypeMusic:
		return []int{CategoryAudio}
	case contentTypeAudiobook:
		return []int{CategoryAudio}
	case contentTypeBook:
		return []int{CategoryBooks, CategoryBooksEbook}
	case contentTypeComic:
		return []int{CategoryBooksComics}
	case contentTypeMagazine:
		return []int{CategoryBooks}
	case contentTypeEducation:
		return []int{CategoryBooks}
	case contentTypeApp, contentTypeGame:
		return []int{CategoryPC}
	default:
		// Return common categories
		return []int{CategoryMovies, CategoryTV}
	}
}
