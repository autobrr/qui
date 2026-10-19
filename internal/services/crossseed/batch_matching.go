// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"maps"
	"path"
	"slices"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/rls"
	"github.com/rs/zerolog/log"

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
// Cross-Seed tab (localMatchTier), without the retitled rescue. The cached
// torrent fields select the Name and Release candidates and the torrents that
// can share a file path, and only their file lists are fetched, through the
// files cache. The Hardlink and Reflink check runs only for a Name or Release
// candidate whose file sizes do not pair, because only that candidate can
// change the result.
func (s *Service) BuildCrossMatchSets(ctx context.Context, currentInstanceID int, needs CrossMatchNeeds) *CrossMatchResult {
	result := &CrossMatchResult{}
	needsSame := needs.SameExists || needs.SameSeeding || needs.SameTags || needs.SameContent
	needsOther := needs.OtherExists || needs.OtherSeeding

	if !needsSame && !needsOther {
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
	}

	// Select the candidates of every torrent first, so that the file lists they
	// need are read in one batch per instance.
	var samePicks, otherPicks [][]*candidatePick
	var pathGroups [][]int
	var peerReadFailed bool
	if needsSame {
		if sameIdx := s.buildIndexFromViews(currentTorrents, needs.SameSeeding); sameIdx != nil {
			samePicks = s.selectAllCandidates(ctx, currentTorrents, sameIdx, true)
			pathGroups = sharedPathGroups(currentTorrents)
		}
	}
	if needsOther && listErr == nil {
		var otherIdx *matchIndex
		otherIdx, peerReadFailed = s.buildOtherInstanceIndex(ctx, instances, currentInstanceID, needs.OtherSeeding)
		if otherIdx != nil {
			otherPicks = s.selectAllCandidates(ctx, currentTorrents, otherIdx, false)
		}
	}
	files.prefetch(ctx, currentInstanceID, currentTorrents, pathGroups, samePicks, otherPicks)

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

	// A peer whose torrents could not be read can hold the only copy.
	sameUnknown := needsSame && listErr != nil
	otherUnknown := needsOther && (listErr != nil || peerReadFailed)
	if sameUnknown {
		result.SameInstanceUnknown = make(map[string]struct{}, len(currentTorrents))
	}
	if otherUnknown {
		result.OtherInstanceUnknown = make(map[string]struct{}, len(currentTorrents))
	}
	if sameUnknown || otherUnknown {
		for _, torrent := range currentTorrents {
			if sameUnknown {
				result.SameInstanceUnknown[torrent.Hash] = struct{}{}
			}
			if otherUnknown {
				result.OtherInstanceUnknown[torrent.Hash] = struct{}{}
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
}

type localLinkResult struct {
	tier   string
	unread bool
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
func (s *Service) selectAllCandidates(ctx context.Context, torrents []qbittorrent.CrossInstanceTorrentView, idx *matchIndex, sameInstance bool) [][]*candidatePick {
	picks := make([][]*candidatePick, len(torrents))
	for i := range torrents {
		if ctx.Err() != nil {
			break
		}
		picks[i] = s.selectCandidates(torrents[i].Torrent, idx, sameInstance)
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

// linkedTier returns matchTypeHardlink or matchTypeReflink when the candidate
// files are linked copies of the source files, as the Cross-Seed tab checks
// them. A failed verification keeps the result unknown.
func (b *batchFiles) linkedTier(ctx context.Context, source *qbt.Torrent, sourceInstanceID int, c candidateEntry) localLinkResult {
	candidateInstance := instanceByID(b.instances, c.instanceID)
	// Check the cheap gates here, so that a source without file identity
	// never builds a context.
	if candidateInstance == nil || !models.FilesystemCapabilitiesOf(candidateInstance).Identity ||
		!instanceHas(b.instances, sourceInstanceID, models.CapabilityIdentity) {
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
		}
		b.links[key] = matchCtx
	}
	tier := b.svc.localLinkedMatchType(matchCtx, candidateInstance, c.view)
	result := localLinkResult{
		tier:   tier,
		unread: tier == "" && (matchCtx.verificationErr != nil || matchCtx.sourceFilesErr != nil || matchCtx.candidateFilesErr != nil),
	}
	b.linkTiers[pair] = result
	return result
}

// buildIndexFromViews builds a matchIndex from CrossInstanceTorrentViews.
func (s *Service) buildIndexFromViews(views []qbittorrent.CrossInstanceTorrentView, needsSeeding bool) *matchIndex {
	if len(views) == 0 {
		return nil
	}

	idx := newMatchIndex()
	for i := range views {
		view := &views[i]
		s.indexView(idx, view, needsSeeding)
	}

	return idx
}

func newMatchIndex() *matchIndex {
	return &matchIndex{
		byName:     make(map[string][]candidateEntry),
		byTitleKey: make(map[titleEpisodeKey][]*releaseCandidate),
	}
}

// buildOtherInstanceIndex builds a matchIndex from all torrents on other instances.
// readFailed is set when the torrents of an instance could not be read.
func (s *Service) buildOtherInstanceIndex(ctx context.Context, instances []*models.Instance, currentInstanceID int, needsSeeding bool) (idx *matchIndex, readFailed bool) {
	idx = newMatchIndex()
	hasEntries := false

	for _, inst := range instances {
		if inst.ID == currentInstanceID || !inst.IsActive {
			continue
		}
		if ctx.Err() != nil {
			break
		}

		views, err := s.syncManager.GetCachedInstanceTorrents(ctx, inst.ID)
		if err != nil {
			log.Debug().Err(err).Int("instanceID", inst.ID).Msg("crossseed: skipping instance for cross-instance matching")
			readFailed = true
			continue
		}

		for i := range views {
			s.indexView(idx, &views[i], needsSeeding)
			hasEntries = true
		}
	}

	if !hasEntries {
		return nil, readFailed
	}
	return idx, readFailed
}

// indexView adds a single torrent view to all indexes.
func (s *Service) indexView(idx *matchIndex, view *qbittorrent.CrossInstanceTorrentView, needsSeeding bool) {
	entry := newCandidateEntry(view, needsSeeding)

	// Index by normalized name
	name := normalizeLowerTrim(view.Name)
	if name != "" {
		idx.byName[name] = append(idx.byName[name], entry)
	}

	read := s.matcher().readName(view.Name)
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
// from the index with the same prefilter as the Cross-Seed tab. sameInstance
// excludes the source itself.
func (s *Service) selectCandidates(source *qbt.Torrent, idx *matchIndex, sameInstance bool) []*candidatePick {
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

	sourceRead := s.matcher().readName(source.Name)
	// A dated source meets the torrents of its date and the undated torrents.
	// An undated source meets the undated torrents and every dated torrent.
	sourceDate := releaseDate(sourceRead.release)
	otherDate := 0
	if sourceDate == 0 {
		otherDate = anyDate
	}
	// A candidate under two shared Title keys meets the matcher once.
	evaluated := make(map[*releaseCandidate]struct{})
	for _, title := range s.matcher().titleKeys(sourceRead) {
		for _, date := range [2]int{sourceDate, otherDate} {
			for _, c := range idx.byTitleKey[titleEpisodeKey{title: title, episode: sourceRead.release.Episode, date: date}] {
				if _, done := evaluated[c]; done || (sameInstance && c.hash == sourceHash) {
					continue
				}
				evaluated[c] = struct{}{}
				match, reason := s.matcher().releasesMatchWithReasonAndNames(sourceRead.release, c.read.release, source.Name, c.read.rawName, false)
				logReleaseMatchDecision(
					source.Name,
					c.read.rawName,
					false,
					sourceRead.release,
					c.read.release,
					match,
					reason,
					"crossseed: release metadata candidate evaluated",
				)
				if match {
					add(c.candidateEntry, matchTypeRelease)
				}
			}
		}
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
			result.unread = result.unread || linked.unread
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
