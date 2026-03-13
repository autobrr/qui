// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheAutomationCandidateResponse_BoundedSize(t *testing.T) {
	t.Parallel()

	autoCtx := &automationContext{
		candidateCache: make(map[string]*FindCandidatesResponse),
		candidateOrder: make([]string, 0, automationCandidateCacheMaxEntries),
	}

	total := automationCandidateCacheMaxEntries + 25
	for i := 0; i < total; i++ {
		key := fmt.Sprintf("release-%d", i)
		cacheAutomationCandidateResponse(autoCtx, key, &FindCandidatesResponse{})
	}

	require.Len(t, autoCtx.candidateCache, automationCandidateCacheMaxEntries)
	assert.NotContains(t, autoCtx.candidateCache, "release-0")
	assert.Contains(t, autoCtx.candidateCache, fmt.Sprintf("release-%d", total-1))
}

func TestCacheAutomationCandidateResponse_DuplicateKeyDoesNotGrowOrder(t *testing.T) {
	t.Parallel()

	autoCtx := &automationContext{
		candidateCache: make(map[string]*FindCandidatesResponse),
		candidateOrder: make([]string, 0, automationCandidateCacheMaxEntries),
	}

	cacheAutomationCandidateResponse(autoCtx, "same-release", &FindCandidatesResponse{})
	cacheAutomationCandidateResponse(autoCtx, "same-release", &FindCandidatesResponse{})

	require.Len(t, autoCtx.candidateCache, 1)
	require.Len(t, autoCtx.candidateOrder, 1)
	assert.Equal(t, "same-release", autoCtx.candidateOrder[0])
}
