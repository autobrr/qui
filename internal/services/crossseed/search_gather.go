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

func (s *Service) searchGatherer(usable func(jackett.SearchResult) bool) searchGatherer {
	return searchGatherer{
		search:        s.searchOnce,
		idCapIndexers: s.jackettService.IndexerIDsWithIDSearchCaps,
		usable:        usable,
	}
}

type gatherInput struct {
	req           *jackett.TorznabSearchRequest
	tagSourcedIDs bool
	// altTitle is the alternate-title query; empty skips that pass.
	altTitle    string
	torrentName string
}

// gather returns the response with every pass's results merged, the covered
// indexer IDs, and the primary-pass or context error. Passes search on waitCtx;
// only a dead ctx aborts a retry. A retry that hits the waitCtx deadline marks
// the response partial and continues.
func (g searchGatherer) gather(ctx, waitCtx context.Context, in gatherInput) (*jackett.SearchResponse, []int, error) {
	req := in.req
	resp, err := g.search(waitCtx, req)
	if err != nil {
		if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
			err = errors.New("search timed out")
		}
		return nil, nil, err
	}
	results := resp.Results

	// An indexer only counts as covered when it answered every pass of this
	// search: a pass it missed is exactly the query that might have matched,
	// so it must stay eligible for the next run.
	covered := resp.CoveredIndexerIDs
	yearlessRetryRan := false

	// Retry without year when the first pass turned up nothing usable, whether
	// it returned no hits at all or only hits that release and size filtering
	// rejected. The year is the narrowest primary-query constraint, so it is
	// the first fallback to drop. This pass stays gated on the whole search:
	// dropping the year fires on nearly every movie and has low per-indexer
	// value, unlike the targeted passes below.
	if req.Year > 0 && !hasUsableSearchResult(results, g.usable) {
		log.Debug().
			Str("torrentName", in.torrentName).
			Int("year", req.Year).
			Msg("[CROSSSEED-SEARCH] Zero results with year filter; retrying without year")

		retryReq := *req
		retryReq.Year = 0
		retryResp, retryErr := g.search(waitCtx, &retryReq)
		if retryErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, nil, ctxErr
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
			yearlessRetryRan = true
		}
	}

	// retry re-queries only the targeted indexers with query, as an internal
	// continuation of the primary search: no history row of its own, IDs
	// cleared so the title reaches ID-capable indexers, and the year the
	// latest pass actually used. Outcome reporting keys on indexer ID under
	// the primary job. A failed pass drops its targets from the covered set.
	retry := func(pass string, targets []int, query string) error {
		if len(targets) == 0 {
			return nil
		}
		log.Debug().
			Str("torrentName", in.torrentName).
			Str("pass", pass).
			Str("query", query).
			Ints("indexerIDs", targets).
			Msg("[CROSSSEED-SEARCH] Nothing usable from primary pass; retrying targeted indexers")

		retryReq := *req
		clearSearchRequestIDs(&retryReq)
		retryReq.Query = query
		retryReq.IndexerIDs = targets
		retryReq.Year = effectiveSearchYear(req.Year, yearlessRetryRan)
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
				Msg("[CROSSSEED-SEARCH] Retry pass failed; continuing with primary results")
			covered = subtractInts(covered, targets)
			return nil
		}
		if retryResp == nil {
			return nil
		}
		covered = uncoverMissed(covered, targets, retryResp.CoveredIndexerIDs)
		if len(retryResp.Results) > 0 {
			log.Debug().
				Str("torrentName", in.torrentName).
				Str("pass", pass).
				Int("results", len(retryResp.Results)).
				Ints("indexerIDs", targets).
				Msg("[CROSSSEED-SEARCH] Retry pass returned additional candidates")
			results, resp.Partial = mergeAltConnectorResults(resp.Partial, results, retryResp)
		}
		return nil
	}
	unsatisfied := func() []int { return indexersWithoutUsableResults(req.IndexerIDs, results, g.usable) }

	// Title rescue for the tag-sourced ID primary: indexers that searched by
	// ID never saw the title query, so a wrong or unrecognized muxer tag would
	// end their search with nothing and no rescue. Indexers without ID caps
	// already searched by title in the primary pass and are covered by the
	// passes below.
	if in.tagSourcedIDs {
		targets := intersectInts(g.idCapIndexers(waitCtx, req), unsatisfied())
		if err := retry("title rescue", targets, req.Query); err != nil {
			return nil, nil, err
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
				return nil, nil, err
			}
		}
		// Some trackers index a show with "&" while the release name spells
		// out "and". A literal q only matches one spelling; the match loop
		// dedupes the merged candidates by GUID/download URL.
		if altQuery, ok := alternateConnectorQuery(req.Query); ok {
			if err := retry("alternate connector", unsatisfied(), altQuery); err != nil {
				return nil, nil, err
			}
		}
	}

	resp.Results = results
	return resp, covered, nil
}

// effectiveSearchYear returns the year actually used by the latest search pass: 0
// once the yearless retry has run, otherwise the originally requested year. The
// targeted passes use this so they do not re-apply a year the primary search
// already proved ineffective.
func effectiveSearchYear(requestedYear int, yearlessRetryRan bool) int {
	if yearlessRetryRan {
		return 0
	}
	return requestedYear
}

// mergeAltConnectorResults appends a retry pass's results to the primary
// results and returns the combined partial flag, so an incomplete retry pass
// is never reported as a complete search. Callers invoke it only when alt
// carries results.
func mergeAltConnectorResults(primaryPartial bool, primaryResults []jackett.SearchResult, alt *jackett.SearchResponse) ([]jackett.SearchResult, bool) {
	return append(primaryResults, alt.Results...), primaryPartial || alt.Partial
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
// still re-queried by the targeted retry passes, so a candidate it carries
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

// hasUsableSearchResult reports whether any result would survive release and
// size filtering. The retry ladder gates on this rather than on the raw result
// count: hits that were all rejected leave the search just as empty as no hits
// at all, so the retry that could still find the match has to run.
func hasUsableSearchResult(results []jackett.SearchResult, usable func(jackett.SearchResult) bool) bool {
	return slices.ContainsFunc(results, usable)
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
