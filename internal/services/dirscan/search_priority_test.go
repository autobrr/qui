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
	return nil
}

func TestSearcher_Search(t *testing.T) {
	tests := []struct {
		name     string
		setupCtx func(context.Context) context.Context
	}{
		{
			name: "uses background priority",
			setupCtx: func(ctx context.Context) context.Context {
				return jackett.WithSearchPriority(ctx, jackett.RateLimitPriorityInteractive)
			},
		},
		{
			name: "uses fixed torznab window and returns all results",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture := &capturingJackettSearcher{}
			searcher := NewSearcher(capture, NewParser(nil))

			req := &SearchRequest{
				Searchee: &Searchee{Name: "Example.Movie.2024.1080p.WEB-DL"},
			}

			ctx := context.Background()
			if tt.setupCtx != nil {
				ctx = tt.setupCtx(ctx)
			}

			err := searcher.Search(ctx, req)
			require.NoError(t, err)

			require.True(t, capture.captured)
			require.Equal(t, jackett.RateLimitPriorityBackground, capture.priority)
			require.NotNil(t, capture.req)
			require.Equal(t, SearchScope, capture.scope)
			require.Equal(t, torznabDirScanSearchLimit, capture.req.Limit)
			require.True(t, capture.req.ReturnAllResults)
		})
	}
}
