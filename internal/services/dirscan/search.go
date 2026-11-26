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
	SearchScope = "dir-scan"

	torznabDirScanSearchLimit = 100
)

type Searcher struct {
	jackettService JackettSearcher
	parser         *Parser
}

type JackettSearcher interface {
	SearchWithScope(ctx context.Context, req *jackett.TorznabSearchRequest, scope string) error
	IndexerIDsWithIDSearchCaps(ctx context.Context, req *jackett.TorznabSearchRequest) []int
}

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

	if meta.HasExternalIDs() {
		s.applyExternalIDs(searchReq, meta)
		searchReq.OmitQueryForIDs = true
	}

	searchReq.Query = buildSearchQuery(meta)

	if meta.IsTV {
		if meta.Season != nil {
			searchReq.Season = meta.Season
		}
		if meta.Episode != nil {
			searchReq.Episode = meta.Episode
		}
	}

	if meta.Year > 0 && meta.IsMovie {
		searchReq.Year = meta.Year
	}

	return searchReq
}

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

func buildSearchQuery(meta *SearcheeMetadata) string {
	return crossseed.BuildTorznabQuery(meta.CleanedName, meta.Release, meta.IsMusic).Query
}

type SearchResult struct {
	*jackett.SearchResult

	IndexerID int

	Searchee *Searchee

	MatchScore float64
}

func CalculateSizeRange(searcheeSize int64, tolerancePercent float64) (minSize, maxSize int64) {
	if searcheeSize <= 0 {
		return 0, 0
	}

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
