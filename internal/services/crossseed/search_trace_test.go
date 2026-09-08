// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/autobrr/autobrr/pkg/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/stringutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/jackett"
)

func TestSearchTraceRejectionsCapsPerReason(t *testing.T) {
	rejections := &searchTraceRejections{}
	counts := make(map[string]int)
	for i := range 8 {
		counts["hdr mismatch"]++
		rejections.add(jackett.SearchResult{IndexerID: 1, Indexer: "alpha", Title: "Show.S01.1080p", Size: int64(i)}, "hdr mismatch", counts["hdr mismatch"])
	}
	counts["size mismatch"]++
	rejections.add(jackett.SearchResult{IndexerID: 2, Indexer: "beta", Title: "Show.S01.720p"}, "size mismatch", counts["size mismatch"])

	require.Len(t, rejections.candidates, maxTraceRejectedPerReason+1)
	assert.Equal(t, "size mismatch", rejections.candidates[maxTraceRejectedPerReason].Reason)
}

func TestSearchIndexerErrorsKeepsFirstError(t *testing.T) {
	capture := &searchIndexerErrors{}
	capture.record(1, 7, errors.New("connection refused"))
	capture.record(2, 7, errors.New("second failure"))
	capture.record(3, 8, nil)
	capture.record(4, 0, errors.New("no indexer id"))

	snap := capture.snapshot()
	assert.Equal(t, map[int]string{7: "connection refused"}, snap)
}

func TestBuildSearchIndexerOutcomes(t *testing.T) {
	tests := []struct {
		name       string
		requested  []int
		covered    []int
		errs       map[int]string
		excluded   map[int]string
		candidates map[int]int
		want       []SearchIndexerOutcome
	}{
		{
			name: "empty inputs return nil",
			want: nil,
		},
		{
			name:       "statuses partition the requested set",
			requested:  []int{4, 1, 2, 3},
			covered:    []int{1},
			errs:       map[int]string{2: "timeout"},
			excluded:   map[int]string{3: "already seeded from this tracker"},
			candidates: map[int]int{1: 6},
			want: []SearchIndexerOutcome{
				{IndexerID: 1, Status: searchIndexerStatusSearched, Candidates: 6},
				{IndexerID: 2, Status: searchIndexerStatusError, Detail: "timeout"},
				{IndexerID: 3, Status: searchIndexerStatusExcluded, Detail: "already seeded from this tracker"},
				{IndexerID: 4, Status: searchIndexerStatusNotCovered},
			},
		},
		{
			name:      "excluded indexers outside the requested set are appended",
			requested: []int{1},
			covered:   []int{1},
			excluded:  map[int]string{9: "has matching content"},
			want: []SearchIndexerOutcome{
				{IndexerID: 1, Status: searchIndexerStatusSearched},
				{IndexerID: 9, Status: searchIndexerStatusExcluded, Detail: "has matching content"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSearchIndexerOutcomes(tt.requested, tt.covered, tt.errs, tt.excluded, tt.candidates)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Regression (PR #2427 review): the breakdown subtracts the late content
// filter from the candidate total, so the total and the per-indexer counts
// must come from the raw results, before that filter removes any.
func TestSearchTorrentMatches_TraceCountsRawCandidates(t *testing.T) {
	const (
		instanceID = 1
		sourceHash = "0123456789abcdef0123456789abcdef01234567"
		sourceName = "Example.Show.S01E01.1080p.WEB-DL.DDP5.1.H.264-GROUP"
	)

	filterCache := ttlcache.New(ttlcache.Options[string, *AsyncIndexerFilteringState]{})
	cacheKey := asyncFilteringCacheKey(instanceID, sourceHash)
	filterCache.Set(cacheKey, &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		CapabilityIndexers:    []int{1},
		FilteredIndexers:      []int{1},
	}, ttlcache.DefaultTTL)

	// The content filter finishes while the search runs and excludes the only
	// indexer, so its one result is dropped after it was already returned.
	lateState := &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      true,
		CapabilityIndexers:    []int{1},
		FilteredIndexers:      nil,
		ExcludedIndexers:      map[int]string{1: "already seeded from Tracker One"},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		filterCache.Set(cacheKey, lateState, ttlcache.DefaultTTL)
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprintf(w, `<rss version="2.0"><channel><title>Tracker One</title><item><title>%s</title><guid>candidate-guid</guid><size>123</size><enclosure url="https://example.invalid/candidate.torrent" length="123" type="application/x-bittorrent" /></item></channel></rss>`, sourceName)
	}))
	t.Cleanup(server.Close)

	instance := &models.Instance{ID: instanceID, Name: "main"}
	source := qbt.Torrent{Hash: sourceHash, Name: sourceName, Size: 123, Progress: 1}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		jackettService: newJackettServiceWithIndexers([]*models.TorznabIndexer{
			{ID: 1, Name: "Tracker One", BaseURL: server.URL, Backend: models.TorznabBackendNative, TimeoutSeconds: 5, Enabled: true},
		}),
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{
			sourceHash: {{Name: sourceName + ".mkv", Size: source.Size}},
		}),
		asyncFilteringCache: filterCache,
		releaseCache:        NewReleaseCache(),
		stringNormalizer:    stringutils.NewDefaultNormalizer(),
	}

	resp, _, _, err := service.searchTorrentMatches(context.Background(), instanceID, sourceHash, TorrentSearchOptions{}, nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.DecisionTrace)

	trace := resp.DecisionTrace
	assert.Equal(t, 1, trace.TotalResults, "the raw candidate still counts toward the total")
	assert.Equal(t, 1, trace.LateContentFiltered)
	assert.Equal(t, 0, trace.FinalMatches)
	require.Len(t, trace.Indexers, 1)
	assert.Equal(t, 1, trace.Indexers[0].Candidates, "the indexer contributed one raw candidate")
}
