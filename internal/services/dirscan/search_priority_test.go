// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/jackett"
)

type capturingJackettSearcher struct {
	noIDSearchCaps
	priority jackett.RateLimitPriority
	req      *jackett.TorznabSearchRequest
	scope    string
	captured bool
}

func (c *capturingJackettSearcher) SearchWithScope(ctx context.Context, req *jackett.TorznabSearchRequest, scope string) error {
	c.req = req
	c.scope = scope
	priority, ok := jackett.SearchPriority(ctx)
	if ok {
		c.priority = priority
		c.captured = true
	}
	req.OnAllComplete(&jackett.SearchResponse{}, nil)
	return nil
}

func TestSearcher_SearchUsesBackgroundPriorityAndFixedWindow(t *testing.T) {
	capture := &capturingJackettSearcher{}
	searcher := NewSearcher(capture, NewParser(nil))
	meta := searcher.parser.Parse("Example.Movie.2024.1080p.WEB-DL")

	ctx := jackett.WithSearchPriority(t.Context(), jackett.RateLimitPriorityInteractive)
	_, err := searcher.searchPass(ctx, searcher.buildSearchRequest(meta, nil, nil))
	require.NoError(t, err)

	require.True(t, capture.captured)
	require.Equal(t, jackett.RateLimitPriorityBackground, capture.priority)
	require.NotNil(t, capture.req)
	require.Equal(t, SearchScope, capture.scope)
	require.Equal(t, torznabDirScanSearchLimit, capture.req.Limit)
	require.True(t, capture.req.ReturnAllResults)
}
