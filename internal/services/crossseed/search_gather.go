// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/services/jackett"
)

// searchGatherer runs the Torznab primary pass and the retry passes of one
// cross-seed search. It decides retries with the usable predicate it is handed
// and never parses or classifies a title itself (ADR 0007).
type searchGatherer struct {
	search        func(ctx context.Context, req *jackett.TorznabSearchRequest) (*jackett.SearchResponse, error)
	idCapIndexers func(ctx context.Context, req *jackett.TorznabSearchRequest) []int
	usable        func(jackett.SearchResult) bool
}

type gatherInput struct {
	req           *jackett.TorznabSearchRequest
	tagSourcedIDs bool
	// altTitle is the alternate-title query; empty skips that pass.
	altTitle    string
	torrentName string
}

// gather returns the response with every pass's results merged, the covered
// indexer IDs, whether any indexer answered any pass, and the primary-pass or
// context error. Passes search on waitCtx.
// Only ctx cancellation aborts a retry. A failed yearless retry marks the
// response partial. A failed per-indexer retry removes its targets from coverage.
func (g searchGatherer) gather(ctx, waitCtx context.Context, in gatherInput) (*jackett.SearchResponse, []int, bool, error) {
	req := in.req
	resp, err := g.search(waitCtx, req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			err = errors.New("search timed out")
		}
		return nil, nil, false, err
	}
	results := resp.Results

	// An indexer only counts as covered when it answered every pass of this
	// search: a pass it missed is exactly the query that might have matched,
	// so it must stay eligible for the next run.
	covered := resp.CoveredIndexerIDs
	// A failed retry uncovers indexers that did answer, so coverage cannot
	// tell whether Torznab ran at all.
	answered := len(covered) > 0
	yearlessRetryRan := false

	// Retry without year when the first pass turned up nothing usable, whether
	// it returned no hits at all or only hits that release and size filtering
	// rejected. The year is the narrowest primary-query constraint, so it is
	// the first fallback to drop. This pass stays gated on the whole search:
	// dropping the year fires on nearly every movie and has low per-indexer
	// value, unlike the targeted passes below.
	if req.Year > 0 && !slices.ContainsFunc(results, g.usable) {
		log.Debug().
			Str("torrentName", in.torrentName).
			Int("year", req.Year).
			Msg("[CROSSSEED-SEARCH] Zero results with year filter; retrying without year")

		retryReq := *req
		retryReq.Year = 0
		retryResp, retryErr := g.search(waitCtx, &retryReq)
		if retryErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, nil, false, ctxErr
			}
			log.Debug().
				Err(retryErr).
				Str("torrentName", in.torrentName).
				Msg("[CROSSSEED-SEARCH] Yearless retry failed; continuing with primary results")
			resp.Partial = true
			covered = nil
		} else if retryResp != nil {
			// The retry is a tracked job of its own, so JobID and Cache come
			// from it; the merged results keep both passes.
			results = append(results, retryResp.Results...)
			retryResp.Partial = resp.Partial || retryResp.Partial
			resp = retryResp
			covered = intersectInts(covered, retryResp.CoveredIndexerIDs)
			answered = answered || len(retryResp.CoveredIndexerIDs) > 0
			yearlessRetryRan = true
		}
	}

	// retry is a per-indexer retry: it re-queries only targets with query, as
	// an internal continuation of the primary search. No history row of its
	// own, IDs cleared so the title reaches ID-capable indexers, and the year
	// the latest pass actually used. SkipCachePersist stays unset so repeated
	// passes reuse the Torznab result cache instead of re-hitting indexers.
	// Outcome reporting keys on indexer ID under the primary job. A failed
	// pass drops its targets from the covered set.
	retry := func(pass string, targets []int, query string) error {
		if len(targets) == 0 {
			return nil
		}
		log.Debug().
			Str("torrentName", in.torrentName).
			Str("pass", pass).
			Str("query", query).
			Ints("indexerIDs", targets).
			Msg("[CROSSSEED-SEARCH] Nothing usable from primary pass; running per-indexer retry")

		retryReq := *req
		clearSearchRequestIDs(&retryReq)
		retryReq.Query = query
		retryReq.IndexerIDs = targets
		if yearlessRetryRan {
			retryReq.Year = 0
		}
		retryReq.SkipHistory = true
		retryResp, retryErr := g.search(waitCtx, &retryReq)
		if retryErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			log.Debug().
				Err(retryErr).
				Str("torrentName", in.torrentName).
				Str("pass", pass).
				Ints("indexerIDs", targets).
				Msg("[CROSSSEED-SEARCH] Per-indexer retry failed; continuing with primary results")
			covered = subtractInts(covered, targets)
			return nil
		}
		if retryResp == nil {
			return nil
		}
		covered = uncoverMissed(covered, targets, retryResp.CoveredIndexerIDs)
		answered = answered || len(retryResp.CoveredIndexerIDs) > 0
		if len(retryResp.Results) > 0 {
			log.Debug().
				Str("torrentName", in.torrentName).
				Str("pass", pass).
				Int("results", len(retryResp.Results)).
				Ints("indexerIDs", targets).
				Msg("[CROSSSEED-SEARCH] Per-indexer retry returned additional candidates")
			results = append(results, retryResp.Results...)
			resp.Partial = resp.Partial || retryResp.Partial
		}
		return nil
	}
	unsatisfied := func() []int { return indexersWithoutUsableResults(req.IndexerIDs, results, g.usable) }

	// Indexers that searched by tag-sourced ID never saw the title query.
	// Retry by title so an incorrect tag does not leave them without usable
	// results. Indexers without ID caps already searched by title in the
	// primary pass and are covered by the passes below.
	if in.tagSourcedIDs {
		targets := intersectInts(g.idCapIndexers(waitCtx, req), unsatisfied())
		if err := retry("title retry after tag-sourced IDs", targets, req.Query); err != nil {
			return nil, nil, false, err
		}
	}

	// The title passes are skipped for arr-ID searches, which do not rely on
	// title text; a tag-sourced ID primary keeps them because the IDs came
	// from the file, not a resolver, and the retry request drops them.
	if !req.OmitQueryForIDs || in.tagSourcedIDs {
		// A tracker can index the same content under a different title
		// (localized, romanized, or an *arr scene alias). An indexer that
		// already produced a usable candidate is left alone, because
		// cross-seed success is per tracker, not per search.
		if in.altTitle != "" {
			if err := retry("alternate title", unsatisfied(), in.altTitle); err != nil {
				return nil, nil, false, err
			}
		}
		// Some trackers index a show with "&" while the release name spells
		// out "and". A literal q only matches one spelling; the match loop
		// dedupes the merged candidates by GUID/download URL.
		if altQuery, ok := alternateConnectorQuery(req.Query); ok {
			if err := retry("alternate connector", unsatisfied(), altQuery); err != nil {
				return nil, nil, false, err
			}
		}
	}

	resp.Results = results
	return resp, covered, answered, nil
}

// indexersWithoutResults returns the requested indexer IDs that produced no
// results, preserving request order.
func indexersWithoutResults(requestedIDs []int, results []jackett.SearchResult) []int {
	if len(requestedIDs) == 0 {
		return nil
	}
	responded := make(map[int]struct{}, len(results))
	for _, r := range results {
		responded[r.IndexerID] = struct{}{}
	}
	var missing []int
	for _, id := range requestedIDs {
		if _, seen := responded[id]; !seen {
			missing = append(missing, id)
		}
	}
	return missing
}

// indexersWithoutUsableResults returns the requested indexer IDs that produced
// no USABLE candidate. Unlike indexersWithoutResults (which counts any raw
// hit), an indexer whose hits were all rejected by release/size filtering is
// still re-queried by the per-indexer retry passes, so a candidate it carries
// under another title, spelling, or ID can surface instead of being
// permanently suppressed.
func indexersWithoutUsableResults(requestedIDs []int, results []jackett.SearchResult, usable func(jackett.SearchResult) bool) []int {
	kept := make([]jackett.SearchResult, 0, len(results))
	for _, r := range results {
		if usable(r) {
			kept = append(kept, r)
		}
	}
	return indexersWithoutResults(requestedIDs, kept)
}

// clearSearchRequestIDs strips the external-ID parameters from a retry
// request copied off an ID-driven primary, so its title query reaches every
// indexer instead of being dropped for the ID-capable ones.
func clearSearchRequestIDs(req *jackett.TorznabSearchRequest) {
	req.IMDbID = ""
	req.TVDbID = ""
	req.TMDbID = 0
	req.TVMazeID = 0
	req.EpisodeMap = nil
	req.OmitQueryForIDs = false
}

// searchOnce runs a single Torznab search to completion and returns its response.
func (s *Service) searchOnce(ctx context.Context, req *jackett.TorznabSearchRequest) (*jackett.SearchResponse, error) {
	respCh := make(chan *jackett.SearchResponse, 1)
	errCh := make(chan error, 1)
	var once sync.Once
	req.OnAllComplete = func(resp *jackett.SearchResponse, err error) {
		once.Do(func() {
			if err != nil {
				select {
				case errCh <- err:
				case <-ctx.Done():
				}
				return
			}
			select {
			case respCh <- resp:
			case <-ctx.Done():
			}
		})
	}
	if err := s.jackettService.Search(ctx, req); err != nil {
		return nil, err
	}
	select {
	case resp := <-respCh:
		return resp, nil
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
