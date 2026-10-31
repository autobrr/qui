// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

// sourceApplyEntry is one attempt to add a search result for a source torrent.
type sourceApplyEntry struct {
	result    models.CrossSeedSearchResult
	indexerID int
	// err is set when the download or the add returned an error.
	err error
}

// applySourceSearchResults applies the search results for one source torrent.
// It tries the normal matches first, then at most
// maxTitleRescueAttemptsPerSearch title rescue matches. Title rescue skips an
// indexer that gave a normal add, and an info hash that is already in the
// client or already added in this search. It returns one entry for each
// attempt and reports the indexer outcomes to Search history under
// resp.JobID. When ctx is canceled, it returns the entries for the attempts
// that it made, together with the error.
//
// The Library Scan and the completion search use it. Two paths do not:
//   - The season ensemble accepts only season packs with the same title and
//     season, so no title rescue result reaches it. It stops at the first add
//     on purpose: the pack is then a real torrent, and the next Library Scan
//     cross-seeds it through the normal search.
//   - The interactive apply in the search dialog applies the results that the
//     user picked, so the indexer rules of title rescue do not apply. Its
//     rescue limit counts over all Apply clicks of one search.
func (s *Service) applySourceSearchResults(ctx context.Context, instanceID int, torrent *qbt.Torrent, resp *TorrentSearchResponse, defaults triggerDefaults, processedAt time.Time) ([]sourceApplyEntry, error) {
	entries := make([]sourceApplyEntry, 0, len(resp.Results))
	attempt := func(match TorrentSearchResult) models.CrossSeedSearchResultStatus {
		result, err := s.executeCrossSeedSearchAttempt(ctx, instanceID, defaults, torrent, match, processedAt)
		entries = append(entries, sourceApplyEntry{result: *result, indexerID: match.IndexerID, err: err})
		return result.Status
	}

	normalMatches, rescueMatches := splitTitleRescueResults(resp.Results)
	normalAddedIndexers := make(map[int]struct{})
	normalAddedHashes := make(map[string]struct{})
	var err error
	for _, match := range normalMatches {
		if err = ctx.Err(); err != nil {
			break
		}
		if attempt(match) == models.CrossSeedSearchResultStatusAdded {
			normalAddedIndexers[match.IndexerID] = struct{}{}
			addTorrentSearchResultHashes(normalAddedHashes, match)
		}
	}
	if err == nil {
		err = s.attemptTitleRescueResults(ctx, instanceID, rescueMatches, normalAddedIndexers, normalAddedHashes, func(match TorrentSearchResult) {
			attempt(match)
		})
	}

	indexerAdds := make(map[int]int)
	indexerFails := make(map[int]int)
	for _, entry := range entries {
		switch {
		case entry.result.Status == models.CrossSeedSearchResultStatusAdded:
			indexerAdds[entry.indexerID]++
		case entry.result.Status == models.CrossSeedSearchResultStatusFailed || entry.err != nil:
			indexerFails[entry.indexerID]++
		}
	}
	s.reportIndexerOutcomes(resp.JobID, indexerAdds, indexerFails)

	return entries, err
}

// sourceApplyCounts sums up the entries of one source torrent.
type sourceApplyCounts struct {
	added, failed, skipped int
	// firstErr is the first attempt error, else the first failed entry. It is
	// nil when no attempt failed.
	firstErr error
	// errorSamples holds up to three distinct failure messages.
	errorSamples []string
}

func countSourceApplyEntries(entries []sourceApplyEntry) sourceApplyCounts {
	var counts sourceApplyCounts
	var firstFailure error
	addSample := func(message string) {
		if message != "" && len(counts.errorSamples) < 3 && !slices.Contains(counts.errorSamples, message) {
			counts.errorSamples = append(counts.errorSamples, message)
		}
	}
	for _, entry := range entries {
		switch entry.result.Status {
		case models.CrossSeedSearchResultStatusAdded:
			counts.added++
		case models.CrossSeedSearchResultStatusFailed:
			counts.failed++
			message := strings.TrimSpace(entry.result.Message)
			addSample(message)
			if firstFailure == nil {
				if message == "" {
					message = "classified apply failure"
				}
				firstFailure = fmt.Errorf("%s %q: %s", entry.result.IndexerName, entry.result.ReleaseTitle, message)
			}
		case models.CrossSeedSearchResultStatusSkipped:
			counts.skipped++
		}
		if entry.err != nil {
			addSample(strings.TrimSpace(entry.err.Error()))
			if counts.firstErr == nil {
				counts.firstErr = fmt.Errorf("%s: %w", entry.result.IndexerName, entry.err)
			}
		}
	}
	if counts.firstErr == nil {
		counts.firstErr = firstFailure
	}
	return counts
}

func splitTitleRescueResults(results []TorrentSearchResult) (normal, rescue []TorrentSearchResult) {
	for _, result := range results {
		if result.SearchDecision.Class == searchCandidateClassTitleRescue {
			rescue = append(rescue, result)
		} else {
			normal = append(normal, result)
		}
	}
	return normal, rescue
}

func selectTitleRescueAttempts(results []TorrentSearchResult, blockedIndexers map[int]struct{}, limit int) []TorrentSearchResult {
	if limit <= 0 {
		return nil
	}
	selected := make([]TorrentSearchResult, 0, min(limit, len(results)))
	selectedResults := make(map[int]struct{}, limit)
	seenIndexers := make(map[int]struct{}, len(results))
	for index, result := range results {
		if _, blocked := blockedIndexers[result.IndexerID]; blocked {
			continue
		}
		if _, seen := seenIndexers[result.IndexerID]; seen {
			continue
		}
		seenIndexers[result.IndexerID] = struct{}{}
		selected = append(selected, result)
		selectedResults[index] = struct{}{}
		if len(selected) == limit {
			return selected
		}
	}
	for index, result := range results {
		if _, blocked := blockedIndexers[result.IndexerID]; blocked {
			continue
		}
		if _, used := selectedResults[index]; used {
			continue
		}
		selected = append(selected, result)
		if len(selected) == limit {
			break
		}
	}
	return selected
}

func (s *Service) attemptTitleRescueResults(ctx context.Context, instanceID int, results []TorrentSearchResult, blockedIndexers map[int]struct{}, knownHashes map[string]struct{}, attempt func(TorrentSearchResult)) error {
	eligible := make([]TorrentSearchResult, 0, len(results))
	seenHashes := make(map[string]struct{}, len(knownHashes))
	maps.Copy(seenHashes, knownHashes)
	for _, result := range results {
		if _, blocked := blockedIndexers[result.IndexerID]; blocked {
			continue
		}
		hashes := torrentSearchResultInfoHashes(result.InfoHashV1, result.InfoHashV2)
		duplicate := false
		for _, hash := range hashes {
			if _, seen := seenHashes[hash]; seen {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		for _, hash := range hashes {
			seenHashes[hash] = struct{}{}
		}
		exists, err := s.titleRescueResultExists(ctx, instanceID, result)
		if err != nil {
			return err
		}
		if !exists {
			eligible = append(eligible, result)
		}
	}

	attempts := 0
	for _, result := range selectTitleRescueAttempts(eligible, nil, len(eligible)) {
		if err := ctx.Err(); err != nil {
			return err
		}
		attempt(result)
		attempts++
		if attempts == maxTitleRescueAttemptsPerSearch {
			break
		}
	}
	return nil
}

func addTorrentSearchResultHashes(hashes map[string]struct{}, result TorrentSearchResult) {
	for _, hash := range torrentSearchResultInfoHashes(result.InfoHashV1, result.InfoHashV2) {
		hashes[hash] = struct{}{}
	}
}

func (s *Service) titleRescueResultExists(ctx context.Context, instanceID int, result TorrentSearchResult) (bool, error) {
	hashes := torrentSearchResultInfoHashes(result.InfoHashV1, result.InfoHashV2)
	if len(hashes) == 0 || s.syncManager == nil {
		return false, nil
	}
	_, exists, err := s.syncManager.HasTorrentByAnyHash(ctx, instanceID, hashes)
	if err == nil {
		return exists, nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false, err
	}
	log.Debug().
		Err(err).
		Int("instanceID", instanceID).
		Int("indexerID", result.IndexerID).
		Str("indexer", result.Indexer).
		Str("title", result.Title).
		Strs("infoHashes", hashes).
		Msg("[CROSSSEED-SEARCH] Failed title rescue duplicate check; keeping result")
	return false, nil
}
