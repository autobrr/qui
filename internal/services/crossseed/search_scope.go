// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
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

// searchScope is the Search scope of one torrent: the Torznab indexers and the
// RED/OPS sites that one search asks.
type searchScope struct {
	torznabIndexerIDs []int
	// gazelle holds one decision for each RED/OPS site, in the order of the
	// site table: will_ask, or not_asked with its reason.
	gazelle []GazelleLookup
	// exportedTorrent is the torrent file that the local RED/OPS checks read,
	// so the lookup does not export it again.
	exportedTorrent []byte
	// skipReason says why the scope asks no Torznab indexer.
	skipReason string
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
	gazelleOff  bool
	// gazelleKeys are the RED/OPS hosts that have an API key.
	gazelleKeys []string
	// gazelleSelected are the RED/OPS hosts that the user picked. Nil means
	// every host.
	gazelleSelected []string
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
	// gazelleLocalSeeded are the RED/OPS hosts whose content or target hash the
	// instance already holds.
	gazelleLocalSeeded []string
	// reuseLocalChecks lets the search reuse the local RED/OPS checks of the
	// dialog open. Only the dialog search sets it: its answer stamps no
	// cooldown, so a check that is up to a minute old cannot hold back a site.
	reuseLocalChecks bool
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
	scope.gazelle = decideGazelleSites(f)
	if !f.torznabOff || scope.asksGazelle() || f.skipGazelle {
		return scope
	}
	if len(f.gazelleKeys) == 0 {
		return searchScope{err: fmt.Errorf("%w: torznab disabled but gazelle not configured", ErrInvalidRequest)}
	}
	if !gazelleCooldownDue(scope.gazelle) {
		scope.skipReason = noGazelleKeySkipReason
	}
	return scope
}

// decideGazelleSites decides for each RED/OPS site whether the search asks it.
// When more than one rule applies, the first rule in the switch wins.
func decideGazelleSites(f searchScopeFacts) []GazelleLookup {
	// A copy from RED/OPS skips the music check, because the tracker proves
	// that RED/OPS can host the content.
	fromGazelle := f.sourceSite != "" || len(f.duplicateSourceSites) > 0
	sites := gazellemusic.Sites()
	out := make([]GazelleLookup, 0, len(sites))
	for _, site := range sites {
		host := site.Host
		d := GazelleLookup{
			Site:         host,
			Name:         site.DisplayName,
			ShortName:    site.ShortName,
			IndexerID:    site.IndexerID,
			Status:       GazelleWillAsk,
			seededByCopy: host == f.sourceSite || slices.Contains(f.duplicateSourceSites, host),
		}
		hasKey := slices.Contains(f.gazelleKeys, host)
		if hasKey {
			d.ReplacedIndexerIDs = replacedTorznabIndexers(f.gazelleTorznabIndexers, host)
		}
		switch {
		case f.gazelleOff:
			d = d.notAsked(GazelleReasonDisabled)
		case !hasKey:
			d = d.notAsked(GazelleReasonNoAPIKey)
		case f.skipGazelle:
			d = d.notAsked(GazelleReasonCooldown)
		case f.gazelleSelected != nil && !slices.Contains(f.gazelleSelected, host):
			d = d.notAsked(GazelleReasonNotSelected)
		case host == f.sourceSite:
			d = d.notAsked(GazelleReasonSourceSite)
		case slices.Contains(f.duplicateSourceSites, host):
			d = d.notAsked(GazelleReasonAlreadySeeded)
		case !fromGazelle && !f.musicCheckPassed:
			d = d.notAsked(GazelleReasonNotMusic)
		case slices.Contains(f.gazelleLocalSeeded, host):
			d = d.notAsked(GazelleReasonAlreadySeeded)
		}
		out = append(out, d)
	}
	return out
}

func replacedTorznabIndexers(gazelleIndexers map[int]string, host string) []int {
	var ids []int
	for id, indexerHost := range gazelleIndexers {
		if indexerHost == host {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// gazelleHosts returns the RED/OPS hosts that the scope asks.
func (scope searchScope) gazelleHosts() []string {
	var hosts []string
	for _, d := range scope.gazelle {
		if d.Status == GazelleWillAsk {
			hosts = append(hosts, d.Site)
		}
	}
	return hosts
}

func (scope searchScope) asksGazelle() bool {
	return len(scope.gazelleHosts()) > 0
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
	// Picked RED/OPS sites with no picked indexer ask no Torznab indexer.
	expand := !f.picked || len(uniquePositiveInts(f.requestedIndexerIDs)) > 0
	if resolveIndexers && !f.torznabOff && expand {
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

	if err := s.collectGazelleFacts(ctx, instanceID, torrent, &f); err != nil {
		return searchScope{err: err}
	}
	scope := decideSearchScope(f)
	if scope.err == nil && scope.asksGazelle() {
		f.gazelleLocalSeeded, scope.exportedTorrent = s.cachedGazelleLocalFacts(ctx, instanceID, torrent, scope.gazelleHosts(), f.reuseLocalChecks)
		if len(f.gazelleLocalSeeded) > 0 {
			exported := scope.exportedTorrent
			scope = decideSearchScope(f)
			scope.exportedTorrent = exported
		}
	}
	log.Trace().
		Int("instanceID", instanceID).
		Str("torrentHash", torrent.Hash).
		Ints("requestedIndexers", f.requestedIndexerIDs).
		Ints("torznabIndexers", scope.torznabIndexerIDs).
		Strs("gazelleHosts", scope.gazelleHosts()).
		Str("skipReason", scope.skipReason).
		Msg("[CROSSSEED-SEARCH] Search scope")
	return scope
}

// collectGazellePlan decides, for the search dialog, what a search with every
// RED/OPS site picked asks each site.
func (s *Service) collectGazellePlan(ctx context.Context, instanceID int, torrent *qbt.Torrent, clients *gazelleClientSet) ([]GazelleLookup, error) {
	f := searchScopeFacts{gazelleOff: clients.disabled(), gazelleKeys: clients.hosts()}
	if len(f.gazelleKeys) > 0 {
		f.gazelleTorznabIndexers = s.gazelleTorznabIndexers(ctx)
	}
	if err := s.collectGazelleFacts(ctx, instanceID, torrent, &f); err != nil {
		return nil, err
	}
	plan := decideGazelleSites(f)
	if hosts := (searchScope{gazelle: plan}).gazelleHosts(); len(hosts) > 0 {
		if f.gazelleLocalSeeded, _ = s.cachedGazelleLocalFacts(ctx, instanceID, torrent, hosts, false); len(f.gazelleLocalSeeded) > 0 {
			plan = decideGazelleSites(f)
		}
	}
	// The search still asks a site whose key rejected notice is open, because
	// the site can answer again. The plan says that the last answer was a
	// rejection.
	for i, d := range plan {
		if d.Status == GazelleWillAsk && s.keyRejectedOpen(gazelleLookupSite(d)) {
			plan[i].Reason = GazelleReasonKeyRejected
		}
	}
	return plan, nil
}

// collectGazelleFacts fills the RED/OPS facts of the torrent into f: the site
// that it comes from, and the music check.
func (s *Service) collectGazelleFacts(ctx context.Context, instanceID int, torrent *qbt.Torrent, f *searchScopeFacts) error {
	f.sourceSite, _ = s.detectGazelleSourceSite(torrent)
	if f.sourceSite == "" && len(f.duplicateSourceSites) == 0 && !f.skipGazelle && len(f.gazelleKeys) > 0 {
		files, err := s.getTorrentFilesCached(ctx, instanceID, torrent.Hash)
		if err != nil {
			return fmt.Errorf("failed to get torrent files: %w", err)
		}
		f.musicCheckPassed = gazellePlausibleContent(files)
	}
	return nil
}

// gazelleLocalResult is the result of the local RED/OPS checks of one torrent.
type gazelleLocalResult struct {
	checked  []string
	seeded   []string
	exported []byte
}

// cachedGazelleLocalFacts answers as gazelleLocalFacts does, and keeps the
// result for a minute. With reuse, it answers from a result of the last minute
// that checked each of hosts.
func (s *Service) cachedGazelleLocalFacts(ctx context.Context, instanceID int, torrent *qbt.Torrent, hosts []string, reuse bool) ([]string, []byte) {
	key := asyncFilteringCacheKey(instanceID, torrent.Hash)
	if reuse && s.gazelleLocalCache != nil {
		if hit, ok := s.gazelleLocalCache.Get(key); ok && !slices.ContainsFunc(hosts, func(host string) bool { return !slices.Contains(hit.checked, host) }) {
			seeded := slices.DeleteFunc(slices.Clone(hit.seeded), func(host string) bool { return !slices.Contains(hosts, host) })
			return seeded, hit.exported
		}
	}
	seeded, exported := s.gazelleLocalFacts(ctx, instanceID, torrent, hosts)
	if s.gazelleLocalCache != nil {
		s.gazelleLocalCache.Set(key, gazelleLocalResult{checked: slices.Clone(hosts), seeded: seeded, exported: exported}, ttlcache.DefaultTTL)
	}
	return seeded, exported
}

// gazelleLocalFacts returns the hosts whose content or target hash the
// instance already holds, and the exported torrent file. It checks only hosts,
// the sites that pass every other rule, so a torrent that is not music
// exports nothing.
func (s *Service) gazelleLocalFacts(ctx context.Context, instanceID int, torrent *qbt.Torrent, hosts []string) ([]string, []byte) {
	if s.syncManager == nil {
		return nil, nil
	}
	var seeded []string
	instanceTorrents, err := s.syncManager.GetCachedInstanceTorrents(ctx, instanceID)
	if err != nil {
		log.Warn().
			Err(err).
			Int("instanceID", instanceID).
			Msg("[CROSSSEED-GAZELLE] failed to load cached instance torrents for prefilter")
	} else {
		matchedContent, _, _, err := s.findLayoutAwareContentPrefilterMatches(
			ctx,
			instanceID,
			normalizeHash(torrent.Hash),
			torrent,
			s.releaseCache.Parse(torrent.Name),
			instanceTorrents,
		)
		if err != nil {
			log.Warn().Err(err).
				Int("instanceID", instanceID).
				Str("hash", torrent.Hash).
				Msg("[CROSSSEED-GAZELLE] failed to run content prefilter")
		}
		for _, host := range hosts {
			if slices.ContainsFunc(matchedContent, func(match contentPrefilterMatchedTorrent) bool {
				return s.trackerDomainsMatchIndexerDomain(match.trackerDomains, "", host)
			}) {
				log.Debug().
					Str("targetHost", host).
					Str("hash", torrent.Hash).
					Msg("[CROSSSEED-GAZELLE] Target tracker content already present locally, skipping search")
				seeded = append(seeded, host)
			}
		}
	}
	if len(seeded) == len(hosts) {
		return seeded, nil
	}

	exported, _, _, err := s.syncManager.ExportTorrent(ctx, instanceID, torrent.Hash)
	if err != nil || len(exported) == 0 {
		log.Warn().
			Err(err).
			Str("hash", torrent.Hash).
			Msg("[CROSSSEED-GAZELLE] Export torrent failed; falling back to filename matching only")
		return seeded, nil
	}
	for _, host := range hosts {
		site, ok := gazellemusic.SiteByHost(host)
		if !ok || slices.Contains(seeded, host) {
			continue
		}
		hashes, err := site.TargetHashes(exported)
		if err != nil {
			continue
		}
		if _, exists, hashErr := s.syncManager.HasTorrentByAnyHash(ctx, instanceID, hashes); hashErr == nil && exists {
			log.Debug().
				Str("targetHost", host).
				Str("hash", torrent.Hash).
				Strs("targetHashes", hashes).
				Msg("[CROSSSEED-GAZELLE] Target hash already present locally, skipping search")
			seeded = append(seeded, host)
		}
	}
	return seeded, exported
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
		for _, site := range gazellemusic.Sites() {
			if site.IsTorznabIndexer(rawURL, name) {
				out[id] = site.Host
				break
			}
		}
	}
	return out
}

// GazelleSiteFacts describes one RED/OPS site for the web UI.
type GazelleSiteFacts struct {
	// Site is the API host of the site.
	Site      string `json:"site"`
	Name      string `json:"name"`
	ShortName string `json:"shortName"`
	IndexerID int    `json:"indexerId"`
	// Active reports that RED/OPS is on and the site has an API key, so each
	// search asks the site through its API.
	Active bool `json:"active"`
	// ReplacedIndexerIDs are the Torznab indexers of an active site, which
	// every search leaves out.
	ReplacedIndexerIDs []int `json:"replacedIndexerIds"`
}

// GazelleSites returns the facts of each RED/OPS site, in the order of the site
// table.
func (s *Service) GazelleSites(ctx context.Context) ([]GazelleSiteFacts, error) {
	clients, err := s.buildGazelleClientSet(ctx, nil)
	if err != nil {
		return nil, err
	}
	keys := clients.hosts()
	var torznabIndexers map[int]string
	if !clients.disabled() && len(keys) > 0 {
		torznabIndexers = s.gazelleTorznabIndexers(ctx)
	}
	sites := gazellemusic.Sites()
	out := make([]GazelleSiteFacts, 0, len(sites))
	for _, site := range sites {
		active := !clients.disabled() && slices.Contains(keys, site.Host)
		facts := GazelleSiteFacts{
			Site:               site.Host,
			Name:               site.DisplayName,
			ShortName:          site.ShortName,
			IndexerID:          site.IndexerID,
			Active:             active,
			ReplacedIndexerIDs: []int{},
		}
		if active {
			facts.ReplacedIndexerIDs = append(facts.ReplacedIndexerIDs, replacedTorznabIndexers(torznabIndexers, site.Host)...)
		}
		out = append(out, facts)
	}
	return out, nil
}
