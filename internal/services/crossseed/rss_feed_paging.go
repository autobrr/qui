// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/services/jackett"
)

const (
	rssFeedPageSize    = 100
	rssFeedMaxPages    = 5
	rssFeedPageTimeout = 3 * time.Minute
)

type feedItemKey struct {
	guid      string
	indexerID int
}

func feedPageContinuation(pageResults []jackett.SearchResult, collected map[feedItemKey]struct{}, handled func(guid string, indexerID int) bool) ([]jackett.SearchResult, []int) {
	var fresh []jackett.SearchResult
	var continueIndexers []int
	continueSeen := make(map[int]struct{})

	for _, result := range pageResults {
		if result.GUID == "" || result.IndexerID == 0 {
			continue
		}
		key := feedItemKey{guid: result.GUID, indexerID: result.IndexerID}
		if _, ok := collected[key]; ok {
			continue
		}
		collected[key] = struct{}{}
		fresh = append(fresh, result)
		if handled(result.GUID, result.IndexerID) {
			continue
		}
		if _, ok := continueSeen[result.IndexerID]; !ok {
			continueSeen[result.IndexerID] = struct{}{}
			continueIndexers = append(continueIndexers, result.IndexerID)
		}
	}

	return fresh, continueIndexers
}

func groupIndexersByOffset(active []int, offsets map[int]int) map[int][]int {
	groups := make(map[int][]int)
	for _, id := range active {
		groups[offsets[id]] = append(groups[offsets[id]], id)
	}
	return groups
}

func (s *Service) pageAutomationFeed(ctx context.Context, firstPage *jackett.SearchResponse, maxPages int) {
	if maxPages <= 1 || s.automationStore == nil || s.jackettService == nil {
		return
	}

	handled := func(guid string, indexerID int) bool {
		seen, _, err := s.automationStore.HasProcessedFeedItem(ctx, guid, indexerID)
		if err != nil {
			log.Debug().
				Err(err).
				Str("guid", guid).
				Int("indexerID", indexerID).
				Msg("[RSS] Feed-item store check failed; not extending paging")
			return true
		}
		return seen
	}

	collected := make(map[feedItemKey]struct{})
	_, active := feedPageContinuation(firstPage.Results, collected, handled)

	pageOneCounts := make(map[int]int)
	for _, result := range firstPage.Results {
		pageOneCounts[result.IndexerID]++
	}
	offsets := make(map[int]int, len(active))
	for _, id := range active {
		offsets[id] = pageOneCounts[id]
	}

	deepCtx := jackett.WithSearchPriority(ctx, jackett.RateLimitPriorityBackground)
	for page := 2; len(active) > 0 && page <= maxPages; page++ {
		var nextActive []int
		for offset, ids := range groupIndexersByOffset(active, offsets) {
			resp, err := s.recentFeedPage(deepCtx, offset, ids)
			if err != nil {
				log.Debug().
					Err(err).
					Int("page", page).
					Int("offset", offset).
					Ints("indexerIDs", ids).
					Msg("[RSS] Deeper feed page failed; keeping results fetched so far")
				continue
			}
			firstPage.Partial = firstPage.Partial || resp.Partial

			fresh, cont := feedPageContinuation(resp.Results, collected, handled)
			firstPage.Results = append(firstPage.Results, fresh...)
			firstPage.Total = len(firstPage.Results)

			pageCounts := make(map[int]int)
			for _, result := range resp.Results {
				pageCounts[result.IndexerID]++
			}
			for _, id := range cont {
				offsets[id] += pageCounts[id]
				nextActive = append(nextActive, id)
			}

			log.Debug().
				Int("page", page).
				Int("offset", offset).
				Ints("indexerIDs", ids).
				Int("pageResults", len(resp.Results)).
				Int("freshResults", len(fresh)).
				Ints("continuing", cont).
				Msg("[RSS] Fetched deeper feed page")
		}
		active = nextActive
	}
}

func (s *Service) recentFeedPage(ctx context.Context, offset int, indexerIDs []int) (*jackett.SearchResponse, error) {
	respCh := make(chan *jackett.SearchResponse, 1)
	errCh := make(chan error, 1)
	err := s.jackettService.Recent(ctx, rssFeedPageSize, offset, indexerIDs, func(resp *jackett.SearchResponse, respErr error) {
		if respErr != nil {
			errCh <- respErr
			return
		}
		respCh <- resp
	})
	if err != nil {
		return nil, err
	}

	select {
	case resp := <-respCh:
		return resp, nil
	case respErr := <-errCh:
		return nil, respErr
	case <-time.After(rssFeedPageTimeout):
		return nil, errors.New("feed page timed out")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
