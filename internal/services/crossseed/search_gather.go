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

type SearchGatherer struct {
	Search        func(ctx context.Context, req *jackett.TorznabSearchRequest) (*jackett.SearchResponse, error)
	IDCapIndexers func(ctx context.Context, req *jackett.TorznabSearchRequest) []int
	Usable        func(jackett.SearchResult) bool
}

type GatherInput struct {
	Req            *jackett.TorznabSearchRequest
	TagSourcedIDs  bool
	AltTitle       string
	AltTitleSeason *int
	TVSourceYear   int
	TorrentName    string
	LogPrefix      string
}

var errSearchTimedOut = errors.New("search timed out")

func (s *Service) newSearchGatherer(usable func(jackett.SearchResult) bool) SearchGatherer {
	return SearchGatherer{Search: s.searchOnce, IDCapIndexers: s.jackettService.IndexerIDsWithIDSearchCaps, Usable: usable}
}

func (g SearchGatherer) Gather(ctx, waitCtx context.Context, in GatherInput) (*jackett.SearchResponse, []int, bool, error) {
	req := in.Req
	resp, err := g.Search(waitCtx, req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			err = errSearchTimedOut
		}
		return nil, nil, false, err
	}
	results := resp.Results

	covered := resp.CoveredIndexerIDs
	answered := len(covered) > 0

	dropYear := req.Year > 0 && !slices.ContainsFunc(results, g.Usable)
	addYear := in.TVSourceYear > 0 && req.Year == 0
	var idCap []int
	if dropYear || addYear || in.TagSourcedIDs {
		idCap = g.IDCapIndexers(waitCtx, req)
	}
	if dropYear {
		if targets := subtractInts(req.IndexerIDs, idCap); len(targets) > 0 {
			log.Debug().
				Str("torrentName", in.TorrentName).
				Int("year", req.Year).
				Ints("indexerIDs", targets).
				Msg(in.LogPrefix + " Nothing usable with year filter; retrying title indexers without year")

			retryReq := *req
			retryReq.Year = 0
			retryReq.IndexerIDs = targets
			retryResp, retryErr := g.Search(waitCtx, &retryReq)
			if retryErr != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return nil, nil, false, ctxErr
				}
				log.Debug().
					Err(retryErr).
					Str("torrentName", in.TorrentName).
					Ints("indexerIDs", targets).
					Msg(in.LogPrefix + " Yearless retry failed; continuing with primary results")
				resp.Partial = true
				covered = subtractInts(covered, targets)
			} else if retryResp != nil {
				results = append(results, retryResp.Results...)
				retryResp.Partial = resp.Partial || retryResp.Partial
				retryResp.RequestedIndexerIDs = resp.RequestedIndexerIDs
				if resp.JobID != 0 && len(targets) < len(req.IndexerIDs) {
					retryResp.JobID = resp.JobID
				}
				resp = retryResp
				covered = uncoverMissed(covered, targets, retryResp.CoveredIndexerIDs)
				answered = answered || len(retryResp.CoveredIndexerIDs) > 0
			}
		}
	}
	laterYear := req.Year
	if dropYear {
		laterYear = 0
	}

	retry := func(pass string, targets []int, query string, season *int, year int) error {
		if len(targets) == 0 {
			return nil
		}
		log.Debug().
			Str("torrentName", in.TorrentName).
			Str("pass", pass).
			Str("query", query).
			Int("year", year).
			Ints("indexerIDs", targets).
			Msg(in.LogPrefix + " Nothing usable from primary pass; running per-indexer retry")

		retryReq := *req
		clearSearchRequestIDs(&retryReq)
		retryReq.Query = query
		retryReq.IndexerIDs = targets
		if season != nil {
			retryReq.Season = season
		}
		retryReq.Year = year
		retryReq.SkipHistory = true
		retryResp, retryErr := g.Search(waitCtx, &retryReq)
		if retryErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			log.Debug().
				Err(retryErr).
				Str("torrentName", in.TorrentName).
				Str("pass", pass).
				Ints("indexerIDs", targets).
				Msg(in.LogPrefix + " Per-indexer retry failed; continuing with primary results")
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
				Str("torrentName", in.TorrentName).
				Str("pass", pass).
				Int("results", len(retryResp.Results)).
				Ints("indexerIDs", targets).
				Msg(in.LogPrefix + " Per-indexer retry returned additional candidates")
			results = append(results, retryResp.Results...)
			resp.Partial = resp.Partial || retryResp.Partial
		}
		return nil
	}
	unsatisfied := func() []int { return indexersWithoutUsableResults(req.IndexerIDs, results, g.Usable) }

	if addYear {
		if err := retry("year", subtractInts(unsatisfied(), idCap), req.Query, nil, in.TVSourceYear); err != nil {
			return nil, nil, false, err
		}
	}

	if in.TagSourcedIDs {
		targets := intersectInts(idCap, unsatisfied())
		if err := retry("title retry after tag-sourced IDs", targets, req.Query, nil, laterYear); err != nil {
			return nil, nil, false, err
		}
	}

	if !req.OmitQueryForIDs || in.TagSourcedIDs {
		if in.AltTitle != "" {
			if err := retry("alternate title", unsatisfied(), in.AltTitle, in.AltTitleSeason, laterYear); err != nil {
				return nil, nil, false, err
			}
		}
		if altQuery, ok := alternateConnectorQuery(req.Query); ok {
			if err := retry("alternate connector", unsatisfied(), altQuery, nil, laterYear); err != nil {
				return nil, nil, false, err
			}
		}
	}

	resp.Results = results
	return resp, covered, answered, nil
}

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

func indexersWithoutUsableResults(requestedIDs []int, results []jackett.SearchResult, usable func(jackett.SearchResult) bool) []int {
	kept := make([]jackett.SearchResult, 0, len(results))
	for _, r := range results {
		if usable(r) {
			kept = append(kept, r)
		}
	}
	return indexersWithoutResults(requestedIDs, kept)
}

func clearSearchRequestIDs(req *jackett.TorznabSearchRequest) {
	req.IMDbID = ""
	req.TVDbID = ""
	req.TMDbID = 0
	req.TVMazeID = 0
	req.EpisodeMap = nil
	req.OmitQueryForIDs = false
}

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
