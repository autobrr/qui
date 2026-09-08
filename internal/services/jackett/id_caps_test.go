// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

// indexerKeepsRequestIDParams must mirror the pruning in
// applyCapabilitySpecificParams: an indexer counts as ID-queried exactly when
// at least one of the request's ID params survives for the search mode.
func TestIndexerKeepsRequestIDParams(t *testing.T) {
	tests := []struct {
		name       string
		req        TorznabSearchRequest
		caps       []string
		searchMode string
		want       bool
	}{
		{
			name:       "movie imdb capability keeps the imdbid param",
			req:        TorznabSearchRequest{IMDbID: "tt0111161"},
			caps:       []string{"search", "movie-search", "movie-search-imdbid"},
			searchMode: "movie",
			want:       true,
		},
		{
			name:       "capability match is case-insensitive",
			req:        TorznabSearchRequest{IMDbID: "tt0111161"},
			caps:       []string{"Movie-Search-IMDBid"},
			searchMode: "movie",
			want:       true,
		},
		{
			name:       "no ID capability falls back to title",
			req:        TorznabSearchRequest{IMDbID: "tt0111161"},
			caps:       []string{"search", "movie-search"},
			searchMode: "movie",
			want:       false,
		},
		{
			name:       "tvdbid has no movie-mode capability",
			req:        TorznabSearchRequest{TVDbID: "12345"},
			caps:       []string{"tv-search-tvdbid"},
			searchMode: "movie",
			want:       false,
		},
		{
			name:       "tvsearch tvdbid capability keeps the param",
			req:        TorznabSearchRequest{TVDbID: "12345"},
			caps:       []string{"tv-search", "tv-search-tvdbid"},
			searchMode: "tvsearch",
			want:       true,
		},
		{
			name:       "one surviving ID out of several is enough",
			req:        TorznabSearchRequest{IMDbID: "tt0111161", TMDbID: 278},
			caps:       []string{"movie-search-tmdbid"},
			searchMode: "movie",
			want:       true,
		},
		{
			// The executor never prunes params for a caps-less indexer, so it
			// searches by ID and must land in the title-rescue set.
			name:       "unknown capabilities keep the ID params",
			req:        TorznabSearchRequest{IMDbID: "tt0111161"},
			caps:       nil,
			searchMode: "movie",
			want:       true,
		},
		{
			name:       "generic search mode never counts as ID-queried",
			req:        TorznabSearchRequest{IMDbID: "tt0111161"},
			caps:       []string{"movie-search-imdbid"},
			searchMode: "search",
			want:       false,
		},
		{
			name:       "request without IDs never counts",
			req:        TorznabSearchRequest{Query: "Signal Static"},
			caps:       []string{"movie-search-imdbid"},
			searchMode: "movie",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := &models.TorznabIndexer{ID: 1, Capabilities: tt.caps}
			require.Equal(t, tt.want, indexerKeepsRequestIDParams(idx, &tt.req, tt.searchMode))
		})
	}
}
