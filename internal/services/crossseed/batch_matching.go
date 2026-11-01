// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"cmp"
	"context"
	"maps"
	"path"
	"slices"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/rls"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// CrossMatchNeeds specifies which cross-match sets to compute.
type CrossMatchNeeds struct {
	SameExists   bool
	SameSeeding  bool
	SameTags     bool
	SameContent  bool
	OtherExists  bool
	OtherSeeding bool
	// OtherShared asks for the torrents that share a file with a torrent on
	// another instance, and for the torrents that qui could not check.
	OtherShared bool
}

// CrossMatchResult contains all cross-match sets for a given instance.
type CrossMatchResult struct {
	Err                  error // The current torrent snapshot could not be read.
	SameInstanceExists   map[string]struct{}
	SameInstanceSeeding  map[string]struct{}
	OtherInstanceExists  map[string]struct{}
	OtherInstanceSeeding map[string]struct{}
	// SameInstanceTagsByHash maps a source hash to the sorted raw Tags strings of
	// its same-instance cross-seed matches (self excluded). A hash is present only
	// when at least one match carries a non-empty tag string.
	SameInstanceTagsByHash map[string][]string
	// SameInstanceContentByHash links torrents on the same instance that share
	// at least one file on disk (the Content tier, self excluded). It keeps only
	// enough links to connect each shared-file group, so read a group by
	// following the links, not from one entry. A candidate whose files could not
	// be read is also linked, so that a delete or move fails closed. A hash is
	// present only when it has a link.
	SameInstanceContentByHash map[string][]string
	// SameInstanceUnknown and OtherInstanceUnknown hold the torrents whose
	// file list, candidate's file list, or instance list could not be read.
	// Conditions without a confirmed positive match are unknown for this run.
	SameInstanceUnknown  map[string]struct{}
	OtherInstanceUnknown map[string]struct{}
	// OtherInstanceShared holds the torrents that share a file with a torrent
	// on another active instance. Both instances need trusted identity.
	// OtherInstanceSharedUnknown holds the torrents that qui could not check:
	// every torrent while OfflineLocalInstances names an instance or the
	// instance list cannot be read, and a torrent whose file list or folders
	// could not be read.
	OtherInstanceShared        map[string]struct{}
	OtherInstanceSharedUnknown map[string]struct{}
	// OfflineLocalInstances names the active instances with trusted identity
	// whose torrents could not be read.
	OfflineLocalInstances []string
}

// candidateEntry holds pre-computed data for a candidate torrent.
type candidateEntry struct {
	hash       string
	tags       string
	seeding    bool
	instanceID int
	savePath   string
	view       *qbittorrent.CrossInstanceTorrentView
}

func newCandidateEntry(view *qbittorrent.CrossInstanceTorrentView, needsSeeding bool) candidateEntry {
	return candidateEntry{
		hash:       normalizeHash(view.Hash),
		tags:       view.Tags,
		seeding:    needsSeeding && isTorrentViewSeeding(view),
		instanceID: view.InstanceID,
		savePath:   view.SavePath,
		view:       view,
	}
}

// releaseCandidate extends candidateEntry with the Read release for rls matching.
type releaseCandidate struct {
	candidateEntry
	read namedRelease
}

// matchIndex holds pre-built indexes that select the Name and Release
// candidates of a source torrent before their file lists are fetched.
type matchIndex struct {
	byName map[string][]candidateEntry // key: normalizeLowerTrim(name)
	// byTitleKey holds each torrent under every Title key. A grouping key can
	// be looser than the matcher, never stricter: the matcher still decides
	// each pair.
	byTitleKey map[titleEpisodeKey][]*releaseCandidate
	// bySize holds each torrent under its exact total size, so that a retitled
	// copy or a copy that numbers its episodes in another way meets the source.
	bySize map[int64][]*releaseCandidate
}

// titleEpisodeKey adds the episode and the air date to a Title key, so that
// the episodes of a long-running show or a daily show do not all meet each
// other. The matcher, with individual episodes off, accepts only a pair with
// equal episodes, and rejects two full dates that differ. A name without a
// full date can match any date, so it meets the dated torrents through
// anyDate. Thus the key stays looser than the matcher.
type titleEpisodeKey struct {
	title   string
	episode int
	date    int // yyyymmdd, 0 without a full date, or anyDate
}

// anyDate holds every dated torrent of a Title key and episode, for a source
// without a full date.
const anyDate = -1

func releaseDate(r *rls.Release) int {
	if r.Year > 0 && r.Month > 0 && r.Day > 0 {
		return r.Year*10000 + r.Month*100 + r.Day
	}
	return 0
}

// BuildCrossMatchSets builds sets of torrent hashes that have cross-seeds on the
// same instance and/or other instances. It uses the same per-file rule as the
// Cross-Seed tab (localMatchTier), and it always applies title rescue. The cached
// torrent fields select the Name and Release candidates and the torrents that
// can share a file path, and only their file lists are fetched, through the
// files cache. The Hardlink and Reflink check runs only for a Name or Release
// candidate whose file sizes do not pair, because only that candidate can
// change the result.
func (s *Service) BuildCrossMatchSets(ctx context.Context, currentInstanceID int, needs CrossMatchNeeds) *CrossMatchResult {
	result := &CrossMatchResult{}
	needsSame := needs.SameExists || needs.SameSeeding || needs.SameTags || needs.SameContent
	needsOther := needs.OtherExists || needs.OtherSeeding

	if !needsSame && !needsOther && !needs.OtherShared {
		return result
	}

	// Get current instance torrents (needed for both same and other matching)
	currentTorrents, err := s.syncManager.GetCachedInstanceTorrents(ctx, currentInstanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", currentInstanceID).Msg("crossseed: failed to get current instance torrents")
		result.Err = err
		return result
	}

	// Keep shared-path safety groups available if the instance list fails.
	// Conditions become unknown below because link and other-instance checks need it.
	instances, listErr := s.instanceStore.List(ctx)
	if listErr != nil {
		log.Error().Err(listErr).Msg("crossseed: failed to list instances for cross-matching")
	}

	files := &batchFiles{
		svc:       s,
		instances: instances,
		byKey:     make(map[string]*batchFileData),
		fetched:   make(map[string]qbt.TorrentFiles),
		links:     make(map[string]*localMatchContext),
		linkTiers: make(map[[2]string]localLinkResult),
		folders:   make(map[string]fsops.FileKey),
	}

	reads := s.readLocalTorrents(ctx, currentInstanceID, viewTorrents(currentTorrents))

	// Select the candidates of every torrent first, so that the file lists they
	// need are read in one batch per instance.
	var samePicks, otherPicks, sharedPicks [][]*candidatePick
	var sharedGroups []crossPathGroup
	var pathGroups [][]int
	var offline []*models.Instance
	if needsSame {
		if sameIdx := s.buildIndexFromViews(currentTorrents, needs.SameSeeding, reads); sameIdx != nil {
			samePicks = s.selectAllCandidates(ctx, currentTorrents, sameIdx, true, reads)
			pathGroups = sharedPathGroups(currentTorrents)
		}
	}
	checkShared := needs.OtherShared && instanceHas(instances, currentInstanceID, models.CapabilityTrustedIdentity)
	if (needsOther || checkShared) && listErr == nil {
		otherIdx, others, failed := s.buildOtherInstanceIndex(ctx, instances, currentInstanceID, needs.OtherSeeding, reads)
		offline = failed
		if otherIdx != nil {
			picks := s.selectAllCandidates(ctx, currentTorrents, otherIdx, false, reads)
			if needsOther {
				otherPicks = picks
			}
			if checkShared {
				// Only a candidate with trusted identity can share a file, so qui reads no other file list.
				untrusted := func(p *candidatePick) bool {
					return !instanceHas(instances, p.entry.instanceID, models.CapabilityTrustedIdentity)
				}
				sharedPicks = make([][]*candidatePick, len(picks))
				for i := range picks {
					trusted := slices.DeleteFunc(slices.Clone(picks[i]), untrusted)
					sharedPicks[i] = addSizePicks(instances, trusted, otherIdx.bySize[searchSourceSize(currentTorrents[i].Torrent)])
				}
				sharedGroups = crossPathGroups(instances, currentTorrents, others, func(string) bool { return true })
			}
		}
	}
	groupSources, groupPicks := prefetchGroups(len(currentTorrents), sharedGroups)
	files.prefetch(ctx, currentInstanceID, currentTorrents, slices.Concat(pathGroups, groupSources), samePicks, otherPicks, sharedPicks, groupPicks)

	if samePicks != nil {
		result.SameInstanceContentByHash = files.addSharedPathPicks(ctx, currentInstanceID, currentTorrents, pathGroups, samePicks, needs)
		result.SameInstanceExists = make(map[string]struct{})
		result.SameInstanceUnknown = make(map[string]struct{})
		if needs.SameSeeding {
			result.SameInstanceSeeding = make(map[string]struct{})
		}
		if needs.SameTags {
			result.SameInstanceTagsByHash = make(map[string][]string)
		}
		for i := range currentTorrents {
			if ctx.Err() != nil {
				break
			}
			source := currentTorrents[i].Torrent
			m := s.classifyCandidates(ctx, source, currentInstanceID, samePicks[i], true, needs.SameTags, files)
			if needs.SameContent && len(m.unreadCandidates) > 0 {
				hashes := result.SameInstanceContentByHash[source.Hash]
				hashes = append(hashes, m.unreadCandidates...)
				slices.Sort(hashes)
				result.SameInstanceContentByHash[source.Hash] = slices.Compact(hashes)
			}
			if m.unread {
				result.SameInstanceUnknown[source.Hash] = struct{}{}
			}
			if !m.matched {
				continue
			}
			result.SameInstanceExists[source.Hash] = struct{}{}
			if needs.SameSeeding && m.seeding {
				result.SameInstanceSeeding[source.Hash] = struct{}{}
			}
			if needs.SameTags && len(m.memberTags) > 0 {
				result.SameInstanceTagsByHash[source.Hash] = m.memberTags
			}
		}
	}

	if otherPicks != nil {
		result.OtherInstanceExists = make(map[string]struct{})
		result.OtherInstanceUnknown = make(map[string]struct{})
		if needs.OtherSeeding {
			result.OtherInstanceSeeding = make(map[string]struct{})
		}
		for i := range currentTorrents {
			if ctx.Err() != nil {
				break
			}
			source := currentTorrents[i].Torrent
			m := s.classifyCandidates(ctx, source, currentInstanceID, otherPicks[i], false, false, files)
			if m.unread {
				result.OtherInstanceUnknown[source.Hash] = struct{}{}
			}
			if m.matched {
				result.OtherInstanceExists[source.Hash] = struct{}{}
				if needs.OtherSeeding && m.seeding {
					result.OtherInstanceSeeding[source.Hash] = struct{}{}
				}
			}
		}
	}

	if checkShared {
		result.OtherInstanceShared = make(map[string]struct{})
		result.OtherInstanceSharedUnknown = make(map[string]struct{})
		if sharedPicks != nil {
			files.addOtherShared(ctx, currentInstanceID, currentTorrents, sharedPicks, sharedGroups, result)
		}
		for _, inst := range offline {
			if models.FilesystemCapabilitiesOf(inst).Has(models.CapabilityTrustedIdentity) {
				result.OfflineLocalInstances = append(result.OfflineLocalInstances, inst.Name)
			}
		}
	}

	// A peer whose torrents could not be read can hold the only copy.
	sameUnknown := needsSame && listErr != nil
	otherUnknown := needsOther && (listErr != nil || len(offline) > 0)
	sharedUnknown := needs.OtherShared && (listErr != nil || len(result.OfflineLocalInstances) > 0)
	if sameUnknown {
		result.SameInstanceUnknown = make(map[string]struct{}, len(currentTorrents))
	}
	if otherUnknown {
		result.OtherInstanceUnknown = make(map[string]struct{}, len(currentTorrents))
	}
	if sharedUnknown {
		result.OtherInstanceSharedUnknown = make(map[string]struct{}, len(currentTorrents))
	}
	if sameUnknown || otherUnknown || sharedUnknown {
		for _, torrent := range currentTorrents {
			if sameUnknown {
				result.SameInstanceUnknown[torrent.Hash] = struct{}{}
			}
			if otherUnknown {
				result.OtherInstanceUnknown[torrent.Hash] = struct{}{}
			}
			if sharedUnknown {
				result.OtherInstanceSharedUnknown[torrent.Hash] = struct{}{}
			}
		}
	}

	return result
}

// batchFiles keeps the file data of each torrent for one BuildCrossMatchSets
// call, so that a torrent that is a candidate of many sources is read once.
type batchFiles struct {
	svc       *Service
	instances []*models.Instance
	byKey     map[string]*batchFileData     // nil value: the fetch failed
	fetched   map[string]qbt.TorrentFiles   // file lists that prefetch read
	links     map[string]*localMatchContext // per source, keeps its stat results
	// linkTiers holds the link check result per pair of torrents. A link
	// goes both ways, so the pair is checked once.
	linkTiers map[[2]string]localLinkResult
	// folders holds the file identity of each folder that sharedFile read. A
	// zero key is a folder that does not exist or has no file identity. Only
	// local instances reach it, so a path on the qui host is the key.
	folders map[string]fsops.FileKey
	// trustedIdentityOnly limits links to instances with TrustedIdentity, as
	// localMatchContext does.
	trustedIdentityOnly bool
	// otherSharedOnly gives a candidate on another instance a tier only for a
	// Shared file, as the delete check asks.
	otherSharedOnly bool
}

type localLinkResult struct {
	tier string
	err  error // the check could not be read, so an empty tier is unknown
}

type batchFileData struct {
	data  sameDataFiles
	files qbt.TorrentFiles
}

func (b *batchFiles) get(ctx context.Context, instanceID int, hash, savePath string) (batchFileData, bool) {
	key := cacheKeyForTorrentFiles(instanceID, hash)
	if data, ok := b.byKey[key]; ok {
		if data == nil {
			return batchFileData{}, false
		}
		return *data, true
	}
	files, ok := b.fetched[key]
	var err error
	if !ok {
		files, err = b.svc.getTorrentFilesCached(ctx, instanceID, hash)
	}
	if err != nil || len(files) == 0 {
		log.Debug().Err(err).Int("instanceID", instanceID).Str("hash", normalizeHash(hash)).
			Msg("crossseed: skipping cross-match candidate without a file list")
		b.byKey[key] = nil
		return batchFileData{}, false
	}
	data := batchFileData{data: newSameDataFiles(savePath, files, normalizerForService(b.svc)), files: files}
	b.byKey[key] = &data
	return data, true
}

// selectAllCandidates runs selectCandidates for each torrent of the instance.
func (s *Service) selectAllCandidates(ctx context.Context, torrents []qbittorrent.CrossInstanceTorrentView, idx *matchIndex, sameInstance bool, reads localReads) [][]*candidatePick {
	picks := make([][]*candidatePick, len(torrents))
	for i := range torrents {
		if ctx.Err() != nil {
			break
		}
		source := torrents[i].Torrent
		picks[i] = s.selectCandidates(source, reads.read(s.matcher(), torrents[i].InstanceID, source), idx, sameInstance)
	}
	return picks
}

// prefetch reads the file lists of the torrents in pathGroups, of the sources
// with candidates, and of their candidates, in one GetTorrentFilesBatch call
// per instance. The sync manager then fetches the lists that it has no copy of
// in parallel, not one after another. A list that the call does not return is
// read again by get.
func (b *batchFiles) prefetch(ctx context.Context, sourceInstanceID int, sources []qbittorrent.CrossInstanceTorrentView, pathGroups [][]int, picksBySource ...[][]*candidatePick) {
	need := make(map[int]map[string]struct{})
	add := func(instanceID int, hash string) {
		if need[instanceID] == nil {
			need[instanceID] = make(map[string]struct{})
		}
		need[instanceID][hash] = struct{}{}
	}
	for _, group := range pathGroups {
		for _, i := range group {
			add(sourceInstanceID, normalizeHash(sources[i].Hash))
		}
	}
	for _, picks := range picksBySource {
		for i, sourcePicks := range picks {
			if len(sourcePicks) == 0 || sources[i].ContentPath == "" {
				continue
			}
			add(sourceInstanceID, normalizeHash(sources[i].Hash))
			for _, p := range sourcePicks {
				if p.entry.view.ContentPath != "" {
					add(p.entry.instanceID, p.entry.hash)
				}
			}
		}
	}

	for instanceID, hashSet := range need {
		hashes := slices.Collect(maps.Keys(hashSet))
		filesByHash, err := b.svc.syncManager.GetTorrentFilesBatch(ctx, instanceID, hashes)
		if err != nil {
			log.Debug().Err(err).Int("instanceID", instanceID).Msg("crossseed: some cross-match file lists could not be read")
		}
		for hash, files := range filesByHash {
			key := cacheKeyForTorrentFiles(instanceID, hash)
			b.fetched[key] = files
			if b.svc.torrentFilesCache != nil {
				b.svc.torrentFilesCache.Set(key, cloneTorrentFiles(files), ttlcache.DefaultTTL)
			}
		}
	}
}

// sharedPathGroups groups the torrents whose cached paths allow a shared file
// on disk: the torrents with one content path, a torrent with the torrents
// whose content path is inside its content path, and the torrents of a save
// path that holds a multi-file torrent without a subfolder (its content path is
// the save path). It returns the indexes into torrents of each group with two
// or more members.
func sharedPathGroups(torrents []qbittorrent.CrossInstanceTorrentView) [][]int {
	byContentPath := make(map[string][]int)
	bySavePath := make(map[string][]int)
	flatSavePaths := make(map[string]bool)
	for i := range torrents {
		contentPath := normalizePathForComparison(torrents[i].ContentPath)
		// A magnet without metadata has no files.
		if contentPath == "" {
			continue
		}
		byContentPath[contentPath] = append(byContentPath[contentPath], i)
		if savePath := normalizePathForComparison(torrents[i].SavePath); savePath != "" {
			bySavePath[savePath] = append(bySavePath[savePath], i)
			if contentPath == savePath {
				flatSavePaths[savePath] = true
			}
		}
	}
	var groups [][]int
	for _, group := range byContentPath {
		if len(group) > 1 {
			groups = append(groups, group)
		}
	}
	// qui adds an episode cross-seed inside the folder of its season pack.
	for contentPath, group := range byContentPath {
		for inner, dir := contentPath, path.Dir(contentPath); dir != inner && dir != "." && dir != "/"; inner, dir = dir, path.Dir(dir) {
			if outer := byContentPath[dir]; len(outer) > 0 {
				groups = append(groups, slices.Concat(group, outer))
			}
		}
	}
	for savePath, group := range bySavePath {
		if len(group) > 1 && flatSavePaths[savePath] {
			groups = append(groups, group)
		}
	}
	return groups
}

// addSharedPathPicks adds to the picks of each torrent in pathGroups the
// torrents that share a file path with it. One index of file paths finds them,
// so a flat folder costs its file count, not its pair count. A torrent whose
// file list cannot be read is paired with every member of its groups, so that
// a delete or move fails closed. Safety groups also include ignored files.
func (b *batchFiles) addSharedPathPicks(
	ctx context.Context,
	instanceID int,
	torrents []qbittorrent.CrossInstanceTorrentView,
	pathGroups [][]int,
	picks [][]*candidatePick,
	needs CrossMatchNeeds,
) map[string][]string {
	capacity := 0
	for _, group := range pathGroups {
		capacity = max(capacity, len(group))
	}
	var sharedPaths map[string][]string
	if needs.SameContent {
		sharedPaths = make(map[string][]string, capacity)
	}

	picked := make(map[int]map[string]struct{})
	link := func(i, j int) {
		if i == j {
			return
		}
		have := picked[i]
		if have == nil {
			have = make(map[string]struct{}, len(picks[i])+1)
			for _, p := range picks[i] {
				have[p.entry.hash] = struct{}{}
			}
			picked[i] = have
		}
		entry := newCandidateEntry(&torrents[j], needs.SameSeeding)
		if _, ok := have[entry.hash]; ok {
			return
		}
		have[entry.hash] = struct{}{}
		picks[i] = append(picks[i], &candidatePick{entry: entry})
	}

	var parent []int
	if sharedPaths != nil && len(pathGroups) > 0 {
		parent = make([]int, len(torrents))
		for i := range parent {
			parent[i] = i
		}
	}
	root := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	// A connecting edge is enough; repeated shared files need no more edges.
	share := func(i, j int) {
		if sharedPaths == nil || i == j {
			return
		}
		a, z := root(i), root(j)
		if a == z {
			return
		}
		parent[z] = a
		first, next := torrents[i].Hash, torrents[j].Hash
		sharedPaths[first] = append(sharedPaths[first], next)
		sharedPaths[next] = append(sharedPaths[next], first)
	}
	var firstByPath map[string]int
	if needs.SameContent {
		firstByPath = make(map[string]int, capacity)
	}
	read := make(map[int]bool, capacity)
	byPath := make(map[string][]int, capacity)
	for _, group := range pathGroups {
		for _, i := range group {
			if _, done := read[i]; done {
				continue
			}
			data, ok := b.get(ctx, instanceID, torrents[i].Hash, torrents[i].SavePath)
			read[i] = ok
			for p := range data.data.paths {
				byPath[p] = append(byPath[p], i)
			}
			if sharedPaths != nil && ok && torrents[i].SavePath != "" {
				for _, file := range data.files {
					p := normalizePathForComparison(torrents[i].SavePath + "/" + file.Name)
					if first, exists := firstByPath[p]; exists {
						share(first, i)
					} else {
						firstByPath[p] = i
					}
				}
			}
		}
	}
	for _, sharing := range byPath {
		for _, i := range sharing {
			for _, j := range sharing {
				link(i, j)
			}
		}
	}
	for _, group := range pathGroups {
		for _, i := range group {
			if read[i] {
				continue
			}
			for _, j := range group {
				link(i, j)
				link(j, i)
				share(i, j)
			}
		}
	}
	for _, hashes := range sharedPaths {
		slices.Sort(hashes)
	}
	return sharedPaths
}

// folderEntry is one file name in one folder on disk.
type folderEntry struct {
	folder fsops.FileKey
	name   string
}

// crossPathGroup is a group of sharedPathGroups that joins torrents of the
// instance (sources, indexes into its torrents) with torrents on other
// instances with trusted identity.
type crossPathGroup struct {
	sources []int
	others  []*candidatePick
}

// crossPathGroups returns the groups of sharedPathGroups over the torrents of
// the instance that isSelected keeps and the torrents of others on instances
// with trusted identity that hold torrents of both sides. A group is one unit,
// so that a flat save path costs its torrent count, not its pair count.
func crossPathGroups(instances []*models.Instance, torrents, others []qbittorrent.CrossInstanceTorrentView, isSelected func(hash string) bool) []crossPathGroup {
	all, sources := selectedAndTrustedOthers(instances, torrents, others, isSelected)
	var groups []crossPathGroup
	for _, members := range sharedPathGroups(all) {
		var group crossPathGroup
		for _, member := range members {
			if member < len(sources) {
				group.sources = append(group.sources, sources[member])
			} else {
				group.others = append(group.others, &candidatePick{entry: newCandidateEntry(&all[member], false)})
			}
		}
		if len(group.sources) > 0 && len(group.others) > 0 {
			groups = append(groups, group)
		}
	}
	return groups
}

// prefetchGroups gives prefetch the sources of each group, and the other
// torrents of each group as picks of its first source.
func prefetchGroups(torrentCount int, groups []crossPathGroup) (sources [][]int, picks [][]*candidatePick) {
	if len(groups) == 0 {
		return nil, nil
	}
	picks = make([][]*candidatePick, torrentCount)
	for _, group := range groups {
		sources = append(sources, group.sources)
		first := group.sources[0]
		picks[first] = append(picks[first], group.others...)
	}
	return sources, picks
}

// addOtherShared adds to result.OtherInstanceShared each torrent with a file
// in a folder entry of a candidate on another instance with trusted identity.
// A candidate is a pick of the torrent or a torrent of one of its path
// groups. A torrent whose file list or folders, or a candidate's, could not be
// read goes to result.OtherInstanceSharedUnknown. The candidates only limit
// which torrents qui reads: a folder entry of any candidate counts for every
// torrent, because a Shared file is a fact on disk.
func (b *batchFiles) addOtherShared(
	ctx context.Context,
	instanceID int,
	torrents []qbittorrent.CrossInstanceTorrentView,
	picks [][]*candidatePick,
	groups []crossPathGroup,
	result *CrossMatchResult,
) {
	_, groupPicks := prefetchGroups(len(torrents), groups)
	entries, candidateRead := b.candidateFolderEntries(ctx, picks, groupPicks)
	// readStatus reports whether qui read any of the candidates, and whether
	// a read failed.
	readStatus := func(candidates []*candidatePick) (checked, unknown bool) {
		for _, p := range candidates {
			ok, read := candidateRead[cacheKeyForTorrentFiles(p.entry.instanceID, p.entry.hash)]
			checked = checked || read
			unknown = unknown || (read && !ok)
		}
		return checked, unknown
	}
	checked := make([]bool, len(torrents))
	unknown := make([]bool, len(torrents))
	for i := range picks {
		checked[i], unknown[i] = readStatus(picks[i])
	}
	for _, group := range groups {
		groupChecked, groupUnknown := readStatus(group.others)
		for _, i := range group.sources {
			checked[i] = checked[i] || groupChecked
			unknown[i] = unknown[i] || groupUnknown
		}
	}

	for i := range torrents {
		if ctx.Err() != nil {
			return
		}
		source := torrents[i].Torrent
		if !checked[i] || source.ContentPath == "" {
			continue
		}
		if unknown[i] {
			result.OtherInstanceSharedUnknown[source.Hash] = struct{}{}
		}
		folders, ok := b.readFolders(ctx, instanceID, source.Hash, source.SavePath)
		switch {
		case !ok:
			result.OtherInstanceSharedUnknown[source.Hash] = struct{}{}
		case sharesFolderEntry(folders, entries):
			result.OtherInstanceShared[source.Hash] = struct{}{}
		}
	}
}

// candidateFolderEntries reads the folder entries of each candidate on an
// instance with trusted identity. read maps the files cache key of each
// candidate that it checked to whether the read succeeded.
func (b *batchFiles) candidateFolderEntries(ctx context.Context, picksBySource ...[][]*candidatePick) (entries map[folderEntry]struct{}, read map[string]bool) {
	entries = make(map[folderEntry]struct{})
	read = make(map[string]bool)
	for _, picks := range picksBySource {
		for _, sourcePicks := range picks {
			for _, p := range sourcePicks {
				c := p.entry
				key := cacheKeyForTorrentFiles(c.instanceID, c.hash)
				if _, done := read[key]; done || c.view.ContentPath == "" ||
					!instanceHas(b.instances, c.instanceID, models.CapabilityTrustedIdentity) {
					continue
				}
				folders, ok := b.readFolders(ctx, c.instanceID, c.hash, c.savePath)
				read[key] = ok
				for name, keys := range folders {
					for _, folder := range keys {
						entries[folderEntry{folder, name}] = struct{}{}
					}
				}
			}
		}
	}
	return entries, read
}

// readFolders returns folderEntries for every file of a torrent, ignored files
// too, because a delete with files removes them. ok is false when its file
// list or a folder could not be read.
func (b *batchFiles) readFolders(ctx context.Context, instanceID int, hash, savePath string) (map[string][]fsops.FileKey, bool) {
	data, ok := b.get(ctx, instanceID, hash, savePath)
	if !ok {
		return nil, false
	}
	folders, err := b.folderEntries(ctx, instanceID, savePath, data.files)
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Str("hash", normalizeHash(hash)).
			Msg("crossseed: could not read the folders of a torrent for the shared file check")
		return nil, false
	}
	return folders, true
}

func sharesFolderEntry(folders map[string][]fsops.FileKey, entries map[folderEntry]struct{}) bool {
	for name, keys := range folders {
		for _, folder := range keys {
			if _, shared := entries[folderEntry{folder, name}]; shared {
				return true
			}
		}
	}
	return false
}

// linkedTier returns matchTypeHardlink or matchTypeReflink when the candidate
// files are linked copies of the source files, as the Cross-Seed tab checks
// them. A failed verification keeps the result unknown.
func (b *batchFiles) linkedTier(ctx context.Context, source *qbt.Torrent, sourceInstanceID int, c candidateEntry) localLinkResult {
	candidateInstance := instanceByID(b.instances, c.instanceID)
	identity := models.CapabilityIdentity
	if b.trustedIdentityOnly {
		identity = models.CapabilityTrustedIdentity
	}
	// Check the cheap gates here, so that a source without file identity
	// never builds a context.
	if candidateInstance == nil || !models.FilesystemCapabilitiesOf(candidateInstance).Has(identity) ||
		!instanceHas(b.instances, sourceInstanceID, identity) {
		return localLinkResult{}
	}
	key := cacheKeyForTorrentFiles(sourceInstanceID, source.Hash)
	pair := [2]string{key, cacheKeyForTorrentFiles(c.instanceID, c.hash)}
	if pair[1] < pair[0] {
		pair[0], pair[1] = pair[1], pair[0]
	}
	if tier, ok := b.linkTiers[pair]; ok {
		return tier
	}
	matchCtx, ok := b.links[key]
	if !ok {
		matchCtx = &localMatchContext{
			ctx:                   ctx,
			svc:                   b.svc,
			sourceInstanceID:      sourceInstanceID,
			sourceInstance:        instanceByID(b.instances, sourceInstanceID),
			sourceHash:            source.Hash,
			sourceSavePath:        source.SavePath,
			sourceContentPath:     source.ContentPath,
			sourceHasFileIdentity: true,
			sourceLocal:           instanceIsLocal(b.instances, sourceInstanceID),
			trustedIdentityOnly:   b.trustedIdentityOnly,
		}
		b.links[key] = matchCtx
	}
	tier := b.svc.localLinkedMatchType(matchCtx, candidateInstance, c.view)
	result := localLinkResult{tier: tier}
	if tier == "" {
		result.err = cmp.Or(matchCtx.sourceFilesErr, matchCtx.candidateFilesErr, matchCtx.verificationErr)
	}
	b.linkTiers[pair] = result
	return result
}

// buildIndexFromViews builds a matchIndex from CrossInstanceTorrentViews.
func (s *Service) buildIndexFromViews(views []qbittorrent.CrossInstanceTorrentView, needsSeeding bool, reads localReads) *matchIndex {
	if len(views) == 0 {
		return nil
	}

	idx := newMatchIndex()
	for i := range views {
		view := &views[i]
		s.indexView(idx, view, needsSeeding, reads)
	}

	return idx
}

func newMatchIndex() *matchIndex {
	return &matchIndex{
		byName:     make(map[string][]candidateEntry),
		byTitleKey: make(map[titleEpisodeKey][]*releaseCandidate),
		bySize:     make(map[int64][]*releaseCandidate),
	}
}

// buildOtherInstanceIndex builds a matchIndex from all torrents on other
// active instances, and adds their reads from files to reads. views holds the
// torrents that it read. offline holds the instances whose torrents could not
// be read.
func (s *Service) buildOtherInstanceIndex(ctx context.Context, instances []*models.Instance, currentInstanceID int, needsSeeding bool, reads localReads) (idx *matchIndex, views []qbittorrent.CrossInstanceTorrentView, offline []*models.Instance) {
	idx = newMatchIndex()

	for _, inst := range instances {
		if inst.ID == currentInstanceID || !inst.IsActive {
			continue
		}
		if ctx.Err() != nil {
			break
		}

		instanceViews, err := s.syncManager.GetCachedInstanceTorrents(ctx, inst.ID)
		if err != nil {
			log.Debug().Err(err).Int("instanceID", inst.ID).Msg("crossseed: skipping instance for cross-instance matching")
			offline = append(offline, inst)
			continue
		}

		maps.Copy(reads, s.readLocalTorrents(ctx, inst.ID, viewTorrents(instanceViews)))
		for i := range instanceViews {
			s.indexView(idx, &instanceViews[i], needsSeeding, reads)
		}
		if views == nil {
			views = instanceViews
		} else {
			views = append(views, instanceViews...)
		}
	}

	if len(views) == 0 {
		return nil, nil, offline
	}
	return idx, views, offline
}

// indexView adds a single torrent view to all indexes.
func (s *Service) indexView(idx *matchIndex, view *qbittorrent.CrossInstanceTorrentView, needsSeeding bool, reads localReads) {
	entry := newCandidateEntry(view, needsSeeding)

	// Index by normalized name
	name := normalizeLowerTrim(view.Name)
	if name != "" {
		idx.byName[name] = append(idx.byName[name], entry)
	}

	read := reads.read(s.matcher(), view.InstanceID, view.Torrent)
	candidate := &releaseCandidate{candidateEntry: entry, read: read}
	date := releaseDate(read.release)
	for _, title := range s.matcher().titleKeys(read) {
		key := titleEpisodeKey{title: title, episode: read.release.Episode, date: date}
		idx.byTitleKey[key] = append(idx.byTitleKey[key], candidate)
		if date != 0 {
			key.date = anyDate
			idx.byTitleKey[key] = append(idx.byTitleKey[key], candidate)
		}
	}
	if size := searchSourceSize(view.Torrent); size > 0 {
		idx.bySize[size] = append(idx.bySize[size], candidate)
	}
}

// indexMatch is the result of classifyCandidates for one source torrent.
type indexMatch struct {
	matched bool
	seeding bool // a match is seeding
	// memberTags holds the non-empty raw Tags strings of the matches, sorted,
	// when collectTags is set.
	memberTags []string
	// Unread candidates outside the path groups still need conservative action grouping.
	unreadCandidates []string
	// unread is set when the file list of the source or of a candidate could
	// not be read, so the other fields may be wrong.
	unread bool
}

// candidatePick is a candidate that passed the prefilter. nameOrRelease is the
// tier it gets if its file sizes pair with the source. It is empty for a
// torrent that addSharedPathPicks added.
type candidatePick struct {
	entry         candidateEntry
	nameOrRelease string
}

// selectCandidates selects the Name and Release candidates of a source torrent
// from the index: its name group, its Title key and episode groups, and its
// size group. sameInstance excludes the source itself.
func (s *Service) selectCandidates(source *qbt.Torrent, sourceRead namedRelease, idx *matchIndex, sameInstance bool) []*candidatePick {
	sourceHash := normalizeHash(source.Hash)

	var picks []*candidatePick
	picked := make(map[string]struct{})
	add := func(c candidateEntry, nameOrRelease string) {
		if sameInstance && c.hash == sourceHash {
			return
		}
		key := cacheKeyForTorrentFiles(c.instanceID, c.hash)
		if _, ok := picked[key]; ok {
			return
		}
		picked[key] = struct{}{}
		picks = append(picks, &candidatePick{entry: c, nameOrRelease: nameOrRelease})
	}

	name := normalizeLowerTrim(source.Name)
	if name != "" {
		for _, c := range idx.byName[name] {
			add(c, matchTypeName)
		}
	}

	// A dated source meets the torrents of its date and the undated torrents.
	// An undated source meets the undated torrents and every dated torrent.
	sourceDate := releaseDate(sourceRead.release)
	otherDate := 0
	if sourceDate == 0 {
		otherDate = anyDate
	}
	// A candidate that two groups select meets the matcher once.
	evaluated := make(map[*releaseCandidate]struct{})
	evaluate := func(c *releaseCandidate) {
		if _, done := evaluated[c]; done || (sameInstance && c.hash == sourceHash) {
			return
		}
		evaluated[c] = struct{}{}
		verdict := s.matcher().matchVerdict(sourceRead, c.read,
			matchEvidence{SourceSize: searchSourceSize(source), CandidateSize: searchSourceSize(c.view.Torrent)}, localMatchPolicy)
		logReleaseMatchDecision(
			source.Name,
			c.read.rawName,
			false,
			sourceRead.release,
			c.read.release,
			verdict.Matched,
			verdict.RejectReason,
			"crossseed: release metadata candidate evaluated",
		)
		if verdict.Matched {
			add(c.candidateEntry, matchTypeRelease)
		}
	}
	for _, title := range s.matcher().titleKeys(sourceRead) {
		for _, date := range [2]int{sourceDate, otherDate} {
			for _, c := range idx.byTitleKey[titleEpisodeKey{title: title, episode: sourceRead.release.Episode, date: date}] {
				evaluate(c)
			}
		}
	}
	for _, c := range idx.bySize[searchSourceSize(source)] {
		evaluate(c)
	}
	return picks
}

// classifyCandidates classifies each candidate of a source torrent with
// localMatchTier. sameInstance allows the Content tier. A candidate whose file
// list cannot be read is not a match and sets unread.
func (s *Service) classifyCandidates(
	ctx context.Context,
	source *qbt.Torrent,
	sourceInstanceID int,
	picks []*candidatePick,
	sameInstance, collectTags bool,
	files *batchFiles,
) indexMatch {
	var result indexMatch
	// A magnet without metadata has no files, so nothing holds its data.
	if len(picks) == 0 || source.ContentPath == "" {
		return result
	}
	sourceData, ok := files.get(ctx, sourceInstanceID, source.Hash, source.SavePath)
	if !ok {
		result.unread = true
		if sameInstance {
			for _, p := range picks {
				if p.entry.view.ContentPath != "" {
					result.unreadCandidates = append(result.unreadCandidates, p.entry.hash)
				}
			}
		}
		return result
	}

	for _, p := range picks {
		c := p.entry
		// A magnet without metadata has no files.
		if c.view.ContentPath == "" {
			continue
		}
		candidateData, ok := files.get(ctx, c.instanceID, c.hash, c.savePath)
		if !ok {
			result.unread = true
			if sameInstance {
				result.unreadCandidates = append(result.unreadCandidates, c.hash)
			}
			continue
		}
		tier := localMatchTier(sameInstance, p.nameOrRelease, sourceData.data, candidateData.data, nil)
		if tier == "" && p.nameOrRelease != "" {
			linked := files.linkedTier(ctx, source, sourceInstanceID, c)
			tier = linked.tier
			result.unread = result.unread || linked.err != nil
		}
		if tier == "" {
			continue
		}
		result.matched = true
		if c.seeding {
			result.seeding = true
		}
		if collectTags && c.tags != "" {
			result.memberTags = append(result.memberTags, c.tags)
		}
	}

	slices.Sort(result.memberTags)
	return result
}

// isTorrentViewSeeding checks if a CrossInstanceTorrentView is in a seeding state.
func isTorrentViewSeeding(view *qbittorrent.CrossInstanceTorrentView) bool {
	if view == nil || view.TorrentView == nil || view.Torrent == nil || view.Progress < 1.0 {
		return false
	}
	switch view.State {
	case qbt.TorrentStateUploading, qbt.TorrentStateStalledUp,
		qbt.TorrentStateQueuedUp, qbt.TorrentStateCheckingUp,
		qbt.TorrentStateForcedUp:
		return true
	case qbt.TorrentStateError, qbt.TorrentStateMissingFiles,
		qbt.TorrentStatePausedUp, qbt.TorrentStateStoppedUp,
		qbt.TorrentStateAllocating, qbt.TorrentStateDownloading,
		qbt.TorrentStateMetaDl, qbt.TorrentStatePausedDl,
		qbt.TorrentStateStoppedDl, qbt.TorrentStateQueuedDl,
		qbt.TorrentStateStalledDl, qbt.TorrentStateCheckingDl,
		qbt.TorrentStateForcedDl, qbt.TorrentStateCheckingResumeData,
		qbt.TorrentStateMoving, qbt.TorrentStateUnknown:
		return false
	}
	return false
}
