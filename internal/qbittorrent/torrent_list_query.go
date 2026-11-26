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

type statusDef struct {
	all    bool
	states []qbt.TorrentState
	invert bool
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

var statesByLowerName = func() map[string]qbt.TorrentState {
	byName := make(map[string]qbt.TorrentState)
	for _, state := range qbt.KnownTorrentStates() {
		byName[strings.ToLower(string(state))] = state
	}
	return byName
}()

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

type trackerSources struct {
	mapping       map[string]map[string]struct{}
	exclusions    map[string]map[string]struct{}
	mainData      *qbt.MainData
	extractDomain func(string) string
}

type trackerURLDomain struct{ domain, url string }

type trackerResolver struct {
	trackerSources
	want           func(hash string) bool
	mainDataByHash map[string][]trackerURLDomain
}

func newTrackerResolver(src trackerSources, want func(hash string) bool) *trackerResolver {
	return &trackerResolver{trackerSources: src, want: want}
}

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

func (src trackerSources) mapped(hash string) bool {
	for domain := range src.mapping[hash] {
		if !src.excluded(domain, hash) {
			return true
		}
	}
	return false
}

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

type torrentListRule struct {
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

func (r *torrentListRule) narrows() bool {
	return r.none || r.hashes != nil || len(r.status) > 0 || len(r.excludeStatus) > 0 ||
		len(r.categories) > 0 || len(r.excludeCategories) > 0 || r.tags != nil || r.excludeTags != nil ||
		r.tracker != nil
}

func anyStatus(defs []statusDef, t *qbt.Torrent, health *TrackerHealthSnapshot) bool {
	return slices.ContainsFunc(defs, func(d statusDef) bool { return d.matches(t, health) })
}

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

type torrentCountsInput struct {
	torrents         []*qbt.Torrent
	useSubcategories bool
	health           *TrackerHealthSnapshot
	domainToHashes   map[string]map[string]struct{}
	trackers         trackerSources
}

type torrentCountsResult struct {
	counts         *TorrentCounts
	emptiedDomains []string
	unmap          map[string][]string
	icons          map[string]string
}

var countedStatuses = func() []string {
	keys := slices.Collect(maps.Keys(namedStatuses))
	return slices.DeleteFunc(keys, func(k string) bool { return k == "stalled_seeding" })
}()

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

func resolveTrackers(in torrentCountsInput, visit func(i int, domain, url string), noTracker func(i int)) {
	torrents := in.torrents
	placed := placeMappedTorrents(in, func(domain string, i int) { visit(i, domain, "") })
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

type trackerSet struct {
	domains []string
	public  bool
	stats   trackerDomainStats
}

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
