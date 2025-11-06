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
		// When categories are provided, skip content detection
		detectedType = contentTypeUnknown
		log.Debug().
			Str("query", req.Query).
			Ints("categories", req.Categories).
			Msg("Using provided categories, skipping content detection")
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
		// When categories are provided, skip content detection
		detectedType = contentTypeUnknown
		log.Debug().
			Str("query", req.Query).
			Ints("categories", req.Categories).
			Msg("Using provided categories for general search, skipping content detection")
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

	if len(requiredCaps) > 0 && len(idx.Capabilities) > 0 && !supportsAnyCapability(idx.Capabilities, requiredCaps) {
		log.Info().
			Int("indexer_id", idx.ID).
			Str("indexer", idx.Name).
			Strs("required_caps", requiredCaps).
			Msg("Skipping torznab indexer due to missing capabilities")
		return true
	}

	if len(requested) == 0 {
		return false
	}

	if len(idx.Categories) == 0 {
		return false
	}

	filtered, ok := filterCategoriesForIndexer(idx.Categories, requested)
	if !ok {
		log.Info().
			Int("indexer_id", idx.ID).
			Str("indexer", idx.Name).
			Ints("requested_categories", requested).
			Msg("Skipping torznab indexer due to unsupported categories")
		return true
	}

	params["cat"] = formatCategoryList(filtered)
	return false
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

	if req.IMDbID != "" {
		// Strip "tt" prefix if present
		params.Set("imdbid", strings.TrimPrefix(req.IMDbID, "tt"))
	}

	if req.TVDbID != "" {
		params.Set("tvdbid", req.TVDbID)
	}

	if req.Season != nil {
		params.Set("season", strconv.Itoa(*req.Season))
	}

	if req.Episode != nil {
		params.Set("ep", strconv.Itoa(*req.Episode))
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
