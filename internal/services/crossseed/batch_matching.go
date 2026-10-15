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
	SameInstanceExists   map[string]struct{}
	SameInstanceSeeding  map[string]struct{}
	OtherInstanceExists  map[string]struct{}
	OtherInstanceSeeding map[string]struct{}
	// SameInstanceTagsByHash maps a source hash to the sorted raw Tags strings of
	// its same-instance cross-seed matches (self excluded). A hash is present only
	// when at least one match carries a non-empty tag string.
	SameInstanceTagsByHash map[string][]string
	// SameInstanceContentByHash maps a source hash to the hashes of the torrents
	// on the same instance that share at least one file on disk with it (the
	// Content tier, self excluded). A candidate whose files could not be read is
	// also listed, so that a delete or move fails closed. A hash is present only
	// when it has such a torrent.
	SameInstanceContentByHash map[string][]string
	// SameInstanceUnknown and OtherInstanceUnknown hold the torrents whose
	// file list, or a candidate's file list, could not be read on that side.
	// Their cross-seed condition values are unknown for this run.
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

// releaseCandidate extends candidateEntry with parsed release metadata for rls matching.
type releaseCandidate struct {
	candidateEntry
	name    string
	release *rls.Release
}

// matchIndex holds pre-built indexes that select the Name and Release
// candidates of a source torrent before their file lists are fetched.
type matchIndex struct {
	byName       map[string][]candidateEntry       // key: normalizeLowerTrim(name)
	byReleaseKey map[releaseKey][]releaseCandidate // for release metadata matching
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
		return result
	}

	// Without the instance list, the link check stays off and the
	// other-instance sets stay empty.
	instances, listErr := s.instanceStore.List(ctx)
	if listErr != nil {
		log.Error().Err(listErr).Msg("crossseed: failed to list instances for cross-matching")
	}

	files := &batchFiles{
		svc:       s,
		instances: instances,
		byKey:     make(map[string]*sameDataFiles),
		fetched:   make(map[string]qbt.TorrentFiles),
		links:     make(map[string]*localMatchContext),
		linkTiers: make(map[[2]string]localLinkResult),
	}

	// Select the candidates of every torrent first, so that the file lists they
	// need are read in one batch per instance.
	var samePicks, otherPicks [][]*candidatePick
	var pathGroups [][]int
	if needsSame {
		if sameIdx := s.buildIndexFromViews(currentTorrents, needs.SameSeeding); sameIdx != nil {
			samePicks = s.selectAllCandidates(ctx, currentTorrents, sameIdx, true)
			pathGroups = sharedPathGroups(currentTorrents)
		}
	}
	if needsOther && listErr == nil {
		if otherIdx := s.buildOtherInstanceIndex(ctx, instances, currentInstanceID, needs.OtherSeeding); otherIdx != nil {
			otherPicks = s.selectAllCandidates(ctx, currentTorrents, otherIdx, false)
		}
	}
	files.prefetch(ctx, currentInstanceID, currentTorrents, pathGroups, samePicks, otherPicks)

	if samePicks != nil {
		files.addSharedPathPicks(ctx, currentInstanceID, currentTorrents, pathGroups, samePicks, needs.SameSeeding)
		result.SameInstanceExists = make(map[string]struct{})
		result.SameInstanceUnknown = make(map[string]struct{})
		if needs.SameSeeding {
			result.SameInstanceSeeding = make(map[string]struct{})
		}
		if needs.SameTags {
			result.SameInstanceTagsByHash = make(map[string][]string)
		}
		if needs.SameContent {
			result.SameInstanceContentByHash = make(map[string][]string)
		}
		for i := range currentTorrents {
			if ctx.Err() != nil {
				break
			}
			source := currentTorrents[i].Torrent
			m := s.classifyCandidates(ctx, source, currentInstanceID, samePicks[i], true, needs.SameTags, files)
			if needs.SameContent && len(m.content) > 0 {
				result.SameInstanceContentByHash[source.Hash] = m.content
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

	return result
}

// batchFiles keeps the file data of each torrent for one BuildCrossMatchSets
// call, so that a torrent that is a candidate of many sources is read once.
type batchFiles struct {
	svc       *Service
	instances []*models.Instance
	byKey     map[string]*sameDataFiles     // nil value: the fetch failed
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

func (b *batchFiles) get(ctx context.Context, instanceID int, hash, savePath string) (sameDataFiles, bool) {
	key := cacheKeyForTorrentFiles(instanceID, hash)
	if data, ok := b.byKey[key]; ok {
		if data == nil {
			return sameDataFiles{}, false
		}
		return *data, true
	}
	files, ok := b.fetched[key]
	var err error
	if !ok {
		files, err = b.svc.getTorrentFilesCached(ctx, instanceID, hash)
		if err == nil {
			b.fetched[key] = files
		}
	}
	if err != nil || len(files) == 0 {
		log.Debug().Err(err).Int("instanceID", instanceID).Str("hash", normalizeHash(hash)).
			Msg("crossseed: skipping cross-match candidate without a file list")
		b.byKey[key] = nil
		return sameDataFiles{}, false
	}
	data := newSameDataFiles(savePath, files, normalizerForService(b.svc))
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
// a delete or move fails closed.
func (b *batchFiles) addSharedPathPicks(
	ctx context.Context,
	instanceID int,
	torrents []qbittorrent.CrossInstanceTorrentView,
	pathGroups [][]int,
	picks [][]*candidatePick,
	needsSeeding bool,
) {
	picked := make(map[int]map[string]*candidatePick)
	link := func(i, j int, sharesFilePath bool) {
		if i == j {
			return
		}
		have := picked[i]
		if have == nil {
			have = make(map[string]*candidatePick, len(picks[i])+1)
			for _, p := range picks[i] {
				have[p.entry.hash] = p
			}
			picked[i] = have
		}
		entry := newCandidateEntry(&torrents[j], needsSeeding)
		if existing, ok := have[entry.hash]; ok {
			existing.sharesFilePath = existing.sharesFilePath || sharesFilePath
			return
		}
		pick := &candidatePick{entry: entry, sharesFilePath: sharesFilePath}
		have[entry.hash] = pick
		picks[i] = append(picks[i], pick)
	}

	read := make(map[int]bool)
	byPath := make(map[string][]int)
	allByPath := make(map[string][]int)
	for _, group := range pathGroups {
		for _, i := range group {
			if _, done := read[i]; done {
				continue
			}
			data, ok := b.get(ctx, instanceID, torrents[i].Hash, torrents[i].SavePath)
			read[i] = ok
			if !ok || torrents[i].SavePath == "" {
				continue
			}
			for p := range data.paths {
				byPath[p] = append(byPath[p], i)
			}
			// Destructive actions must preserve shared sidecars too.
			for _, file := range b.fetched[cacheKeyForTorrentFiles(instanceID, torrents[i].Hash)] {
				p := normalizePathForComparison(torrents[i].SavePath + "/" + file.Name)
				allByPath[p] = append(allByPath[p], i)
			}
		}
	}
	for _, sharing := range byPath {
		for _, i := range sharing {
			for _, j := range sharing {
				link(i, j, true)
			}
		}
	}
	// One shared member connects the safety group without checking every pair.
	for _, sharing := range allByPath {
		for _, i := range sharing[1:] {
			link(sharing[0], i, true)
			link(i, sharing[0], true)
		}
	}
	for _, group := range pathGroups {
		for _, i := range group {
			if read[i] {
				continue
			}
			for _, j := range group {
				link(i, j, false)
				link(j, i, false)
			}
		}
	}
}

// linkedTier returns matchTypeHardlink or matchTypeReflink when the candidate
// files are linked copies of the source files, as the Cross-Seed tab checks
// them. A failed verification keeps the result unknown.
func (b *batchFiles) linkedTier(ctx context.Context, source *qbt.Torrent, sourceInstanceID int, c candidateEntry) localLinkResult {
	candidateInstance := instanceByID(b.instances, c.instanceID)
	// Check the cheap gates here, so that a source without file identity
	// never builds a context.
	if candidateInstance == nil || !models.FilesystemCapabilitiesOf(candidateInstance).Identity ||
		!instanceHasFileIdentity(b.instances, sourceInstanceID) {
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
		byName:       make(map[string][]candidateEntry),
		byReleaseKey: make(map[releaseKey][]releaseCandidate),
	}
}

// buildOtherInstanceIndex builds a matchIndex from all torrents on other instances.
func (s *Service) buildOtherInstanceIndex(ctx context.Context, instances []*models.Instance, currentInstanceID int, needsSeeding bool) *matchIndex {
	idx := newMatchIndex()
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
			continue
		}

		for i := range views {
			s.indexView(idx, &views[i], needsSeeding)
			hasEntries = true
		}
	}

	if !hasEntries {
		return nil
	}
	return idx
}

// indexView adds a single torrent view to all indexes.
func (s *Service) indexView(idx *matchIndex, view *qbittorrent.CrossInstanceTorrentView, needsSeeding bool) {
	entry := newCandidateEntry(view, needsSeeding)

	// Index by normalized name
	name := normalizeLowerTrim(view.Name)
	if name != "" {
		idx.byName[name] = append(idx.byName[name], entry)
	}

	// Index by release key for rls matching
	release := s.matcher().parseReleaseName(view.Name)
	title := normalizeLowerTrim(release.Title)
	if title != "" {
		key := makeReleaseKey(release)
		idx.byReleaseKey[key] = append(idx.byReleaseKey[key], releaseCandidate{
			candidateEntry: entry,
			name:           view.Name,
			release:        release,
		})
	}
}

// indexMatch is the result of classifyCandidates for one source torrent.
type indexMatch struct {
	matched bool
	seeding bool // a match is seeding
	// memberTags holds the non-empty raw Tags strings of the matches, sorted,
	// when collectTags is set.
	memberTags []string
	// content holds same-instance torrents that share any file path, including
	// ignored files, and candidates whose files could not be read.
	content []string
	// unread is set when the file list of the source or of a candidate could
	// not be read, so the other fields may be wrong.
	unread bool
}

// candidatePick is a candidate that passed the prefilter. nameOrRelease is the
// tier it gets if its file sizes pair with the source. It is empty for a
// torrent that addSharedPathPicks added.
type candidatePick struct {
	entry          candidateEntry
	nameOrRelease  string
	sharesFilePath bool
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

	sourceRelease := s.matcher().parseReleaseName(source.Name)
	if normalizeLowerTrim(sourceRelease.Title) != "" {
		for _, c := range idx.byReleaseKey[makeReleaseKey(sourceRelease)] {
			if sameInstance && c.hash == sourceHash {
				continue
			}
			match, reason := s.matcher().releasesMatchWithReason(sourceRelease, c.release, false)
			logReleaseMatchDecision(
				source.Name,
				c.name,
				false,
				sourceRelease,
				c.release,
				match,
				reason,
				"crossseed: release metadata candidate evaluated",
			)
			if match {
				add(c.candidateEntry, matchTypeRelease)
			}
		}
	}
	return picks
}

// classifyCandidates classifies each candidate of a source torrent with
// localMatchTier. sameInstance allows the Content tier. A candidate whose file
// list cannot be read is not a match and sets unread, and on the same instance
// it goes into content.
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
					result.content = append(result.content, p.entry.hash)
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
				result.content = append(result.content, c.hash)
			}
			continue
		}
		tier := localMatchTier(sameInstance, p.nameOrRelease, sourceData, candidateData, nil)
		if tier == "" && p.nameOrRelease != "" {
			linked := files.linkedTier(ctx, source, sourceInstanceID, c)
			tier = linked.tier
			result.unread = result.unread || linked.unread
		}
		if sameInstance && (p.sharesFilePath || tier == matchTypeContentPath) {
			result.content = append(result.content, c.hash)
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
	slices.Sort(result.content)
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
