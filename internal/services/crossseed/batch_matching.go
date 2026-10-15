// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"maps"
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
}

// candidateEntry holds pre-computed data for a candidate torrent.
type candidateEntry struct {
	hash       string
	tags       string
	seeding    bool
	instanceID int
	savePath   string
	// ambiguous is true when the content path equals the save path: a
	// multi-file torrent without a subfolder.
	ambiguous bool
	view      *qbittorrent.CrossInstanceTorrentView
}

// releaseCandidate extends candidateEntry with parsed release metadata for rls matching.
type releaseCandidate struct {
	candidateEntry
	name    string
	release *rls.Release
}

// matchIndex holds pre-built indexes that select the candidates of a source
// torrent before their file lists are fetched.
type matchIndex struct {
	byContentPath map[string][]candidateEntry       // key: normalizedContentPath (non-ambiguous only)
	bySavePath    map[string][]candidateEntry       // key: normalized save path
	byName        map[string][]candidateEntry       // key: normalizeLowerTrim(name)
	byReleaseKey  map[releaseKey][]releaseCandidate // for release metadata matching
}

// BuildCrossMatchSets builds sets of torrent hashes that have cross-seeds on the
// same instance and/or other instances. It uses the same per-file rule as the
// Cross-Seed tab (localMatchTier), without the retitled rescue. The cached
// torrent fields select the candidates, and only their file lists are fetched,
// through the files cache. The Hardlink and Reflink check runs only for a Name
// or Release candidate whose file sizes do not pair, because only that
// candidate can change the result.
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
		linkTiers: make(map[[2]string]string),
	}

	// Select the candidates of every torrent first, so that the file lists they
	// need are read in one batch per instance.
	var samePicks, otherPicks [][]*candidatePick
	if needsSame {
		if sameIdx := s.buildIndexFromViews(currentTorrents, needs.SameSeeding); sameIdx != nil {
			samePicks = s.selectAllCandidates(ctx, currentTorrents, sameIdx, true)
		}
	}
	if needsOther && listErr == nil {
		if otherIdx := s.buildOtherInstanceIndex(ctx, instances, currentInstanceID, needs.OtherSeeding); otherIdx != nil {
			otherPicks = s.selectAllCandidates(ctx, currentTorrents, otherIdx, false)
		}
	}
	files.prefetch(ctx, currentInstanceID, currentTorrents, samePicks, otherPicks)

	if samePicks != nil {
		result.SameInstanceExists = make(map[string]struct{})
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
		if needs.OtherSeeding {
			result.OtherInstanceSeeding = make(map[string]struct{})
		}
		for i := range currentTorrents {
			if ctx.Err() != nil {
				break
			}
			source := currentTorrents[i].Torrent
			if m := s.classifyCandidates(ctx, source, currentInstanceID, otherPicks[i], false, false, files); m.matched {
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
	linkTiers map[[2]string]string
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

// prefetch reads the file lists of the sources with candidates and of their
// candidates, in one GetTorrentFilesBatch call per instance. The sync manager
// then fetches the lists that it has no copy of in parallel, not one after
// another. A list that the call does not return is read again by get.
func (b *batchFiles) prefetch(ctx context.Context, sourceInstanceID int, sources []qbittorrent.CrossInstanceTorrentView, picksBySource ...[][]*candidatePick) {
	need := make(map[int]map[string]struct{})
	add := func(instanceID int, hash string) {
		if need[instanceID] == nil {
			need[instanceID] = make(map[string]struct{})
		}
		need[instanceID][hash] = struct{}{}
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

// linkedTier returns matchTypeHardlink or matchTypeReflink when the candidate
// files are linked copies of the source files, as the Cross-Seed tab checks
// them. A stat failure counts as no link.
func (b *batchFiles) linkedTier(ctx context.Context, source *qbt.Torrent, sourceInstanceID int, c candidateEntry) string {
	candidateInstance := instanceByID(b.instances, c.instanceID)
	// Check the cheap gates here, so that a source without file identity
	// never builds a context.
	if candidateInstance == nil || !models.FilesystemCapabilitiesOf(candidateInstance).Identity ||
		!instanceHasFileIdentity(b.instances, sourceInstanceID) {
		return ""
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
	b.linkTiers[pair] = tier
	return tier
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
		byContentPath: make(map[string][]candidateEntry),
		bySavePath:    make(map[string][]candidateEntry),
		byName:        make(map[string][]candidateEntry),
		byReleaseKey:  make(map[releaseKey][]releaseCandidate),
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
	contentPath := normalizePathForComparison(view.ContentPath)
	savePath := normalizePathForComparison(view.SavePath)
	entry := candidateEntry{
		hash:       normalizeHash(view.Hash),
		tags:       view.Tags,
		seeding:    needsSeeding && isTorrentViewSeeding(view),
		instanceID: view.InstanceID,
		savePath:   view.SavePath,
		ambiguous:  contentPath != "" && contentPath == savePath,
		view:       view,
	}

	if contentPath != "" && !entry.ambiguous {
		idx.byContentPath[contentPath] = append(idx.byContentPath[contentPath], entry)
	}
	if savePath != "" {
		idx.bySavePath[savePath] = append(idx.bySavePath[savePath], entry)
	}

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
	// content holds the hashes of the matches in the Content tier, and of the
	// same-instance candidates whose files could not be read. A delete or move
	// must treat an unread candidate as one that shares files.
	content []string
}

// candidatePick is a candidate that passed the prefilter. nameOrRelease is the
// tier it gets if its file sizes pair with the source.
type candidatePick struct {
	entry         candidateEntry
	nameOrRelease string
}

// selectCandidates selects the candidates of a source torrent from the index
// with the same prefilter as the Cross-Seed tab. sameInstance excludes the
// source itself.
func (s *Service) selectCandidates(source *qbt.Torrent, idx *matchIndex, sameInstance bool) []*candidatePick {
	sourceHash := normalizeHash(source.Hash)

	var picks []*candidatePick
	byKey := make(map[string]*candidatePick)
	add := func(c candidateEntry, nameOrRelease string) {
		if sameInstance && c.hash == sourceHash {
			return
		}
		key := cacheKeyForTorrentFiles(c.instanceID, c.hash)
		if p, ok := byKey[key]; ok {
			if p.nameOrRelease == "" {
				p.nameOrRelease = nameOrRelease
			}
			return
		}
		p := &candidatePick{entry: c, nameOrRelease: nameOrRelease}
		byKey[key] = p
		picks = append(picks, p)
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

	contentPath := normalizePathForComparison(source.ContentPath)
	savePath := normalizePathForComparison(source.SavePath)
	sourceAmbiguous := contentPath != "" && contentPath == savePath
	if contentPath != "" && !sourceAmbiguous {
		for _, c := range idx.byContentPath[contentPath] {
			add(c, "")
		}
	}
	if savePath != "" {
		for _, c := range idx.bySavePath[savePath] {
			if sourceAmbiguous || c.ambiguous {
				add(c, "")
			}
		}
	}
	return picks
}

// classifyCandidates classifies each candidate of a source torrent with
// localMatchTier. sameInstance allows the Content tier. A candidate whose file
// list cannot be read is not a match, but on the same instance it goes into
// content.
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
			if sameInstance {
				result.content = append(result.content, c.hash)
			}
			continue
		}
		tier := localMatchTier(sameInstance, p.nameOrRelease, sourceData, candidateData, nil)
		if tier == "" && p.nameOrRelease != "" {
			tier = files.linkedTier(ctx, source, sourceInstanceID, c)
		}
		if tier == "" {
			continue
		}
		result.matched = true
		if c.seeding {
			result.seeding = true
		}
		if tier == matchTypeContentPath {
			result.content = append(result.content, c.hash)
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
