// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"maps"
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/trackergroup"
)

// The torrent list query module decides the rows and the sidebar counts of the
// torrent list with one set of definitions (ADR 0043). It reads no client and
// changes no sync manager state: the sync manager gathers its inputs and does
// the side effects that the counts report.

// statusDef is what one status value matches.
type statusDef struct {
	all    bool
	states []qbt.TorrentState
	// invert matches each state that is not in states.
	invert bool
	// health is set for a Tracker health status, which reads the snapshot.
	health TrackerHealth
}

func (d statusDef) matchesState(state qbt.TorrentState) bool {
	return d.all || slices.Contains(d.states, state) != d.invert
}

func (d statusDef) matches(t *qbt.Torrent, health *TrackerHealthSnapshot) bool {
	if d.health != "" {
		return health.Health(t.Hash) == d.health
	}
	return d.matchesState(t.State)
}

var (
	completedStates = []qbt.TorrentState{qbt.TorrentStateUploading, qbt.TorrentStateStalledUp, qbt.TorrentStateQueuedUp,
		qbt.TorrentStateCheckingUp, qbt.TorrentStateForcedUp, qbt.TorrentStatePausedUp, qbt.TorrentStateStoppedUp}
	stoppedStates = []qbt.TorrentState{qbt.TorrentStatePausedDl, qbt.TorrentStatePausedUp,
		qbt.TorrentStateStoppedDl, qbt.TorrentStateStoppedUp}
	seedingStates = []qbt.TorrentState{qbt.TorrentStateUploading, qbt.TorrentStateStalledUp, qbt.TorrentStateQueuedUp,
		qbt.TorrentStateCheckingUp, qbt.TorrentStateForcedUp}
	downloadingStates = []qbt.TorrentState{qbt.TorrentStateDownloading, qbt.TorrentStateStalledDl, qbt.TorrentStateMetaDl,
		qbt.TorrentStateForcedMetaDl, qbt.TorrentStateQueuedDl, qbt.TorrentStateAllocating, qbt.TorrentStateCheckingDl,
		qbt.TorrentStateForcedDl}
	activeStates = []qbt.TorrentState{qbt.TorrentStateDownloading, qbt.TorrentStateUploading,
		qbt.TorrentStateForcedDl, qbt.TorrentStateForcedUp}
)

// namedStatuses are the status values that qui names. A named status wins over
// a qBittorrent state of the same name.
var namedStatuses = map[string]statusDef{
	"all":                 {all: true},
	"completed":           {states: completedStates},
	"stopped":             {states: stoppedStates},
	"paused":              {states: stoppedStates},
	"running":             {states: stoppedStates, invert: true},
	"resumed":             {states: stoppedStates, invert: true},
	"seeding":             {states: seedingStates},
	"uploading":           {states: seedingStates},
	"stalled_seeding":     {states: []qbt.TorrentState{qbt.TorrentStateStalledUp}},
	"stalled_uploading":   {states: []qbt.TorrentState{qbt.TorrentStateStalledUp}},
	"downloading":         {states: downloadingStates},
	"active":              {states: activeStates},
	"inactive":            {states: activeStates, invert: true},
	"stalled":             {states: []qbt.TorrentState{qbt.TorrentStateStalledDl, qbt.TorrentStateStalledUp}},
	"stalled_downloading": {states: []qbt.TorrentState{qbt.TorrentStateStalledDl}},
	"checking": {states: []qbt.TorrentState{qbt.TorrentStateCheckingDl, qbt.TorrentStateCheckingUp,
		qbt.TorrentStateCheckingResumeData}},
	"errored":                         {states: []qbt.TorrentState{qbt.TorrentStateError, qbt.TorrentStateMissingFiles}},
	"moving":                          {states: []qbt.TorrentState{qbt.TorrentStateMoving}},
	string(TrackerHealthUnregistered): {health: TrackerHealthUnregistered},
	string(TrackerHealthDown):         {health: TrackerHealthDown},
	string(TrackerHealthError):        {health: TrackerHealthError},
}

// statesByLowerName lets a qBittorrent state name match in any letter case,
// because the proxy lowercases each status.
var statesByLowerName = func() map[string]qbt.TorrentState {
	byName := make(map[string]qbt.TorrentState)
	for _, state := range qbt.KnownTorrentStates() {
		byName[strings.ToLower(string(state))] = state
	}
	return byName
}()

// lookupStatus resolves a status value. ok is false for a value that qui does
// not know.
func lookupStatus(value string) (def statusDef, ok bool) {
	value = strings.ToLower(value)
	if def, ok := namedStatuses[value]; ok {
		return def, true
	}
	if state, ok := statesByLowerName[value]; ok {
		return statusDef{states: []qbt.TorrentState{state}}, true
	}
	return statusDef{}, false
}

// trackerSources are the inputs that decide the Trackers of a torrent.
type trackerSources struct {
	// mapping is the tracker mapping, hash to domains for the rows. Nil when
	// no mapping exists yet.
	mapping    map[string]map[string]struct{}
	exclusions map[string]map[string]struct{}
	mainData   *qbt.MainData
	// extractDomain keeps its URL cache, so the module gets it from the caller.
	extractDomain func(string) string
}

type trackerURLDomain struct{ domain, url string }

// trackerResolver gives the Trackers of a torrent from the first step that
// gives a result:
//  1. The tracker mapping entry for the torrent, less the tracker exclusions.
//  2. The entries for the torrent in the tracker list of the sync data, less
//     the tracker exclusions, when the torrent has no primary tracker or the
//     list holds the domain of the primary tracker. The mapping is built from
//     this list, so a torrent gets the same Trackers before the mapping holds
//     it. A list without the primary tracker is older than a tracker edit.
//  3. Else the primary tracker, if it is not blank and not a pseudo tracker
//     label.
//  4. Else, with a tracker count of 0, No tracker.
//
// A torrent that gets no result is in no tracker row until the next sync.
type trackerResolver struct {
	trackerSources
	// want limits the index of step 2 to some hashes. Nil indexes each hash.
	want func(hash string) bool
	// mainDataByHash is the tracker list of the sync data, by torrent. It is
	// built at the first torrent that needs step 2.
	mainDataByHash map[string][]trackerURLDomain
}

func newTrackerResolver(src trackerSources, want func(hash string) bool) *trackerResolver {
	return &trackerResolver{trackerSources: src, want: want}
}

// mainDataEntries returns the entries of step 2 for hash.
func (r *trackerResolver) mainDataEntries(hash string) []trackerURLDomain {
	if r.mainData == nil {
		return nil
	}
	if r.mainDataByHash == nil {
		r.mainDataByHash = make(map[string][]trackerURLDomain)
		for trackerURL, hashes := range r.mainData.Trackers {
			domain := r.extractDomain(trackerURL)
			if domain == "" {
				continue
			}
			for _, h := range hashes {
				if r.want != nil && !r.want(h) {
					continue
				}
				entries := r.mainDataByHash[h]
				if !slices.ContainsFunc(entries, func(e trackerURLDomain) bool { return e.domain == domain }) {
					r.mainDataByHash[h] = append(entries, trackerURLDomain{domain, trackerURL})
				}
			}
		}
	}
	return r.mainDataByHash[hash]
}

func (src trackerSources) excluded(domain, hash string) bool {
	_, skip := src.exclusions[domain][hash]
	return skip
}

// mapped reports whether step 1 gives a result for hash.
func (src trackerSources) mapped(hash string) bool {
	for domain := range src.mapping[hash] {
		if !src.excluded(domain, hash) {
			return true
		}
	}
	return false
}

// fallback runs steps 2 to 4 for a torrent that the mapping does not place. It
// calls visit for each domain and reports whether the torrent is in No tracker.
func (r *trackerResolver) fallback(t *qbt.Torrent, visit func(domain, url string)) (noTracker bool) {
	entries := r.mainDataEntries(t.Hash)
	visitEntries := func() (found bool) {
		for _, entry := range entries {
			if !r.excluded(entry.domain, t.Hash) {
				found = true
				visit(entry.domain, entry.url)
			}
		}
		return found
	}
	if primary := strings.TrimSpace(t.Tracker); primary == "" || IsPseudoTrackerLabel(primary) {
		return !visitEntries() && t.TrackersCount == 0
	}
	domain := r.extractDomain(t.Tracker)
	if slices.ContainsFunc(entries, func(e trackerURLDomain) bool { return e.domain == domain }) {
		visitEntries()
		return false
	}
	if domain != "" && !r.excluded(domain, t.Hash) {
		visit(domain, t.Tracker)
	}
	return false
}

// inAny reports whether one of the Trackers of t is in set. The key "" is No
// tracker. A torrent with no result is in no row.
func (r *trackerResolver) inAny(t *qbt.Torrent, set map[string]struct{}) bool {
	placed, hit := false, false
	for domain := range r.mapping[t.Hash] {
		if r.excluded(domain, t.Hash) {
			continue
		}
		placed = true
		if _, ok := set[domain]; ok {
			hit = true
		}
	}
	if placed {
		return hit
	}
	if r.fallback(t, func(domain, _ string) {
		if _, ok := set[domain]; ok {
			hit = true
		}
	}) {
		_, hit = set[""]
	}
	return hit
}

// torrentListRule decides whether one torrent is a row of a request.
type torrentListRule struct {
	// none is set by a status value that qui does not know: the request gives no rows.
	none              bool
	hashes            map[string]struct{}
	status            []statusDef
	excludeStatus     []statusDef
	categories        []string
	excludeCategories []string
	useSubcategories  bool
	tags              map[string]struct{}
	excludeTags       map[string]struct{}
	trackers          map[string]struct{}
	excludeTrackers   map[string]struct{}
	tracker           *trackerResolver
	health            *TrackerHealthSnapshot
}

func hasTrackerFilter(filters FilterOptions) bool {
	return len(filters.Trackers) > 0 || len(filters.ExcludeTrackers) > 0
}

func setOf(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		set[v] = struct{}{}
	}
	return set
}

// newTorrentListRule builds the rule of filters. Expressions and search are not
// part of the rule. trackers is read only when filters has a tracker filter.
func newTorrentListRule(filters FilterOptions, health *TrackerHealthSnapshot, useSubcategories bool, trackers trackerSources) *torrentListRule {
	r := &torrentListRule{
		categories:        filters.Categories,
		excludeCategories: filters.ExcludeCategories,
		useSubcategories:  useSubcategories,
		tags:              setOf(filters.Tags),
		excludeTags:       setOf(filters.ExcludeTags),
		trackers:          setOf(filters.Trackers),
		excludeTrackers:   setOf(filters.ExcludeTrackers),
		health:            health,
	}
	for _, hash := range filters.Hashes {
		if hash == "" {
			continue
		}
		if r.hashes == nil {
			r.hashes = make(map[string]struct{}, len(filters.Hashes))
		}
		r.hashes[strings.ToUpper(hash)] = struct{}{}
	}

	matchesAll := false
	for _, value := range filters.Status {
		def, ok := lookupStatus(value)
		r.none = r.none || !ok
		matchesAll = matchesAll || def.all
		r.status = append(r.status, def)
	}
	if matchesAll {
		r.status = nil
	}
	for _, value := range filters.ExcludeStatus {
		def, ok := lookupStatus(value)
		r.none = r.none || !ok
		r.excludeStatus = append(r.excludeStatus, def)
	}

	if hasTrackerFilter(filters) {
		var want func(hash string) bool
		switch {
		case trackers.mapping == nil:
		case len(trackers.exclusions) == 0:
			// The mapping holds no hash with an empty set of domains.
			want = func(hash string) bool {
				_, ok := trackers.mapping[hash]
				return !ok
			}
		default:
			want = func(hash string) bool { return !trackers.mapped(hash) }
		}
		r.tracker = newTrackerResolver(trackers, want)
	}
	return r
}

// narrows reports whether the rule can leave a torrent out. A rule that narrows
// nothing lets the rows and the counts share one read of the whole library.
func (r *torrentListRule) narrows() bool {
	return r.none || r.hashes != nil || len(r.status) > 0 || len(r.excludeStatus) > 0 ||
		len(r.categories) > 0 || len(r.excludeCategories) > 0 || r.tags != nil || r.excludeTags != nil ||
		r.tracker != nil
}

func anyStatus(defs []statusDef, t *qbt.Torrent, health *TrackerHealthSnapshot) bool {
	return slices.ContainsFunc(defs, func(d statusDef) bool { return d.matches(t, health) })
}

// inCategory reports whether category is one of wanted. With subcategories on,
// a parent includes each category under its name prefix.
func (r *torrentListRule) inCategory(category string, wanted []string) bool {
	for _, c := range wanted {
		if category == c {
			return true
		}
		if r.useSubcategories && c != "" && len(category) > len(c) && category[len(c)] == '/' && strings.HasPrefix(category, c) {
			return true
		}
	}
	return false
}

// hasTag reports whether one tag of tags is in set. A torrent with only blank
// tags has no tag, which the key "" matches.
func hasTag(tags string, set map[string]struct{}) bool {
	untagged := true
	for tag := range strings.SplitSeq(tags, ",") {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		untagged = false
		if _, ok := set[tag]; ok {
			return true
		}
	}
	if untagged {
		_, ok := set[""]
		return ok
	}
	return false
}

func hashMatches(t *qbt.Torrent, set map[string]struct{}) bool {
	for _, candidate := range [...]string{t.Hash, t.InfohashV1, t.InfohashV2} {
		if candidate == "" {
			continue
		}
		if _, ok := set[strings.ToUpper(candidate)]; ok {
			return true
		}
	}
	return false
}

// match reports whether t is a row of the request. It runs under the read lock
// of the library sync manager, so it keeps no pointer.
func (r *torrentListRule) match(t *qbt.Torrent) bool {
	switch {
	case r.none:
		return false
	case r.hashes != nil && !hashMatches(t, r.hashes):
		return false
	case len(r.status) > 0 && !anyStatus(r.status, t, r.health):
		return false
	case anyStatus(r.excludeStatus, t, r.health):
		return false
	case len(r.categories) > 0 && !r.inCategory(t.Category, r.categories):
		return false
	case r.inCategory(t.Category, r.excludeCategories):
		return false
	case r.tags != nil && !hasTag(t.Tags, r.tags):
		return false
	case r.excludeTags != nil && hasTag(t.Tags, r.excludeTags):
		return false
	case r.trackers != nil && !r.tracker.inAny(t, r.trackers):
		return false
	case r.excludeTrackers != nil && r.tracker.inAny(t, r.excludeTrackers):
		return false
	}
	return true
}

// torrentCountsInput is what the counts pass reads. domainToHashes is the
// tracker mapping by domain, nil when no mapping exists yet. health is nil when
// the instance has no tracker health.
type torrentCountsInput struct {
	torrents         []*qbt.Torrent
	useSubcategories bool
	health           *TrackerHealthSnapshot
	domainToHashes   map[string]map[string]struct{}
	trackers         trackerSources
}

// torrentCountsResult holds the counts and the work they leave for the sync
// manager.
type torrentCountsResult struct {
	counts *TorrentCounts
	// emptiedDomains are domains with tracker exclusions and no torrent left.
	// Their exclusions can go.
	emptiedDomains []string
	// unmap holds, per emptied domain, the hashes to remove from the tracker mapping.
	unmap map[string][]string
	// icons holds one tracker URL for each domain that steps 2 and 3 found.
	icons map[string]string
}

// countedStatuses are the status keys of the sidebar counts. stalled_seeding is
// an alias with no sidebar entry.
var countedStatuses = func() []string {
	keys := slices.Collect(maps.Keys(namedStatuses))
	return slices.DeleteFunc(keys, func(k string) bool { return k == "stalled_seeding" })
}()

// countTorrents makes the sidebar counts of the whole library with the
// definitions of the rule.
func countTorrents(in torrentCountsInput) torrentCountsResult {
	torrents := in.torrents
	counts := &TorrentCounts{
		Status:        make(map[string]int, len(countedStatuses)),
		Categories:    make(map[string]int),
		CategorySizes: make(map[string]int64),
		Tags:          make(map[string]int),
		TagSizes:      make(map[string]int64),
		Trackers:      make(map[string]int),
		Total:         len(torrents),
	}
	sharedContentPaths := findSharedContentPaths(torrents)

	byState := make(map[qbt.TorrentState]int)
	categoryStats := make(map[string]*countWithSize)
	tagStats := make(map[string]*countWithSize)
	for i := range torrents {
		t := torrents[i]
		byState[t.State]++
		addStat(categoryStats, t.Category, t, sharedContentPaths[i])
		tagged := false
		for tag := range strings.SplitSeq(t.Tags, ",") {
			if tag = strings.TrimSpace(tag); tag != "" {
				tagged = true
				addStat(tagStats, tag, t, sharedContentPaths[i])
			}
		}
		if !tagged {
			addStat(tagStats, "", t, sharedContentPaths[i])
		}
	}

	for _, key := range countedStatuses {
		def := namedStatuses[key]
		n := 0
		if def.health == "" {
			for state, count := range byState {
				if def.matchesState(state) {
					n += count
				}
			}
		}
		counts.Status[key] = n
	}
	if in.health != nil {
		in.health.writeCounts(counts.Status)
	}

	for category, stats := range categoryStats {
		counts.Categories[category] = stats.count
		counts.CategorySizes[category] = stats.size.total()
	}
	if in.useSubcategories {
		addSubcategoriesToParents(counts)
	}
	for tag, stats := range tagStats {
		counts.Tags[tag] = stats.count
		counts.TagSizes[tag] = stats.size.total()
	}

	result := torrentCountsResult{counts: counts}
	countTrackers(in, &result)
	return result
}

// addSubcategoriesToParents adds the count and the size of each subcategory to
// each parent above it.
func addSubcategoriesToParents(counts *TorrentCounts) {
	exact := maps.Clone(counts.Categories)
	exactSizes := maps.Clone(counts.CategorySizes)
	for category, count := range exact {
		size := exactSizes[category]
		for i := range len(category) {
			if category[i] == '/' {
				counts.Categories[category[:i]] += count
				counts.CategorySizes[category[:i]] += size
			}
		}
	}
}

// countTrackers adds the tracker counts with the steps of trackerResolver.
func countTrackers(in torrentCountsInput, result *torrentCountsResult) {
	resolveTrackers(in, func(_ int, domain, url string) {
		result.counts.Trackers[domain]++
		if url == "" {
			return
		}
		if result.icons == nil {
			result.icons = make(map[string]string)
		}
		if _, ok := result.icons[domain]; !ok && domain != "Unknown" {
			result.icons[domain] = url
		}
	}, func(int) {
		result.counts.Trackers[""]++
	})

	reportEmptiedDomains(in, result)
}

// resolveTrackers runs the steps of trackerResolver for each torrent. Step 1
// reads the mapping by domain, and steps 2 to 4 run only on the torrents that
// step 1 does not place. It calls visit for each domain of torrent i, with the
// URL that gave the domain, or "" when step 1 gave it. It calls noTracker for a
// torrent in No tracker.
func resolveTrackers(in torrentCountsInput, visit func(i int, domain, url string), noTracker func(i int)) {
	torrents := in.torrents
	placed := placeMappedTorrents(in, func(domain string, i int) { visit(i, domain, "") })
	// Without a mapping, no torrent is placed, so each hash is needed.
	var want func(hash string) bool
	if in.domainToHashes != nil {
		need := make(map[string]struct{})
		for i := range torrents {
			if !placed[i] {
				need[torrents[i].Hash] = struct{}{}
			}
		}
		want = func(hash string) bool {
			_, ok := need[hash]
			return ok
		}
	}
	resolver := newTrackerResolver(in.trackers, want)
	for i := range torrents {
		if placed[i] {
			continue
		}
		if resolver.fallback(torrents[i], func(domain, url string) { visit(i, domain, url) }) {
			noTracker(i)
		}
	}
}

// placeMappedTorrents runs step 1 for each domain of the mapping, and reports
// which torrents it placed.
func placeMappedTorrents(in torrentCountsInput, add func(domain string, i int)) []bool {
	placed := make([]bool, len(in.torrents))
	if in.domainToHashes == nil {
		return placed
	}
	index := make(map[string]int, len(in.torrents))
	for i := range in.torrents {
		index[in.torrents[i].Hash] = i
	}
	for domain, hashes := range in.domainToHashes {
		for hash := range hashes {
			i, ok := index[hash]
			if !ok {
				continue
			}
			if in.trackers.excluded(domain, hash) {
				continue
			}
			placed[i] = true
			add(domain, i)
		}
	}
	return placed
}

// reportEmptiedDomains lists the domains whose exclusions no longer hide a
// torrent, with the excluded hashes to drop from the mapping.
func reportEmptiedDomains(in torrentCountsInput, result *torrentCountsResult) {
	for domain, hashes := range in.trackers.exclusions {
		if _, ok := result.counts.Trackers[domain]; ok {
			continue
		}
		result.emptiedDomains = append(result.emptiedDomains, domain)
		if in.domainToHashes == nil {
			continue
		}
		if result.unmap == nil {
			result.unmap = make(map[string][]string)
		}
		result.unmap[domain] = slices.Collect(maps.Keys(hashes))
	}
}

// trackerSet holds the totals of the torrents that share one set of tracker
// domains. Public torrents share one set whatever their domains are.
type trackerSet struct {
	domains []string
	public  bool
	stats   trackerDomainStats
}

// groupTrackers sums the torrents of the input by Tracker group. A torrent
// counts toward one group, and a content path that two torrents of one group
// share counts once in it.
func groupTrackers(in torrentCountsInput, mapper *trackergroup.Mapper) map[trackergroup.Group]TrackerTransferStats {
	torrents := in.torrents
	domainsOf := make([][]string, len(torrents))
	resolveTrackers(in, func(i int, domain, _ string) {
		domainsOf[i] = append(domainsOf[i], domain)
	}, func(int) {})

	sharedContentPaths := findSharedContentPaths(torrents)
	byKey := make(map[string]*trackerSet)
	var sets []*trackerSet
	for i := range torrents {
		t := torrents[i]
		public := !PrivateUnknown(t) && !t.Private
		domains := domainsOf[i]
		key := "\x00public"
		if public {
			domains = nil
		} else {
			slices.Sort(domains)
			key = strings.Join(domains, "\x00")
		}
		set := byKey[key]
		if set == nil {
			set = &trackerSet{domains: domains, public: public}
			byKey[key] = set
			sets = append(sets, set)
		}
		set.stats.add(t, sharedContentPaths[i])
	}

	merged := make(map[trackergroup.Group]*trackerDomainStats)
	for _, set := range sets {
		var private *bool
		if set.public {
			private = new(false)
		}
		group := mapper.Of(set.domains, private)
		total := merged[group]
		if total == nil {
			total = &trackerDomainStats{}
			merged[group] = total
		}
		total.merge(&set.stats)
	}
	groups := make(map[trackergroup.Group]TrackerTransferStats, len(merged))
	for group, total := range merged {
		groups[group] = total.totals()
	}
	return groups
}
