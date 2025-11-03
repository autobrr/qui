// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

// IndexerStore defines the interface for indexer storage operations
type IndexerStore interface {
	Get(ctx context.Context, id int) (*models.TorznabIndexer, error)
	List(ctx context.Context) ([]*models.TorznabIndexer, error)
	ListEnabled(ctx context.Context) ([]*models.TorznabIndexer, error)
	GetDecryptedAPIKey(indexer *models.TorznabIndexer) (string, error)
}

// Service provides Jackett integration for Torznab searching
type Service struct {
	indexerStore IndexerStore
}

// NewService creates a new Jackett service
func NewService(indexerStore IndexerStore) *Service {
	return &Service{
		indexerStore: indexerStore,
	}
}

// Search searches enabled Torznab indexers with intelligent category detection
func (s *Service) Search(ctx context.Context, req *TorznabSearchRequest) (*SearchResponse, error) {
	if req.Query == "" {
		return nil, fmt.Errorf("query is required")
	}

	// Auto-detect content type if categories not provided
	if len(req.Categories) == 0 {
		contentType := detectContentType(req)
		req.Categories = getCategoriesForContentType(contentType)

		log.Debug().
			Str("query", req.Query).
			Int("content_type", int(contentType)).
			Ints("categories", req.Categories).
			Msg("Auto-detected content type and categories")
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
	params := s.buildSearchParams(req)

	// Search all enabled indexers
	allResults := s.searchMultipleIndexers(ctx, indexers, params)

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

	// Build search parameters
	params := url.Values{}
	params.Set("q", req.Query)

	if len(req.Categories) > 0 {
		catStr := make([]string, len(req.Categories))
		for i, cat := range req.Categories {
			catStr[i] = strconv.Itoa(cat)
		}
		params.Set("cat", strings.Join(catStr, ","))
	}

	if req.IMDbID != "" {
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
	allResults := s.searchMultipleIndexers(ctx, indexersToSearch, params)

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

// searchMultipleIndexers searches multiple indexers in parallel and aggregates results
func (s *Service) searchMultipleIndexers(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values) []Result {
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

			var results []Result
			switch idx.Backend {
			case models.TorznabBackendNative:
				// Direct Torznab endpoint - search without indexer path
				log.Debug().
					Int("indexer_id", idx.ID).
					Str("indexer_name", idx.Name).
					Str("base_url", idx.BaseURL).
					Str("backend", string(idx.Backend)).
					Msg("Searching native Torznab endpoint")
				results, err = client.SearchDirect(paramsMap)
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

				log.Debug().
					Int("indexer_id", idx.ID).
					Str("indexer_name", idx.Name).
					Str("backend", string(idx.Backend)).
					Str("torznab_indexer_id", indexerID).
					Msg("Searching Prowlarr indexer")
				results, err = client.Search(indexerID, paramsMap)
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

				log.Debug().
					Int("indexer_id", idx.ID).
					Str("indexer_name", idx.Name).
					Str("backend", string(idx.Backend)).
					Str("torznab_indexer_id", indexerID).
					Msg("Searching Torznab aggregator indexer")
				results, err = client.Search(indexerID, paramsMap)
			}
			if err != nil {
				log.Warn().
					Err(err).
					Int("indexer_id", idx.ID).
					Str("indexer", idx.Name).
					Msg("Failed to search indexer")
				resultsChan <- indexerResult{nil, err}
				return
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

// buildSearchParams builds URL parameters from a TorznabSearchRequest
func (s *Service) buildSearchParams(req *TorznabSearchRequest) url.Values {
	params := url.Values{}
	params.Set("t", "search")
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

// convertResults converts Jackett results to our SearchResult format
func (s *Service) convertResults(results []Result) []SearchResult {
	searchResults := make([]SearchResult, 0, len(results))

	for _, r := range results {
		result := SearchResult{
			Indexer:              r.Tracker,
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

// contentType represents the type of content being searched (internal use only)
type contentType int

const (
	contentTypeUnknown contentType = iota
	contentTypeMovie
	contentTypeTVShow
	contentTypeTVDaily
	contentTypeXXX
)

// detectContentType attempts to detect the content type from search parameters
func detectContentType(req *TorznabSearchRequest) contentType {
	queryLower := strings.ReplaceAll(strings.ToLower(req.Query), ".", " ")
	if strings.Contains(queryLower, "xxx") {
		return contentTypeXXX
	}

	// If we have episode info, it's TV
	if req.Episode != nil && *req.Episode > 0 {
		return contentTypeTVShow
	}

	// If we have season but no episode, could be season pack
	if req.Season != nil && *req.Season > 0 {
		return contentTypeTVShow
	}

	// If we have TVDbID, it's TV
	if req.TVDbID != "" {
		return contentTypeTVShow
	}

	// If we have year but no season/episode, likely a movie
	if req.IMDbID != "" {
		return contentTypeMovie
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
	default:
		// Return common categories
		return []int{CategoryMovies, CategoryTV}
	}
}
