// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"fmt"

	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/jackett"
)

func (s *Searcher) gatherer(usable func(jackett.SearchResult) bool) crossseed.SearchGatherer {
	return crossseed.SearchGatherer{Search: s.searchPass, IDCapIndexers: s.jackettService.IndexerIDsWithIDSearchCaps, Usable: usable}
}

func (s *Searcher) searchPass(ctx context.Context, req *jackett.TorznabSearchRequest) (*jackett.SearchResponse, error) {
	ctx = jackett.WithSearchPriority(ctx, jackett.RateLimitPriorityBackground)
	respCh := make(chan *jackett.SearchResponse, 1)
	errCh := make(chan error, 1)
	req.OnAllComplete = func(response *jackett.SearchResponse, err error) {
		if err != nil {
			errCh <- err
			return
		}
		respCh <- response
	}
	if err := s.jackettService.SearchWithScope(ctx, req, SearchScope); err != nil {
		return nil, fmt.Errorf("search indexers: %w", err)
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errCh:
		return nil, err
	case response := <-respCh:
		return response, nil
	}
}
