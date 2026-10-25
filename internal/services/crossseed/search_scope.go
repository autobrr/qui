// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
)

// Skip reasons of a Search scope that asks no Torznab indexer.
const (
	noEligibleIndexersSkipReason        = "no eligible indexers"
	noIndexersAfterFilteringSkipReason  = "no eligible indexers after filtering"
	indexersInCooldownSkipReason        = "all eligible indexers within cooldown"
	noIndexerCapsSkipReason             = "no indexers support required caps"
	selectedIndexerContentSkipReason    = "selected indexers were filtered out"
	selectedIndexerCapabilitySkipReason = "selected indexers do not support required caps"
)

// searchScope is the Search scope of one torrent: the Torznab indexers and the
// RED/OPS sites that one search asks.
type searchScope struct {
	torznabIndexerIDs []int
	gazelleHosts      []string
	// skipReason says why the scope asks no Torznab indexer.
	skipReason string
	// gazelleCooldownWithoutLookup reports that the search starts the RED/OPS
	// cooldown although it asks no RED/OPS site.
	gazelleCooldownWithoutLookup bool
	// err reports that the search cannot run.
	err error
}

// searchScopeFacts holds what decideSearchScope reads. The Trigger sets the
// request fields; collectSearchScope fills the facts about the torrent.
type searchScopeFacts struct {
	// requestedIndexerIDs are the Torznab indexers after the setup expanded an
	// empty selection to all enabled indexers.
	requestedIndexerIDs []int
	// picked reports that the user picked the indexers.
	picked             bool
	torznabOff         bool
	cooldownIndexerIDs []int
	// skipGazelle reports that the RED/OPS cooldown of the torrent still runs.
	skipGazelle bool
	// gazelleKeys are the RED/OPS hosts that have an API key.
	gazelleKeys []string

	contentFilter contentFilterFacts
	// sourceSite is the RED/OPS host that the torrent comes from, or "".
	sourceSite       string
	musicCheckPassed bool
}

// contentFilterFacts is the result of the content analysis of one torrent.
type contentFilterFacts struct {
	err error
	// done reports that the content filter finished. When the wait for it timed
	// out, indexerIDs holds the result of the capability filter.
	done       bool
	indexerIDs []int
	// seededOn counts the indexers that the filter removed because the content
	// seeds there already.
	seededOn          int
	noCapableIndexers bool
}

// decideSearchScope applies the rules of the Library Scan to the facts of one
// torrent. It reads no store and sends no request.
func decideSearchScope(f searchScopeFacts) searchScope {
	ids, skipReason, err := decideTorznabIndexers(f)
	if err != nil {
		return searchScope{err: err}
	}
	scope := searchScope{torznabIndexerIDs: ids, skipReason: skipReason}
	if f.skipGazelle {
		return scope
	}

	var hosts []string
	for _, host := range gazelleTargetHosts(f.sourceSite) {
		if slices.Contains(f.gazelleKeys, host) {
			hosts = append(hosts, host)
		}
	}
	switch {
	case len(hosts) == 0 && f.torznabOff:
		return searchScope{err: fmt.Errorf("%w: torznab disabled but gazelle not configured", ErrInvalidRequest)}
	case len(hosts) == 0:
		scope.gazelleCooldownWithoutLookup = len(f.gazelleKeys) > 0
	case f.sourceSite == "" && !f.musicCheckPassed:
		// A torrent from RED/OPS skips the music check, because the tracker
		// proves that RED/OPS can host the content.
		scope.gazelleCooldownWithoutLookup = true
	default:
		scope.gazelleHosts = hosts
	}
	return scope
}

func decideTorznabIndexers(f searchScopeFacts) ([]int, string, error) {
	if f.torznabOff {
		return nil, noEligibleIndexersSkipReason, nil
	}
	if len(f.requestedIndexerIDs) == 0 {
		// Picked indexers that the setup removed must not expand to every
		// enabled indexer.
		if f.picked {
			return nil, noIndexersAfterFilteringSkipReason, nil
		}
		return nil, noEligibleIndexersSkipReason, nil
	}

	cf := f.contentFilter
	if cf.err != nil {
		return nil, "", fmt.Errorf("analyze torrent: %w", cf.err)
	}
	allowed := intersectInts(cf.indexerIDs, f.requestedIndexerIDs)
	if len(allowed) == 0 {
		switch {
		case len(cf.indexerIDs) > 0 && cf.done:
			return nil, selectedIndexerContentSkipReason, nil
		case len(cf.indexerIDs) > 0:
			return nil, selectedIndexerCapabilitySkipReason, nil
		case cf.seededOn > 0:
			return nil, fmt.Sprintf("skipped: already seeded from %d tracker(s)", cf.seededOn), nil
		case !cf.done || cf.noCapableIndexers:
			return nil, noIndexerCapsSkipReason, nil
		default:
			return nil, noIndexersAfterFilteringSkipReason, nil
		}
	}
	allowed = subtractInts(allowed, f.cooldownIndexerIDs)
	if len(allowed) == 0 {
		return nil, indexersInCooldownSkipReason, nil
	}
	return allowed, "", nil
}

func gazelleTargetHosts(sourceSite string) []string {
	if target := gazelleTargetForSource(sourceSite); target != "" {
		return []string{target}
	}
	return []string{"redacted.sh", "orpheus.network"}
}

// collectSearchScope collects the facts about one torrent and decides its
// Search scope. f holds the request fields of the Trigger. When
// resolveIndexers is set, the collector expands f.requestedIndexerIDs; the
// Library Scan resolves them one time for each run instead.
func (s *Service) collectSearchScope(ctx context.Context, instanceID int, torrent *qbt.Torrent, f searchScopeFacts, resolveIndexers bool) searchScope {
	if resolveIndexers && !f.torznabOff {
		ids, err := s.resolveTorznabIndexerIDs(ctx, f.requestedIndexerIDs, true)
		if err != nil {
			return searchScope{err: fmt.Errorf("resolve indexers: %w", err)}
		}
		f.requestedIndexerIDs = ids
	}

	if !f.torznabOff && len(f.requestedIndexerIDs) > 0 {
		f.contentFilter = s.contentFilterFacts(ctx, instanceID, torrent.Hash)
	}

	f.sourceSite, _ = s.detectGazelleSourceSite(torrent)
	if f.sourceSite == "" && !f.skipGazelle && len(f.gazelleKeys) > 0 {
		files, err := s.getTorrentFilesCached(ctx, instanceID, torrent.Hash)
		if err != nil {
			return searchScope{err: fmt.Errorf("failed to get torrent files: %w", err)}
		}
		f.musicCheckPassed = gazellePlausibleContent(files)
	}

	scope := decideSearchScope(f)
	log.Debug().
		Int("instanceID", instanceID).
		Str("torrentHash", torrent.Hash).
		Ints("requestedIndexers", f.requestedIndexerIDs).
		Ints("torznabIndexers", scope.torznabIndexerIDs).
		Strs("gazelleHosts", scope.gazelleHosts).
		Str("skipReason", scope.skipReason).
		Msg("[CROSSSEED-SEARCH] Search scope")
	return scope
}

// contentFilterFacts runs the content analysis of one torrent and waits for
// the content filter.
func (s *Service) contentFilterFacts(ctx context.Context, instanceID int, hash string) contentFilterFacts {
	analysis, err := s.AnalyzeTorrentForSearchAsync(ctx, instanceID, hash, true)
	if err != nil {
		return contentFilterFacts{err: err}
	}
	s.waitForContentFilteringCompletion(ctx, analysis.FilteringState)
	snapshot := analysis.FilteringState.Clone()
	facts := contentFilterFacts{
		done:              snapshot.ContentCompleted,
		indexerIDs:        snapshot.CapabilityIndexers,
		seededOn:          len(snapshot.ExcludedIndexers),
		noCapableIndexers: len(snapshot.CapabilityIndexers) == 0,
	}
	if snapshot.ContentCompleted {
		facts.indexerIDs = snapshot.FilteredIndexers
	}
	return facts
}
