// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveAllowedIndexerIDsRespectsSelection(t *testing.T) {
	svc := &Service{}
	state := &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      true,
		FilteredIndexers:      []int{1, 2, 3},
		CapabilityIndexers:    []int{1, 2, 3},
	}

	ids, reason := svc.resolveAllowedIndexerIDs(context.Background(), "hash", state, []int{2})
	require.Equal(t, []int{2}, ids)
	require.Equal(t, "", reason)
}

func TestResolveAllowedIndexerIDsSelectionFilteredOut(t *testing.T) {
	svc := &Service{}
	state := &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      true,
		FilteredIndexers:      []int{1, 2},
	}

	ids, reason := svc.resolveAllowedIndexerIDs(context.Background(), "hash", state, []int{99})
	require.Nil(t, ids)
	require.Equal(t, selectedIndexerContentSkipReason, reason)
}

func TestResolveAllowedIndexerIDsCapabilitySelection(t *testing.T) {
	svc := &Service{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state := &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      false,
		CapabilityIndexers:    []int{4, 5},
	}

	ids, reason := svc.resolveAllowedIndexerIDs(ctx, "hash", state, []int{4})
	require.Equal(t, []int{4}, ids)
	require.Equal(t, "", reason)

	state2 := &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      false,
		CapabilityIndexers:    []int{7, 8},
	}
	idMismatch, mismatchReason := svc.resolveAllowedIndexerIDs(ctx, "hash", state2, []int{99})
	require.Nil(t, idMismatch)
	require.Equal(t, selectedIndexerCapabilitySkipReason, mismatchReason)
}

func TestFilterIndexersBySelection_AllCandidatesReturnedWhenSelectionEmpty(t *testing.T) {
	candidates := []int{1, 2, 3}
	filtered, removed := filterIndexersBySelection(candidates, nil)
	require.False(t, removed)
	require.Equal(t, candidates, filtered)

	// ensure we returned a copy
	filtered[0] = 99
	require.Equal(t, []int{1, 2, 3}, candidates)
}

func TestFilterIndexersBySelection_ReturnsNilWhenSelectionRemovesAll(t *testing.T) {
	candidates := []int{1, 2}
	filtered, removed := filterIndexersBySelection(candidates, []int{99})
	require.Nil(t, filtered)
	require.True(t, removed)
}

func TestFilterIndexersBySelection_SelectsSubset(t *testing.T) {
	candidates := []int{1, 2, 3, 4}
	filtered, removed := filterIndexersBySelection(candidates, []int{2, 4})
	require.Equal(t, []int{2, 4}, filtered)
	require.False(t, removed)
}
