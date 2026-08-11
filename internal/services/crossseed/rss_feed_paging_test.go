// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/jackett"
)

func feedResult(guid string, indexerID int) jackett.SearchResult {
	return jackett.SearchResult{GUID: guid, IndexerID: indexerID, Title: guid}
}

func TestFeedPageContinuation(t *testing.T) {
	neverHandled := func(string, int) bool { return false }
	alwaysHandled := func(string, int) bool { return true }

	t.Run("unhandled items extend paging", func(t *testing.T) {
		collected := make(map[feedItemKey]struct{})
		fresh, cont := feedPageContinuation([]jackett.SearchResult{
			feedResult("a", 1),
			feedResult("b", 1),
		}, collected, neverHandled)
		require.Len(t, fresh, 2)
		require.Equal(t, []int{1}, cont)
	})

	t.Run("fully known page stops paging but still returns fresh results", func(t *testing.T) {
		collected := make(map[feedItemKey]struct{})
		fresh, cont := feedPageContinuation([]jackett.SearchResult{
			feedResult("a", 1),
		}, collected, alwaysHandled)
		require.Len(t, fresh, 1, "known items are still new to this run and must be kept")
		require.Empty(t, cont)
	})

	t.Run("repeated page yields nothing fresh and stops", func(t *testing.T) {
		collected := make(map[feedItemKey]struct{})
		page := []jackett.SearchResult{feedResult("a", 1), feedResult("b", 1)}
		fresh, cont := feedPageContinuation(page, collected, neverHandled)
		require.Len(t, fresh, 2)
		require.Equal(t, []int{1}, cont)

		// An indexer that ignores the offset parameter returns the same page.
		fresh, cont = feedPageContinuation(page, collected, neverHandled)
		require.Empty(t, fresh)
		require.Empty(t, cont)
	})

	t.Run("indexers decide independently", func(t *testing.T) {
		collected := make(map[feedItemKey]struct{})
		handled := func(guid string, _ int) bool { return guid == "known" }
		fresh, cont := feedPageContinuation([]jackett.SearchResult{
			feedResult("known", 1),
			feedResult("new", 2),
		}, collected, handled)
		require.Len(t, fresh, 2)
		require.Equal(t, []int{2}, cont)
	})

	t.Run("results without a GUID never extend paging", func(t *testing.T) {
		collected := make(map[feedItemKey]struct{})
		fresh, cont := feedPageContinuation([]jackett.SearchResult{
			{IndexerID: 1, Title: "no guid"},
			{GUID: "x", Title: "no indexer"},
		}, collected, neverHandled)
		require.Empty(t, fresh)
		require.Empty(t, cont)
	})

	t.Run("same GUID on two indexers stays distinct", func(t *testing.T) {
		collected := make(map[feedItemKey]struct{})
		fresh, cont := feedPageContinuation([]jackett.SearchResult{
			feedResult("a", 1),
			feedResult("a", 2),
		}, collected, neverHandled)
		require.Len(t, fresh, 2)
		require.Equal(t, []int{1, 2}, cont)
	})
}

func TestGroupIndexersByOffset(t *testing.T) {
	groups := groupIndexersByOffset([]int{1, 2, 3}, map[int]int{1: 100, 2: 50, 3: 100})
	require.Equal(t, map[int][]int{100: {1, 3}, 50: {2}}, groups)
}
