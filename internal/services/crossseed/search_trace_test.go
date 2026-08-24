// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"errors"
	"testing"

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
