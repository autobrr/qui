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

const (
	noEligibleIndexersSkipReason        = "no eligible indexers"
	noIndexersAfterFilteringSkipReason  = "no eligible indexers after filtering"
	indexersInCooldownSkipReason        = "all eligible indexers within cooldown"
	noIndexerCapsSkipReason             = "no indexers support required caps"
	selectedIndexerContentSkipReason    = "selected indexers were filtered out"
	selectedIndexerCapabilitySkipReason = "selected indexers do not support required caps"
	noGazelleKeySkipReason              = "no RED/OPS API key for the target site"
	gazelleAPISkipReason                = "asked through the RED/OPS API"
	indexerContentSkipReason            = "filtered out by the content filter"
	indexerCapabilitySkipReason         = "does not support the required caps"
	indexerInCooldownSkipReason         = "within cooldown"
)

type searchScope struct {
	sourceInstanceID  int
	torznabIndexerIDs []int
	gazelle           []GazelleLookup
	gazelleKeyMissing bool
	exportedTorrent   []byte
	skipReason        string
	facts             searchScopeFacts
	torznabErr        error
	err               error
}

type searchScopeFacts struct {
	requestedIndexerIDs    []int
	picked                 bool
	torznabOff             bool
	cooldownIndexerIDs     []int
	skipGazelle            bool
	gazelleOff             bool
	gazelleKeys            []string
	gazelleSelected        []string
	gazelleTorznabIndexers map[int]string

	contentFilter        contentFilterFacts
	sourceSite           string
	duplicateSourceSites []string
	musicCheckPassed     bool
	gazelleLocalSeeded   []string
	reuseLocalChecks     bool
}

type contentFilterFacts struct {
	err               error
	torznabErr        error
	done              bool
	indexerIDs        []int
	capableIDs        []int
	seededOn          int
	noCapableIndexers bool
}

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
		scope = searchScope{skipReason: torznabErr.Error(), torznabErr: torznabErr}
	}
	scope.gazelle = decideGazelleSites(f)
	scope.gazelleKeyMissing = gazelleKeyMissing(f)
	if !f.torznabOff || scope.asksGazelle() || f.skipGazelle {
		return scope
	}
	if len(f.gazelleKeys) == 0 {
		return searchScope{err: fmt.Errorf("%w: torznab disabled but gazelle not configured", ErrInvalidRequest)}
	}
	if !gazelleCooldownDue(scope.gazelle, scope.gazelleKeyMissing) {
		scope.skipReason = noGazelleKeySkipReason
	}
	return scope
}

func decideGazelleSites(f searchScopeFacts) []GazelleLookup {
	if f.gazelleOff {
		return []GazelleLookup{}
	}
	fromGazelle := f.sourceSite != "" || len(f.duplicateSourceSites) > 0
	sites := gazellemusic.Sites()
	out := make([]GazelleLookup, 0, len(sites))
	for _, site := range sites {
		host := site.Host
		if !slices.Contains(f.gazelleKeys, host) {
			continue
		}
		d := GazelleLookup{
			Site:               host,
			Name:               site.DisplayName,
			ShortName:          site.ShortName,
			IndexerID:          site.IndexerID,
			Status:             GazelleWillAsk,
			ReplacedIndexerIDs: replacedTorznabIndexers(f.gazelleTorznabIndexers, host),
			seededByCopy:       gazelleSeededByCopy(f, host),
		}
		switch {
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

func gazelleSeededByCopy(f searchScopeFacts, host string) bool {
	return host == f.sourceSite || slices.Contains(f.duplicateSourceSites, host)
}

func gazelleKeyMissing(f searchScopeFacts) bool {
	if f.gazelleOff {
		return false
	}
	return slices.ContainsFunc(gazellemusic.Sites(), func(site gazellemusic.Site) bool {
		return !slices.Contains(f.gazelleKeys, site.Host) && !gazelleSeededByCopy(f, site.Host)
	})
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

func skippedTorznabIndexers(f searchScopeFacts, asked []int, skipReason string) map[int]string {
	skipped := map[int]string{}
	for _, id := range uniquePositiveInts(f.requestedIndexerIDs) {
		host, isGazelle := f.gazelleTorznabIndexers[id]
		switch {
		case slices.Contains(asked, id):
		case isGazelle && slices.Contains(f.gazelleKeys, host):
			skipped[id] = gazelleAPISkipReason
		case f.torznabOff || f.contentFilter.torznabErr != nil:
			skipped[id] = skipReason
		case !slices.Contains(f.contentFilter.capableIDs, id):
			skipped[id] = indexerCapabilitySkipReason
		case !slices.Contains(f.contentFilter.indexerIDs, id):
			skipped[id] = indexerContentSkipReason
		case slices.Contains(f.cooldownIndexerIDs, id):
			skipped[id] = indexerInCooldownSkipReason
		default:
			skipped[id] = skipReason
		}
	}
	return skipped
}

func (s *Service) collectSearchScope(ctx context.Context, instanceID int, torrent *qbt.Torrent, f searchScopeFacts, resolveIndexers bool) searchScope {
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
	scope.facts = f
	return scope
}

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
	for i, d := range plan {
		if d.Status == GazelleWillAsk && s.keyRejectedOpen(gazelleLookupSite(d)) {
			plan[i].Reason = GazelleReasonKeyRejected
		}
	}
	return plan, nil
}

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

type gazelleLocalResult struct {
	checked  []string
	seeded   []string
	exported []byte
}

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
		capableIDs:        snapshot.CapabilityIndexers,
		seededOn:          len(snapshot.ExcludedIndexers),
		noCapableIndexers: len(snapshot.CapabilityIndexers) == 0,
	}
	if snapshot.ContentCompleted {
		facts.indexerIDs = snapshot.FilteredIndexers
	}
	return facts
}

func withoutGazelleTorznabIndexers(ids []int, gazelleIndexers map[int]string, gazelleKeys []string) []int {
	return slices.DeleteFunc(slices.Clone(ids), func(id int) bool {
		host, ok := gazelleIndexers[id]
		return ok && slices.Contains(gazelleKeys, host)
	})
}

func (s *Service) searchRunIndexerIDs(ctx context.Context, requested []int, gazelleKeys []string) ([]int, error) {
	ids, err := s.resolveTorznabIndexerIDs(ctx, requested)
	if err != nil || len(gazelleKeys) == 0 {
		return ids, err
	}
	return withoutGazelleTorznabIndexers(ids, s.gazelleTorznabIndexers(ctx), gazelleKeys), nil
}

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

type GazelleSiteFacts struct {
	Site               string `json:"site"`
	Name               string `json:"name"`
	ShortName          string `json:"shortName"`
	IndexerID          int    `json:"indexerId"`
	Active             bool   `json:"active"`
	ReplacedIndexerIDs []int  `json:"replacedIndexerIds"`
}

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
