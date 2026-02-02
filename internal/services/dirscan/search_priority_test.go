// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"testing"

	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/stretchr/testify/require"
)

type capturingJackettSearcher struct {
	priority jackett.RateLimitPriority
	captured bool
}

func (c *capturingJackettSearcher) SearchWithScope(ctx context.Context, _ *jackett.TorznabSearchRequest, _ string) error {
	priority, ok := jackett.SearchPriority(ctx)
	if ok {
		c.priority = priority
		c.captured = true
	}
	return nil
}

func TestSearcher_Search_UsesBackgroundPriority(t *testing.T) {
	capture := &capturingJackettSearcher{}
	searcher := NewSearcher(capture, NewParser(nil))

	req := &SearchRequest{
		Searchee: &Searchee{Name: "Example.Movie.2024.1080p.WEB-DL"},
		Limit:    1,
	}

	ctx := jackett.WithSearchPriority(context.Background(), jackett.RateLimitPriorityInteractive)

	err := searcher.Search(ctx, req)
	require.NoError(t, err)

	require.True(t, capture.captured)
	require.Equal(t, jackett.RateLimitPriorityBackground, capture.priority)
}
