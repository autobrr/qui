// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/autobrr/qui/internal/services/jackett"
)

// SearchScope is the cache scope used for dir-scan searches.
const SearchScope = "dir-scan"

// Searcher handles searching Torznab indexers for matching torrents.
type Searcher struct {
	jackettService JackettSearcher
	parser         *Parser
}

// JackettSearcher is the interface for the jackett service search functionality.
type JackettSearcher interface {
	SearchWithScope(ctx context.Context, req *jackett.TorznabSearchRequest, scope string) error
}

// NewSearcher creates a new searcher.
func NewSearcher(jackettService JackettSearcher, parser *Parser) *Searcher {
	if parser == nil {
		parser = NewParser(nil)
	}
	return &Searcher{
		jackettService: jackettService,
		parser:         parser,
	}
}

// SearchRequest contains parameters for searching a searchee.
type SearchRequest struct {
	// Searchee to search for
	Searchee *Searchee

	// Metadata parsed from the searchee name (optional, will be parsed if nil)
	Metadata *SearcheeMetadata

	// IndexerIDs to search (empty = all enabled indexers)
	IndexerIDs []int

	// Categories to search (optional, but recommended for better results).
	Categories []int

	// Limit results per indexer
	Limit int

	// OnAllComplete is called when all search jobs complete with the final results
	OnAllComplete func(response *jackett.SearchResponse, err error)
}

// Search searches Torznab indexers for torrents matching a searchee.
// Uses embedded IDs first (most accurate), then falls back to title + year.
func (s *Searcher) Search(ctx context.Context, req *SearchRequest) error {
	if req.Searchee == nil {
		return errors.New("searchee is required")
	}

	ctx = jackett.WithSearchPriority(ctx, jackett.RateLimitPriorityBackground)

	// Parse metadata if not provided
	meta := req.Metadata
	if meta == nil {
		meta = s.parser.Parse(req.Searchee.Name)
	}

	// Build the search request
	searchReq := s.buildSearchRequest(meta, req)

	// Execute the search with dir-scan scope
	if err := s.jackettService.SearchWithScope(ctx, searchReq, SearchScope); err != nil {
		return fmt.Errorf("search indexers: %w", err)
	}
	return nil
}

// buildSearchRequest constructs a TorznabSearchRequest from parsed metadata.
func (s *Searcher) buildSearchRequest(meta *SearcheeMetadata, req *SearchRequest) *jackett.TorznabSearchRequest {
	searchReq := &jackett.TorznabSearchRequest{
		ReleaseName:   meta.OriginalName,
		Categories:    req.Categories,
		IndexerIDs:    req.IndexerIDs,
		Limit:         req.Limit,
		OnAllComplete: req.OnAllComplete,
	}

	// Priority 1: Use embedded external IDs if available (most accurate)
	if meta.HasExternalIDs() {
		s.applyExternalIDs(searchReq, meta)
		searchReq.OmitQueryForIDs = true
	}

	// Always set the query for fallback/combined search
	searchReq.Query = buildSearchQuery(meta)

	// Apply TV-specific parameters
	if meta.IsTV {
		if meta.Season != nil {
			searchReq.Season = meta.Season
		}
		if meta.Episode != nil {
			searchReq.Episode = meta.Episode
		}
	}

	// Apply year if available
	if meta.Year > 0 {
		searchReq.Year = meta.Year
	}

	return searchReq
}

// applyExternalIDs sets external database IDs on the search request.
func (s *Searcher) applyExternalIDs(req *jackett.TorznabSearchRequest, meta *SearcheeMetadata) {
	if imdbID := meta.GetIMDbID(); imdbID != "" {
		req.IMDbID = imdbID
	}
	if tvdbID := meta.GetTVDbID(); tvdbID > 0 {
		req.TVDbID = strconv.Itoa(tvdbID)
	}
	if tmdbID := meta.GetTMDbID(); tmdbID > 0 {
		req.TMDbID = tmdbID
	}
}

// buildSearchQuery constructs a search query string from metadata.
func buildSearchQuery(meta *SearcheeMetadata) string {
	var parts []string

	// Use the parsed title
	title := meta.Title
	if title == "" {
		title = meta.CleanedName
	}

	// Clean the title for searching
	title = cleanForSearch(title)
	if title != "" {
		parts = append(parts, title)
	}

	// Add year if available and this looks like a movie
	if meta.Year > 0 && meta.IsMovie {
		parts = append(parts, strconv.Itoa(meta.Year))
	}

	return strings.Join(parts, " ")
}

// cleanForSearch removes characters that might interfere with search.
func cleanForSearch(s string) string {
	// Remove common problematic characters
	replacer := strings.NewReplacer(
		".", " ",
		"_", " ",
		"-", " ",
		"[", "",
		"]", "",
		"(", "",
		")", "",
		"{", "",
		"}", "",
	)
	s = replacer.Replace(s)

	// Clean up extra spaces
	return cleanExtraSpaces(s)
}

// SearchResult wraps a jackett search result with additional matching context.
type SearchResult struct {
	*jackett.SearchResult

	// IndexerID that returned this result
	IndexerID int

	// Searchee this result is for
	Searchee *Searchee

	// MatchScore indicates match quality (higher is better)
	MatchScore float64
}

// FilterResults filters search results based on basic criteria.
func FilterResults(results []*SearchResult, minSize, maxSize int64) []*SearchResult {
	if len(results) == 0 {
		return results
	}

	filtered := make([]*SearchResult, 0, len(results))
	for _, r := range results {
		// Skip if size is outside bounds
		if minSize > 0 && r.Size < minSize {
			continue
		}
		if maxSize > 0 && r.Size > maxSize {
			continue
		}

		filtered = append(filtered, r)
	}

	return filtered
}

// CalculateSizeRange calculates min/max size based on searchee size and tolerance.
func CalculateSizeRange(searcheeSize int64, tolerancePercent float64) (minSize, maxSize int64) {
	if searcheeSize <= 0 {
		return 0, 0
	}

	// 0% tolerance means an exact match (min==max==searcheeSize).
	if tolerancePercent <= 0 {
		return searcheeSize, searcheeSize
	}

	tolerance := float64(searcheeSize) * (tolerancePercent / 100.0)
	minSize = int64(float64(searcheeSize) - tolerance)
	maxSize = int64(float64(searcheeSize) + tolerance)

	if minSize < 0 {
		minSize = 0
	}

	return minSize, maxSize
}
