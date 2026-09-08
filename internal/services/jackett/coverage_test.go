// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestSearchResponse_FullyCovered(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		requested []int
		covered   []int
		want      bool
	}{
		{name: "all requested covered", requested: []int{1, 2}, covered: []int{1, 2}, want: true},
		{name: "extra covered ids are fine", requested: []int{1}, covered: []int{1, 2}, want: true},
		{name: "missing one requested", requested: []int{1, 2}, covered: []int{1}, want: false},
		{name: "nothing covered", requested: []int{1}, covered: nil, want: false},
		{name: "nothing requested is vacuously covered", requested: nil, covered: nil, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resp := &SearchResponse{RequestedIndexerIDs: tt.requested, CoveredIndexerIDs: tt.covered}
			require.Equal(t, tt.want, resp.FullyCovered())
		})
	}

	require.False(t, (*SearchResponse)(nil).FullyCovered())
}

func TestSearchResponseIsPartialWhenCoverageIsIncomplete(t *testing.T) {
	indexers := []*models.TorznabIndexer{
		{ID: 1, Name: "Answered", Enabled: true},
		{ID: 2, Name: "Unavailable", Enabled: true},
	}
	service := NewService(&mockTorznabIndexerStore{indexers: indexers})
	t.Cleanup(service.searchScheduler.Stop)
	service.searchExecutor = func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error) {
		return []Result{{IndexerID: 1, Title: "result"}}, []int{1}, nil
	}

	var response *SearchResponse
	req := &TorznabSearchRequest{
		Query:      "test query",
		IndexerIDs: []int{1, 2},
		CacheMode:  CacheModeBypass,
		OnAllComplete: func(resp *SearchResponse, err error) {
			require.NoError(t, err)
			response = resp
		},
	}
	require.NoError(t, service.SearchGeneric(t.Context(), req))
	require.NotNil(t, response)
	require.True(t, response.Partial)
	require.Equal(t, []int{1}, response.CoveredIndexerIDs)
}

func TestSearchDeduplicatesRequestedIndexerIDs(t *testing.T) {
	indexer := &models.TorznabIndexer{ID: 1, Name: "Answered", Enabled: true}
	service := NewService(&mockTorznabIndexerStore{indexers: []*models.TorznabIndexer{indexer}})
	t.Cleanup(service.searchScheduler.Stop)

	var searchedIndexerIDs []int
	service.searchExecutor = func(_ context.Context, indexers []*models.TorznabIndexer, _ url.Values, _ *searchContext) ([]Result, []int, error) {
		for _, indexer := range indexers {
			searchedIndexerIDs = append(searchedIndexerIDs, indexer.ID)
		}
		return []Result{{IndexerID: 1, Title: "result"}}, []int{1}, nil
	}

	var response *SearchResponse
	req := &TorznabSearchRequest{
		Query:      "test query",
		IndexerIDs: []int{1, 1},
		CacheMode:  CacheModeBypass,
		OnAllComplete: func(resp *SearchResponse, err error) {
			require.NoError(t, err)
			response = resp
		},
	}
	require.NoError(t, service.SearchGeneric(t.Context(), req))
	require.NotNil(t, response)
	require.Equal(t, []int{1}, searchedIndexerIDs)
	require.Equal(t, []int{1}, response.RequestedIndexerIDs)
	require.False(t, response.Partial)
}

func TestFilterIndexersForCapabilities_MirrorsExecutorGate(t *testing.T) {
	t.Parallel()

	indexers := []*models.TorznabIndexer{
		{ID: 1, Enabled: true, Capabilities: []string{"movie-search"}},
		{ID: 2, Enabled: true, Capabilities: []string{"music-search"}},
		// No stored caps or categories yet: the executor fetches metadata and
		// applies its own gate, so the pre-filter must keep this indexer.
		{ID: 3, Enabled: true},
		{ID: 4, Enabled: true, Capabilities: []string{"audio-search"}},
	}
	svc := NewService(&mockTorznabIndexerStore{indexers: indexers})
	ctx := context.Background()

	t.Run("keeps indexers without stored caps", func(t *testing.T) {
		got, err := svc.FilterIndexersForCapabilities(ctx, nil, []string{"movie-search"}, nil)
		require.NoError(t, err)
		require.Equal(t, []int{1, 3}, got)
	})

	t.Run("any required cap is enough", func(t *testing.T) {
		got, err := svc.FilterIndexersForCapabilities(ctx, nil, []string{"music-search", "audio-search"}, nil)
		require.NoError(t, err)
		require.Equal(t, []int{2, 3, 4}, got)
	})

	t.Run("keeps indexers without stored categories", func(t *testing.T) {
		withCategories := []*models.TorznabIndexer{
			{ID: 1, Enabled: true, Categories: []models.TorznabIndexerCategory{{CategoryID: 2000}}},
			{ID: 2, Enabled: true, Categories: []models.TorznabIndexerCategory{{CategoryID: 3000}}},
			{ID: 3, Enabled: true},
		}
		svc := NewService(&mockTorznabIndexerStore{indexers: withCategories})
		got, err := svc.FilterIndexersForCapabilities(ctx, nil, nil, []int{2000})
		require.NoError(t, err)
		require.Equal(t, []int{1, 3}, got)
	})
}
