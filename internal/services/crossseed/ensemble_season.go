// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
)

// Ensemble season search synthesizes virtual "season pack" queue entries from
// groups of seeded loose episodes, so a normal library run can search trackers
// for the pack and hand admitted candidates to the season-pack assembly
// pipeline via the regular cross-seed apply path. Virtual entries ride the
// ordinary search queue under a synthetic pseudo-hash, which reuses the
// per-hash search-history cooldown unchanged: new episodes in a season do not
// change the pseudo-hash, so they do not bypass the cooldown.
const (
	ensembleSeasonHashPrefix = "season:"

	// ensembleMinEpisodes is the seeded-episode floor before a season is worth
	// a pack search. Deliberately not a setting.
	ensembleMinEpisodes = 3

	// ponytail: hard cap on apply attempts (each downloads a .torrent) per
	// group per run; raise only if field data shows viable packs ranked below
	// the cap.
	ensembleMaxApplyAttempts = 3
)

// ensembleGroupKey identifies a show season assembled from loose episodes.
// Season 0 groups absolute-numbered (seasonless anime) episodes.
type ensembleGroupKey struct {
	normalizedTitle string
	season          int
}

func isEnsembleSeasonCandidate(hash string) bool {
	return strings.HasPrefix(hash, ensembleSeasonHashPrefix)
}

func ensembleSeasonPseudoHash(key ensembleGroupKey) string {
	return fmt.Sprintf("%s%s:s%02d", ensembleSeasonHashPrefix, key.normalizedTitle, key.season)
}

// parseEnsembleSeasonPseudoHash recovers the group identity from the synthetic
// hash. Identity deliberately never round-trips through rls re-parsing of the
// virtual display name, which is lossy for unusual titles.
func parseEnsembleSeasonPseudoHash(hash string) (ensembleGroupKey, bool) {
	rest, ok := strings.CutPrefix(hash, ensembleSeasonHashPrefix)
	if !ok {
		return ensembleGroupKey{}, false
	}
	idx := strings.LastIndex(rest, ":s")
	if idx <= 0 {
		return ensembleGroupKey{}, false
	}
	season, err := strconv.Atoi(rest[idx+2:])
	if err != nil || season < 0 {
		return ensembleGroupKey{}, false
	}
	return ensembleGroupKey{normalizedTitle: rest[:idx], season: season}, true
}

// buildEnsembleSeasonCandidates groups completed loose episodes by normalized
// show title + season and returns one virtual queue entry per group with at
// least ensembleMinEpisodes distinct episodes and no seeded pack for the same
// key. Input torrents have already passed the run's category/tag filters and
// content dedup, so groups inherit the run scoping.
func (s *Service) buildEnsembleSeasonCandidates(torrents []qbt.Torrent) []qbt.Torrent {
	type groupInfo struct {
		displayTitle string
		episodes     map[int]struct{}
	}
	groups := make(map[ensembleGroupKey]*groupInfo)
	packs := make(map[ensembleGroupKey]struct{})

	for i := range torrents {
		torrent := &torrents[i]
		if torrent.Progress < 1.0 {
			continue
		}
		release := s.releaseCache.Parse(torrent.Name)
		key := ensembleGroupKey{
			normalizedTitle: s.stringNormalizer.Normalize(release.Title),
			season:          release.Series,
		}
		if key.normalizedTitle == "" {
			continue
		}
		switch {
		case isTVSeasonPack(release):
			packs[key] = struct{}{}
		case isTVEpisode(release):
			group := groups[key]
			if group == nil {
				group = &groupInfo{episodes: make(map[int]struct{})}
				groups[key] = group
			}
			if group.displayTitle == "" {
				group.displayTitle = release.Title
			}
			group.episodes[release.Episode] = struct{}{}
		}
	}

	virtual := make([]qbt.Torrent, 0, len(groups))
	for key, group := range groups {
		if len(group.episodes) < ensembleMinEpisodes {
			continue
		}
		if _, packSeeded := packs[key]; packSeeded {
			continue
		}
		name := group.displayTitle
		if key.season > 0 {
			name = fmt.Sprintf("%s S%02d", group.displayTitle, key.season)
		}
		virtual = append(virtual, qbt.Torrent{
			Hash:     ensembleSeasonPseudoHash(key),
			Name:     name,
			Progress: 1.0,
		})
	}

	slices.SortFunc(virtual, func(a, b qbt.Torrent) int {
		return strings.Compare(a.Hash, b.Hash)
	})
	return virtual
}

// processEnsembleSeasonCandidate searches trackers for a season pack matching
// a virtual episode-group entry and feeds admitted candidates through the
// regular apply path, where the season-pack diversion assembles them from
// local episodes. Admission is name-level only (show + season + is-pack): the
// virtual entry has no meaningful size, and the assembly pipeline's per-file
// coverage check is the real gate.
func (s *Service) processEnsembleSeasonCandidate(ctx context.Context, state *searchRunState, torrent *qbt.Torrent, processedAt time.Time) (bool, error) {
	key, ok := parseEnsembleSeasonPseudoHash(torrent.Hash)
	if !ok {
		// Unreachable: every queued pseudo-hash is minted by ensembleSeasonPseudoHash.
		log.Debug().Str("hash", torrent.Hash).Msg("[CROSSSEED-ENSEMBLE] Unparseable ensemble candidate")
		return false, nil
	}

	if state.opts.DisableTorznab || state.resolvedTorznabIndexerErr != nil || len(state.resolvedTorznabIndexerIDs) == 0 {
		s.recordEnsembleOutcome(state, torrent, processedAt, models.CrossSeedSearchResultStatusSkipped, "no eligible indexers")
		return false, nil
	}

	release := s.releaseCache.Parse(torrent.Name)
	searchQuery := buildSafeSearchQuery(torrent.Name, release, release.Title)
	contentInfo := DetermineContentType(release)

	searchCtx, searchCancel, searchTimeout := automationTorrentSearchContext(ctx, false)
	if searchCancel != nil {
		defer searchCancel()
	}

	searchReq := &jackett.TorznabSearchRequest{
		Query:            searchQuery.Query,
		ReleaseName:      torrent.Name,
		Limit:            effectiveTorznabCrossSeedSearchLimit(0),
		IndexerIDs:       state.resolvedTorznabIndexerIDs,
		ReturnAllResults: true,
	}
	if len(contentInfo.Categories) > 0 {
		searchReq.Categories = contentInfo.Categories
	}
	if searchQuery.Season != nil {
		searchReq.Season = searchQuery.Season
	}

	resp, err := s.searchOnce(searchCtx, searchReq)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			s.recordEnsembleOutcome(state, torrent, processedAt, models.CrossSeedSearchResultStatusSkipped, fmt.Sprintf("search timed out after %s", searchTimeout))
			return true, nil
		}
		s.recordEnsembleOutcome(state, torrent, processedAt, models.CrossSeedSearchResultStatusFailed, fmt.Sprintf("search failed: %v", err))
		return false, err
	}

	if s.automationStore != nil {
		if histErr := s.automationStore.UpsertSearchHistory(ctx, state.opts.InstanceID, torrent.Hash, processedAt); histErr != nil {
			log.Debug().Err(histErr).Msg("failed to update ensemble search history")
		}
	}

	s.applyEnsembleSearchResults(ctx, state, torrent, key, searchQuery.Query, resp, processedAt)
	return true, nil
}

// applyEnsembleSearchResults admits season-pack results for an ensemble group
// and attempts each admitted candidate through the regular apply path until
// one is added or the attempt cap is reached.
func (s *Service) applyEnsembleSearchResults(ctx context.Context, state *searchRunState, torrent *qbt.Torrent, key ensembleGroupKey, query string, resp *jackett.SearchResponse, processedAt time.Time) {
	admitted := 0
	successCount := 0
	failedAttempt := false
	indexerAdds := make(map[int]int)
	indexerFails := make(map[int]int)

	for _, res := range resp.Results {
		candidate := s.releaseCache.Parse(res.Title)
		if !isTVSeasonPack(candidate) {
			continue
		}
		if s.stringNormalizer.Normalize(candidate.Title) != key.normalizedTitle {
			continue
		}
		if key.season > 0 && candidate.Series != key.season {
			continue
		}
		hashes := torrentSearchResultInfoHashes(res.InfoHashV1, res.InfoHashV2)
		if len(hashes) > 0 && s.syncManager != nil {
			if _, exists, hashErr := s.syncManager.HasTorrentByAnyHash(ctx, state.opts.InstanceID, hashes); hashErr == nil && exists {
				continue
			}
		}

		admitted++
		if admitted > ensembleMaxApplyAttempts {
			break
		}

		match := torrentSearchResultFromJackett(res, "season pack from seeded episodes", 0, searchCandidateClassStrict, "", nil, nil)
		attemptResult, attemptErr := s.executeCrossSeedSearchAttempt(ctx, state, torrent, match, processedAt)
		if attemptResult != nil {
			switch attemptResult.Status {
			case models.CrossSeedSearchResultStatusAdded:
				s.searchMu.Lock()
				state.run.TorrentsAdded++
				s.searchMu.Unlock()
				successCount++
				indexerAdds[match.IndexerID]++
			case models.CrossSeedSearchResultStatusFailed:
				failedAttempt = true
				indexerFails[match.IndexerID]++
			case models.CrossSeedSearchResultStatusSkipped:
				// harmless no-add (e.g. already exists); falls through to the
				// skipped outcome below
			}
			s.appendSearchResult(state, *attemptResult)
		}
		if attemptErr != nil {
			failedAttempt = true
			indexerFails[match.IndexerID]++
		}
		if successCount > 0 {
			break
		}
	}

	s.reportIndexerOutcomes(resp.JobID, indexerAdds, indexerFails)

	log.Debug().
		Str("group", torrent.Hash).
		Str("query", query).
		Int("results", len(resp.Results)).
		Int("admitted", admitted).
		Int("added", successCount).
		Msg("[CROSSSEED-ENSEMBLE] Season pack search completed")

	switch {
	case successCount > 0:
		s.persistSearchRun(state)
	case admitted == 0:
		s.recordEnsembleOutcome(state, torrent, processedAt, models.CrossSeedSearchResultStatusSkipped, "no season pack candidates")
	case failedAttempt:
		s.searchMu.Lock()
		state.run.TorrentsFailed++
		s.searchMu.Unlock()
		s.persistSearchRun(state)
	default:
		s.searchMu.Lock()
		state.run.TorrentsSkipped++
		s.searchMu.Unlock()
		s.persistSearchRun(state)
	}
}

// recordEnsembleOutcome appends a summary result row for an ensemble candidate
// that produced no per-attempt rows, bumping the matching run counter.
func (s *Service) recordEnsembleOutcome(state *searchRunState, torrent *qbt.Torrent, processedAt time.Time, status models.CrossSeedSearchResultStatus, message string) {
	s.searchMu.Lock()
	if status == models.CrossSeedSearchResultStatusFailed {
		state.run.TorrentsFailed++
	} else {
		state.run.TorrentsSkipped++
	}
	s.searchMu.Unlock()
	s.appendSearchResult(state, models.CrossSeedSearchResult{
		TorrentHash: torrent.Hash,
		TorrentName: torrent.Name,
		Status:      status,
		Message:     message,
		ProcessedAt: processedAt,
	})
	s.persistSearchRun(state)
}
