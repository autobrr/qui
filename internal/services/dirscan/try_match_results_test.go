// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/jackett"
)

func TestTryMatchResultsPerIndexer_SkipsAfterMatchPerIndexer(t *testing.T) {
	results := []jackett.SearchResult{
		{IndexerID: 1, Title: "A", Size: 100},
		{IndexerID: 1, Title: "B", Size: 110},
		{IndexerID: 2, Title: "C", Size: 120},
		{IndexerID: 2, Title: "D", Size: 130},
		{IndexerID: 3, Title: "E", Size: 140},
	}

	callCounts := map[int]int{}
	attemptedTitles := []string{}

	matches, stats := tryMatchResultsPerIndexer(
		results,
		0,
		0,
		func(result *jackett.SearchResult) bool {
			// Simulate "already exists" skipping for indexer 3.
			return result.IndexerID == 3
		},
		func(result *jackett.SearchResult) *searcheeMatch {
			callCounts[result.IndexerID]++
			attemptedTitles = append(attemptedTitles, result.Title)

			switch result.IndexerID {
			case 1:
				return &searcheeMatch{searchResult: result}
			case 2:
				// First result from indexer 2 fails, second matches.
				if callCounts[result.IndexerID] == 1 {
					return nil
				}
				return &searcheeMatch{searchResult: result}
			default:
				return nil
			}
		},
	)

	require.Len(t, matches, 2)
	require.Equal(t, 3, stats.attemptedMatches, "should only attempt A, C, D")
	require.Equal(t, 1, stats.skippedIndexerSatisfied, "should skip B after A matched for indexer 1")
	require.Equal(t, 1, stats.skippedExists, "should skip E due to existence check")

	require.Equal(t, []string{"A", "C", "D"}, attemptedTitles)
}

func TestTryMatchResultsPerIndexer_SizeFiltering(t *testing.T) {
	results := []jackett.SearchResult{
		{IndexerID: 1, Title: "A", Size: 100},
		{IndexerID: 1, Title: "B", Size: 110},
		{IndexerID: 2, Title: "C", Size: 120},
		{IndexerID: 2, Title: "D", Size: 130},
	}

	attemptedTitles := []string{}
	matches, stats := tryMatchResultsPerIndexer(
		results,
		125,
		0,
		nil,
		func(result *jackett.SearchResult) *searcheeMatch {
			attemptedTitles = append(attemptedTitles, result.Title)
			return &searcheeMatch{searchResult: result}
		},
	)

	require.Len(t, matches, 1)
	require.Equal(t, []string{"D"}, attemptedTitles)
	require.Equal(t, 3, stats.skippedTooSmall)
	require.Equal(t, 1, stats.attemptedMatches)
}

func TestTryMatchResultsPerIndexer_DoesNotSatisfyOnInjectionFailure(t *testing.T) {
	results := []jackett.SearchResult{
		{IndexerID: 1, Title: "A", Size: 100},
		{IndexerID: 1, Title: "B", Size: 100},
	}

	attemptedTitles := []string{}
	callCount := 0

	matches, stats := tryMatchResultsPerIndexer(
		results,
		0,
		0,
		nil,
		func(result *jackett.SearchResult) *searcheeMatch {
			attemptedTitles = append(attemptedTitles, result.Title)
			callCount++

			if callCount == 1 {
				return &searcheeMatch{searchResult: result, injectionFailed: true}
			}
			return &searcheeMatch{searchResult: result}
		},
	)

	require.Len(t, matches, 2)
	require.Equal(t, 2, stats.attemptedMatches)
	require.Equal(t, 0, stats.skippedIndexerSatisfied)
	require.Equal(t, []string{"A", "B"}, attemptedTitles)
}
