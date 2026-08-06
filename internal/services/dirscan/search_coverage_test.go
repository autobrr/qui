// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"io"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
)

func coverageTestService(response *jackett.SearchResponse) *Service {
	return retryTestService(&recordingSearcher{responses: []*jackett.SearchResponse{response}})
}

func coverageTestSearchee() *Searchee {
	return &Searchee{
		Name: "Example.Movie.2024.1080p.WEB-DL",
		Path: "/tmp/example",
		Files: []*ScannedFile{
			{Path: "/tmp/example/video.mkv", RelPath: "video.mkv", Size: 123},
		},
	}
}

func TestProcessSearchee_CoverageGatesFinalization(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	l := zerolog.New(io.Discard)
	dir := &models.DirScanDirectory{ID: 1}
	settings := &models.DirScanSettings{MatchMode: models.MatchModeStrict}
	matcher := NewMatcher(MatchModeStrict, 0)
	enabled := map[int]struct{}{1: {}, 2: {}}

	tests := []struct {
		name         string
		response     *jackett.SearchResponse
		wantSearched bool
	}{
		{
			name: "full coverage counts as searched",
			response: &jackett.SearchResponse{
				Results:             []jackett.SearchResult{},
				RequestedIndexerIDs: []int{1, 2},
				CoveredIndexerIDs:   []int{1, 2},
			},
			wantSearched: true,
		},
		{
			name: "partial coverage stays pending",
			response: &jackett.SearchResponse{
				Results:             []jackett.SearchResult{},
				RequestedIndexerIDs: []int{1, 2},
				CoveredIndexerIDs:   []int{1},
			},
			wantSearched: false,
		},
		{
			name: "zero covered indexers stays pending",
			response: &jackett.SearchResponse{
				Results:             []jackett.SearchResult{},
				RequestedIndexerIDs: []int{1, 2},
			},
			wantSearched: false,
		},
		{
			name: "empty resolved indexer set stays pending",
			response: &jackett.SearchResponse{
				Results: []jackett.SearchResult{},
			},
			wantSearched: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := coverageTestService(tt.response)
			matches, outcome := svc.processSearchee(ctx, dir, coverageTestSearchee(), settings, matcher, 1, enabled, &l)
			require.Nil(t, matches)
			require.Equal(t, tt.wantSearched, outcome.searched)
			require.False(t, outcome.searchError)
		})
	}
}
