// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"strconv"

	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/jackett"
)

const (
	// SearchScope is the cache scope used for dir-scan searches.
	SearchScope = "dir-scan"

	torznabDirScanSearchLimit = 100
)

// Searcher handles searching Torznab indexers for matching torrents.
type Searcher struct {
	jackettService JackettSearcher
	parser         *Parser
}

// JackettSearcher is the interface for the jackett service search functionality.
type JackettSearcher interface {
	SearchWithScope(ctx context.Context, req *jackett.TorznabSearchRequest, scope string) error
	IndexerIDsWithIDSearchCaps(ctx context.Context, req *jackett.TorznabSearchRequest) []int
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

func (s *Searcher) buildSearchRequest(meta *SearcheeMetadata, indexerIDs, categories []int) *jackett.TorznabSearchRequest {
	searchReq := &jackett.TorznabSearchRequest{
		ReleaseName:      meta.OriginalName,
		Categories:       categories,
		IndexerIDs:       indexerIDs,
		Limit:            torznabDirScanSearchLimit,
		ReturnAllResults: true,
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

	// Year hurts TV searches on many indexers; keep it for movies only.
	if meta.Year > 0 && meta.IsMovie {
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
// Shared with cross-seed so the two search paths cannot drift apart.
func buildSearchQuery(meta *SearcheeMetadata) string {
	return crossseed.BuildTorznabQuery(meta.CleanedName, meta.Release, meta.IsMusic).Query
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
