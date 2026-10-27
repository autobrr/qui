// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

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
	noGazelleKeySkipReason              = "no RED/OPS API key for the target site"
)

// The RED/OPS sites, in the order that a search asks them.
const (
	redHost = "redacted.sh"
	opsHost = "orpheus.network"
)

var gazelleSites = []string{redHost, opsHost}

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
	// torznabErr reports that qui could not list the Torznab indexers, so the
	// search asks only RED/OPS.
	torznabErr error
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
	// gazelleTorznabIndexers maps each Torznab indexer of RED or OPS to its
	// host.
	gazelleTorznabIndexers map[int]string

	contentFilter contentFilterFacts
	// sourceSite is the RED/OPS host that the torrent comes from, or "".
	sourceSite string
	// duplicateSourceSites are the RED/OPS hosts of the other copies in the
	// duplicate group of the torrent. The Library Scan sets them.
	duplicateSourceSites []string
	musicCheckPassed     bool
}

// contentFilterFacts is the result of the content analysis of one torrent.
type contentFilterFacts struct {
	err error
	// torznabErr reports that qui could not list the Torznab indexers, for
	// example because Prowlarr or Jackett is down.
	torznabErr error
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
	if listErr := f.contentFilter.torznabErr; listErr != nil {
		torznabErr := fmt.Errorf("load torznab indexers: %w", listErr)
		if len(f.gazelleKeys) == 0 {
			return searchScope{err: torznabErr}
		}
		// The error is the skip reason, so that an outage does not look like a
		// content filter that removed every indexer.
		scope = searchScope{skipReason: torznabErr.Error(), torznabErr: torznabErr}
	}
	if f.skipGazelle {
		return scope
	}

	// The scope asks only the RED/OPS sites where no copy of the group seeds.
	var hosts []string
	missingKey := false
	for _, host := range gazelleSites {
		if host == f.sourceSite || slices.Contains(f.duplicateSourceSites, host) {
			continue
		}
		if slices.Contains(f.gazelleKeys, host) {
			hosts = append(hosts, host)
		} else {
			missingKey = true
		}
	}
	// A copy from RED/OPS skips the music check, because the tracker proves
	// that RED/OPS can host the content.
	fromGazelle := f.sourceSite != "" || len(f.duplicateSourceSites) > 0
	switch {
	case len(hosts) > 0 && !fromGazelle && !f.musicCheckPassed:
		// The files of a torrent do not change, so a failed music check starts
		// the cooldown.
		scope.gazelleCooldownWithoutLookup = true
	case len(hosts) > 0:
		scope.gazelleHosts = hosts
	case !missingKey && len(f.gazelleKeys) > 0:
		// A copy of the group seeds on each RED/OPS site.
		scope.gazelleCooldownWithoutLookup = true
	case f.torznabOff && len(f.gazelleKeys) == 0:
		return searchScope{err: fmt.Errorf("%w: torznab disabled but gazelle not configured", ErrInvalidRequest)}
	case f.torznabOff:
		scope.skipReason = noGazelleKeySkipReason
	default:
		// A missing API key starts no cooldown, so a key that the user adds
		// later works at the next run.
	}
	return scope
}

func decideTorznabIndexers(f searchScopeFacts) ([]int, string, error) {
	if f.torznabOff {
		return nil, noEligibleIndexersSkipReason, nil
	}
	requested := withoutGazelleTorznabIndexers(f.requestedIndexerIDs, f.gazelleTorznabIndexers, f.gazelleKeys)
	if len(requested) == 0 {
		// An empty list of picked indexers is not an empty selection, which
		// means every enabled indexer.
		if f.picked {
			return nil, noIndexersAfterFilteringSkipReason, nil
		}
		return nil, noEligibleIndexersSkipReason, nil
	}

	cf := f.contentFilter
	if cf.err != nil {
		return nil, "", fmt.Errorf("analyze torrent: %w", cf.err)
	}
	allowed := intersectInts(cf.indexerIDs, requested)
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

// collectSearchScope fills the torrent facts into f and decides the Search scope.
// The Library Scan resolves its indexers one time for each run, so it passes
// resolveIndexers false.
func (s *Service) collectSearchScope(ctx context.Context, instanceID int, torrent *qbt.Torrent, f searchScopeFacts, resolveIndexers bool) searchScope {
	if resolveIndexers && !f.torznabOff {
		ids, err := s.resolveTorznabIndexerIDs(ctx, f.requestedIndexerIDs)
		f.requestedIndexerIDs = ids
		f.contentFilter.torznabErr = err
		if err == nil && len(f.gazelleKeys) > 0 {
			f.gazelleTorznabIndexers = s.gazelleTorznabIndexers(ctx)
		}
	}

	if !f.torznabOff && len(f.requestedIndexerIDs) > 0 {
		f.contentFilter = s.contentFilterFacts(ctx, instanceID, torrent.Hash)
	}

	f.sourceSite, _ = s.detectGazelleSourceSite(torrent)
	if f.sourceSite == "" && len(f.duplicateSourceSites) == 0 && !f.skipGazelle && len(f.gazelleKeys) > 0 {
		files, err := s.getTorrentFilesCached(ctx, instanceID, torrent.Hash)
		if err != nil {
			return searchScope{err: fmt.Errorf("failed to get torrent files: %w", err)}
		}
		f.musicCheckPassed = gazellePlausibleContent(files)
	}

	scope := decideSearchScope(f)
	log.Trace().
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
		torznabErr:        snapshot.torznabErr,
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

// withoutGazelleTorznabIndexers removes the Torznab indexer of each RED/OPS site
// that has an API key. The search asks that site through its API, and asking it
// through Torznab too sends the same search two times.
func withoutGazelleTorznabIndexers(ids []int, gazelleIndexers map[int]string, gazelleKeys []string) []int {
	return slices.DeleteFunc(slices.Clone(ids), func(id int) bool {
		host, ok := gazelleIndexers[id]
		return ok && slices.Contains(gazelleKeys, host)
	})
}

// searchRunIndexerIDs resolves the Torznab indexers of a Library Scan one time
// for each run.
func (s *Service) searchRunIndexerIDs(ctx context.Context, requested []int, gazelleKeys []string) ([]int, error) {
	ids, err := s.resolveTorznabIndexerIDs(ctx, requested)
	if err != nil || len(gazelleKeys) == 0 {
		return ids, err
	}
	return withoutGazelleTorznabIndexers(ids, s.gazelleTorznabIndexers(ctx), gazelleKeys), nil
}

// resolveTorznabIndexerIDs expands an empty selection to all enabled Torznab
// indexers. An error means that qui could not list the indexers.
func (s *Service) resolveTorznabIndexerIDs(ctx context.Context, requested []int) ([]int, error) {
	requested = uniquePositiveInts(requested)
	if len(requested) > 0 || s.jackettService == nil {
		return requested, nil
	}
	info, err := s.jackettService.GetEnabledIndexersInfo(ctx)
	if err != nil {
		return nil, err
	}
	return slices.Sorted(maps.Keys(info)), nil
}

// gazelleTorznabSites finds the Torznab indexers of RED and OPS by the URL or
// the name of the indexer.
var gazelleTorznabSites = []struct {
	host   string
	urlRE  *regexp.Regexp
	nameRE *regexp.Regexp
}{
	{redHost, regexp.MustCompile(`(^|[^a-z0-9])redacted([^a-z0-9]|$)`), regexp.MustCompile(`\bredacted\b`)},
	{opsHost, regexp.MustCompile(`(^|[^a-z0-9])(ops|orpheus)([^a-z0-9]|$)`), regexp.MustCompile(`\b(ops|orpheus)\b`)},
}

// gazelleTorznabIndexers maps each Torznab indexer of RED or OPS to its host.
// When qui cannot list the indexers, it returns nil and the search keeps them.
func (s *Service) gazelleTorznabIndexers(ctx context.Context) map[int]string {
	if s.jackettService == nil {
		return nil
	}
	resp, err := s.jackettService.GetIndexers(ctx)
	if err != nil || resp == nil {
		return nil
	}
	out := make(map[int]string)
	for _, idx := range resp.Indexers {
		id, convErr := strconv.Atoi(strings.TrimSpace(idx.ID))
		if convErr != nil || id <= 0 {
			continue
		}
		rawURL := normalizeLowerTrim(idx.Description)
		name := normalizeLowerTrim(idx.Name)
		for _, site := range gazelleTorznabSites {
			if site.urlRE.MatchString(rawURL) || site.nameRE.MatchString(name) {
				out[id] = site.host
				break
			}
		}
	}
	return out
}
