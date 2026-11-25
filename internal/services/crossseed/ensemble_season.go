// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/releases"
)

const (
	ensembleSeasonHashPrefix = "season:"

	ensembleMinEpisodes = 3

	ensembleMaxApplyAttempts = 3
)

type ensembleGroupKey struct {
	normalizedTitle string
	season          int
}

func isEnsembleSeasonCandidate(hash string) bool {
	return strings.HasPrefix(hash, ensembleSeasonHashPrefix)
}

var seasonRangeRe = regexp.MustCompile(`(?i)\bS(?:eason[ ._-]?)?(\d{1,2})[ ._-]?-[ ._-]?S?(\d{1,2})\b`)

func isSeasonRangePack(name string) bool {
	m := seasonRangeRe.FindStringSubmatch(name)
	if m == nil {
		return false
	}
	start, _ := strconv.Atoi(m[1])
	end, _ := strconv.Atoi(m[2])
	return end > start
}

func ensembleSeasonPseudoHash(key ensembleGroupKey) string {
	return fmt.Sprintf("%s%s:s%02d", ensembleSeasonHashPrefix, key.normalizedTitle, key.season)
}

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

func (s *Service) buildEnsembleSeasonCandidates(torrents, packSource []*qbt.Torrent, copyAddedOn map[string]int64) []*qbt.Torrent {
	type groupInfo struct {
		displayTitle string
		episodes     map[int]struct{}
		addedOn      int64
	}
	groups := make(map[ensembleGroupKey]*groupInfo)
	packs := make(map[ensembleGroupKey]struct{})

	for _, pack := range packSource {
		release := s.releaseCache.Parse(pack.Name)
		if !isTVSeasonPack(release) {
			continue
		}
		if releases.IsEpisodeRange(release) {
			continue
		}
		key := ensembleGroupKey{
			normalizedTitle: s.stringNormalizer.Normalize(release.Title),
			season:          release.Series,
		}
		if key.normalizedTitle != "" {
			packs[key] = struct{}{}
		}
	}

	for _, torrent := range torrents {
		if s.sourceGate().check(torrent, sourceFilter{}) != sourcePass {
			continue
		}
		release := s.releaseCache.Parse(torrent.Name)
		isRange := releases.IsEpisodeRange(release)
		if !isTVEpisode(release) && !isRange {
			continue
		}
		key := ensembleGroupKey{
			normalizedTitle: s.stringNormalizer.Normalize(release.Title),
			season:          release.Series,
		}
		if key.normalizedTitle == "" {
			continue
		}
		group := groups[key]
		if group == nil {
			group = &groupInfo{episodes: make(map[int]struct{})}
			groups[key] = group
		}
		if group.displayTitle == "" {
			group.displayTitle = release.Title
		}
		group.addedOn = max(group.addedOn, torrent.AddedOn, copyAddedOn[torrent.Hash])
		if isRange {
			for _, se := range release.SeriesEpisodes() {
				if se[0] == release.Series {
					group.episodes[se[1]] = struct{}{}
				}
			}
		} else {
			group.episodes[release.Episode] = struct{}{}
		}
	}

	virtual := make([]*qbt.Torrent, 0, len(groups))
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
		virtual = append(virtual, &qbt.Torrent{
			Hash:     ensembleSeasonPseudoHash(key),
			Name:     name,
			Progress: 1.0,
			AddedOn:  group.addedOn,
		})
	}

	slices.SortFunc(virtual, func(a, b *qbt.Torrent) int {
		return strings.Compare(a.Hash, b.Hash)
	})
	return virtual
}

func (s *Service) processEnsembleSeasonCandidate(ctx context.Context, state *searchRunState, torrent *qbt.Torrent, processedAt time.Time) (bool, error) {
	key, ok := parseEnsembleSeasonPseudoHash(torrent.Hash)
	if !ok {
		log.Debug().Str("hash", torrent.Hash).Msg("[CROSSSEED-ENSEMBLE] Unparseable ensemble candidate")
		return false, nil
	}

	if state.opts.DisableTorznab || len(state.resolvedTorznabIndexerIDs) == 0 {
		s.recordEnsembleOutcome(state, torrent, processedAt, models.CrossSeedSearchResultStatusSkipped, noEligibleIndexersSkipReason)
		return false, nil
	}

	source := s.matcher().readName(torrent.Name)
	gatherIn := s.matcher().buildTorznabRequest(torznabRequestInput{
		torrentName: torrent.Name,
		source:      source,
		contentInfo: DetermineContentType(source.release),
		indexerIDs:  state.resolvedTorznabIndexerIDs,
	})
	gatherIn.AltTitle = ""
	gatherIn.AltTitleSeason = nil
	gatherIn.LogPrefix = "[CROSSSEED-ENSEMBLE]"
	searchReq := gatherIn.Req
	if len(searchReq.Categories) > 0 {
		searchReq.Categories = s.jackettService.GetOptimalCategoriesForIndexers(ctx, searchReq.Categories, searchReq.IndexerIDs)
	}

	searchCtx := jackett.WithSearchPriority(ctx, jackett.RateLimitPriorityBackground)

	usable := func(res jackett.SearchResult) bool { return s.ensemblePackAdmissible(res.Title, key) }
	resp, _, answered, err := s.newSearchGatherer(usable).Gather(ctx, searchCtx, gatherIn)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if errors.Is(err, errSearchTimedOut) {
			s.recordEnsembleOutcome(state, torrent, processedAt, models.CrossSeedSearchResultStatusSkipped, "search timed out")
			return true, nil
		}
		s.recordEnsembleOutcome(state, torrent, processedAt, models.CrossSeedSearchResultStatusFailed, fmt.Sprintf("search failed: %v", err))
		return true, err
	}
	if answered {
		state.torznabSearched = true
	}

	if s.automationStore != nil {
		if histErr := s.automationStore.UpsertSearchHistory(ctx, state.opts.InstanceID, torrent.Hash, processedAt); histErr != nil {
			log.Debug().Err(histErr).Msg("failed to update ensemble search history")
		}
	}

	s.applyEnsembleSearchResults(ctx, state, torrent, key, searchReq.Query, resp, processedAt)
	return true, nil
}

func (s *Service) applyEnsembleSearchResults(ctx context.Context, state *searchRunState, torrent *qbt.Torrent, key ensembleGroupKey, query string, resp *jackett.SearchResponse, processedAt time.Time) {
	admitted := 0
	successCount := 0
	failedAttempt := false
	indexerAdds := make(map[int]int)
	indexerFails := make(map[int]int)
	attemptedNames := make(map[string]struct{})

	for _, res := range resp.Results {
		if !s.ensemblePackAdmissible(res.Title, key) {
			continue
		}
		normName := canonicalReleaseNameKey(res.Title)
		if _, dup := attemptedNames[normName]; dup {
			continue
		}
		attemptedNames[normName] = struct{}{}
		hashes := torrentSearchResultInfoHashes(res.InfoHashV1, res.InfoHashV2)
		if len(hashes) > 0 && s.syncManager != nil {
			if _, exists, hashErr := s.syncManager.HasTorrentByAnyHash(ctx, state.opts.InstanceID, hashes); hashErr == nil && exists {
				continue
			}
		}
		if s.seasonPackFailCooldownActive(ctx, res.Title, time.Duration(state.opts.CooldownMinutes)*time.Minute) {
			continue
		}

		admitted++
		if admitted > ensembleMaxApplyAttempts {
			break
		}

		match := torrentSearchResultFromJackett(res, "season pack from seeded episodes", 0, searchDecisionProvenance{Class: searchCandidateClassStrict})
		attemptResult, attemptErr := s.executeCrossSeedSearchAttempt(ctx, state.opts.InstanceID, state.opts.triggerDefaults, torrent, match, processedAt)
		if attemptResult != nil {
			switch attemptResult.Status {
			case models.CrossSeedSearchResultStatusAdded:
				s.searchMu.Lock()
				state.run.CrossSeedsAdded++
				s.searchMu.Unlock()
				successCount++
				indexerAdds[match.IndexerID]++
			case models.CrossSeedSearchResultStatusFailed:
				failedAttempt = true
				indexerFails[match.IndexerID]++
			case models.CrossSeedSearchResultStatusSkipped:
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

	if admitted == 0 {
		s.recordEnsembleOutcome(state, torrent, processedAt, models.CrossSeedSearchResultStatusSkipped, "no season pack candidates")
		return
	}
	s.finishSearchCandidate(state, successCount, failedAttempt)
}

func (s *Service) ensemblePackAdmissible(title string, key ensembleGroupKey) bool {
	candidate := s.releaseCache.Parse(title)
	return isTVSeasonPack(candidate) &&
		!isSeasonRangePack(title) &&
		s.stringNormalizer.Normalize(candidate.Title) == key.normalizedTitle &&
		(key.season == 0 || candidate.Series == key.season)
}

func (s *Service) recordEnsembleOutcome(state *searchRunState, torrent *qbt.Torrent, processedAt time.Time, status models.CrossSeedSearchResultStatus, message string) {
	s.appendSearchResult(state, models.CrossSeedSearchResult{
		TorrentHash: torrent.Hash,
		TorrentName: torrent.Name,
		Status:      status,
		Message:     message,
		ProcessedAt: processedAt,
	})
	s.finishSearchCandidate(state, 0, status == models.CrossSeedSearchResultStatusFailed)
}
