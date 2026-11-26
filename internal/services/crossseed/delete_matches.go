// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"syscall"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

func (s *Service) FindDeleteMatches(ctx context.Context, selection map[int][]string) (*DeleteMatchesResponse, error) {
	requested := make(map[int]map[string]struct{}, len(selection))
	for instanceID, hashes := range selection {
		requested[instanceID] = make(map[string]struct{}, len(hashes))
		for _, hash := range hashes {
			requested[instanceID][normalizeHash(hash)] = struct{}{}
		}
	}
	matches := deleteMatches{at: make(map[string]int)}
	skip := func(c candidateEntry) bool {
		sel := requested[c.instanceID]
		return len(sel) > 0 && slices.ContainsFunc(normalizedHashes(c.view.Hash, c.view.InfohashV1, c.view.InfohashV2), func(h string) bool {
			_, ok := sel[h]
			return ok
		}) || matches.shared(c.instanceID, c.hash)
	}
	response := &DeleteMatchesResponse{Matches: []LocalMatch{}, Unchecked: []UncheckedInstance{}}
	for _, instanceID := range slices.Sorted(maps.Keys(selection)) {
		unchecked, err := s.findSelectedMatches(ctx, instanceID, selection[instanceID], true, skip, func(m LocalMatch) {
			if m.MatchType == matchTypeContentPath || m.MatchType == matchTypeHardlink || m.MatchType == matchTypeReflink {
				matches.add(m)
			}
		})
		if errors.Is(err, ErrTorrentNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, u := range unchecked {
			if !slices.ContainsFunc(response.Unchecked, func(have UncheckedInstance) bool { return have.InstanceID == u.InstanceID }) {
				response.Unchecked = append(response.Unchecked, u)
			}
		}
	}
	response.Matches = append(response.Matches, matches.list...)
	return response, nil
}

func (s *Service) FindCrossSeeds(ctx context.Context, instanceID int, hash string) (*LocalMatchesResponse, error) {
	var matches []LocalMatch
	if _, err := s.findSelectedMatches(ctx, instanceID, []string{hash}, false, nil, func(m LocalMatch) {
		matches = append(matches, m)
	}); err != nil {
		return nil, err
	}
	return &LocalMatchesResponse{Matches: matches}, nil
}

func (s *Service) findSelectedMatches(
	ctx context.Context,
	instanceID int,
	hashes []string,
	sharedOnly bool,
	skip func(candidateEntry) bool,
	match func(LocalMatch),
) (unchecked []UncheckedInstance, err error) {
	torrents, err := s.syncManager.GetCachedInstanceTorrents(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("get torrents of instance %d: %w", instanceID, err)
	}
	instances, err := s.instanceStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}
	if instanceByID(instances, instanceID) == nil {
		return nil, fmt.Errorf("instance %d: %w", instanceID, ErrInvalidRequest)
	}

	selected := selectedHashes(torrents, hashes)
	if len(selected) == 0 {
		return nil, ErrTorrentNotFound
	}
	isSelected := func(hash string) bool {
		_, ok := selected[normalizeHash(hash)]
		return ok
	}

	reads := s.readLocalTorrents(ctx, instanceID, viewTorrents(torrents))
	idx := s.buildIndexFromViews(torrents, false, reads)
	sourceTrusted := instanceHas(instances, instanceID, models.CapabilityTrustedIdentity)
	var otherIdx *matchIndex
	var others []qbittorrent.CrossInstanceTorrentView
	switch {
	case !sharedOnly:
		otherIdx, others, _ = s.buildOtherInstanceIndex(ctx, instances, instanceID, false, reads)
	case sourceTrusted:
		otherIdx, others, unchecked = s.buildSharedFileIndex(ctx, instances, instanceID, reads)
	}
	picks := make([][]*candidatePick, len(torrents))
	otherPicks := make([][]*candidatePick, len(torrents))
	for i := range torrents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		source := torrents[i].Torrent
		if !isSelected(source.Hash) {
			continue
		}
		read := reads.read(s.matcher(), instanceID, source)
		picks[i] = s.selectCandidates(source, read, idx, true)
		if otherIdx != nil {
			otherPicks[i] = s.selectCandidates(source, read, otherIdx, false)
			if sourceTrusted {
				otherPicks[i] = addSizePicks(instances, otherPicks[i], otherIdx.bySize[searchSourceSize(source)])
			}
		}
	}
	var groups []crossPathGroup
	if sourceTrusted {
		groups = crossPathGroups(instances, torrents, others, isSelected)
	}
	pathGroups := slices.DeleteFunc(sharedPathGroups(torrents), func(group []int) bool {
		return !slices.ContainsFunc(group, func(i int) bool { return isSelected(torrents[i].Hash) })
	})

	files := &batchFiles{
		svc:                 s,
		instances:           instances,
		byKey:               make(map[string]*batchFileData),
		fetched:             make(map[string]qbt.TorrentFiles),
		links:               make(map[string]*localMatchContext),
		linkTiers:           make(map[[2]string]localLinkResult),
		folders:             make(map[string]fsops.FileKey),
		trustedIdentityOnly: true,
		otherSharedOnly:     sharedOnly,
	}
	groupSources, groupPicks := prefetchGroups(len(torrents), groups)
	files.prefetch(ctx, instanceID, torrents, slices.Concat(pathGroups, groupSources), picks, otherPicks, groupPicks)
	files.addSharedPathPicks(ctx, instanceID, torrents, pathGroups, picks, CrossMatchNeeds{})
	files.addOtherPathPicks(ctx, instanceID, torrents, groups, otherPicks)

	skipCandidate := func(c candidateEntry) bool {
		return (c.instanceID == instanceID && isSelected(c.hash)) || (skip != nil && skip(c))
	}
	for i := range torrents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if isSelected(torrents[i].Hash) {
			if err := files.matchSource(ctx, torrents[i].Torrent, instanceID, slices.Concat(picks[i], otherPicks[i]), skipCandidate, match); err != nil {
				return nil, err
			}
		}
	}
	return unchecked, nil
}

func (s *Service) buildSharedFileIndex(
	ctx context.Context,
	instances []*models.Instance,
	instanceID int,
	reads localReads,
) (*matchIndex, []qbittorrent.CrossInstanceTorrentView, []UncheckedInstance) {
	trusted := slices.DeleteFunc(slices.Clone(instances), func(inst *models.Instance) bool {
		return !models.FilesystemCapabilitiesOf(inst).Has(models.CapabilityTrustedIdentity)
	})
	idx, views, offline := s.buildOtherInstanceIndex(ctx, trusted, instanceID, false, reads)
	var unchecked []UncheckedInstance
	for _, inst := range offline {
		unchecked = append(unchecked, UncheckedInstance{InstanceID: inst.ID, InstanceName: inst.Name, Reason: UncheckedOffline})
	}
	for _, inst := range trusted {
		if !inst.IsActive && inst.ID != instanceID {
			unchecked = append(unchecked, UncheckedInstance{InstanceID: inst.ID, InstanceName: inst.Name, Reason: UncheckedDisabled})
		}
	}
	return idx, views, unchecked
}

func (b *batchFiles) addOtherPathPicks(
	ctx context.Context,
	instanceID int,
	torrents []qbittorrent.CrossInstanceTorrentView,
	groups []crossPathGroup,
	picks [][]*candidatePick,
) {
	picked := make(map[int]map[string]struct{})
	add := func(i int, p *candidatePick) {
		have := picked[i]
		if have == nil {
			have = make(map[string]struct{}, len(picks[i]))
			for _, q := range picks[i] {
				have[cacheKeyForTorrentFiles(q.entry.instanceID, q.entry.hash)] = struct{}{}
			}
			picked[i] = have
		}
		key := cacheKeyForTorrentFiles(p.entry.instanceID, p.entry.hash)
		if _, ok := have[key]; !ok {
			have[key] = struct{}{}
			picks[i] = append(picks[i], p)
		}
	}
	for _, group := range groups {
		byPath := make(map[string][]*candidatePick)
		for _, p := range group.others {
			data, ok := b.get(ctx, p.entry.instanceID, p.entry.hash, p.entry.savePath)
			if !ok {
				add(group.sources[0], p)
			}
			for path := range data.data.paths {
				byPath[path] = append(byPath[path], p)
			}
		}
		for _, i := range group.sources {
			data, ok := b.get(ctx, instanceID, torrents[i].Hash, torrents[i].SavePath)
			if !ok {
				add(i, group.others[0])
			}
			for path := range data.data.paths {
				for _, p := range byPath[path] {
					add(i, p)
				}
			}
		}
	}
}

func addSizePicks(instances []*models.Instance, picks []*candidatePick, sizeGroup []*releaseCandidate) []*candidatePick {
	for _, c := range sizeGroup {
		if !instanceHas(instances, c.instanceID, models.CapabilityTrustedIdentity) ||
			slices.ContainsFunc(picks, func(p *candidatePick) bool { return p.entry.instanceID == c.instanceID && p.entry.hash == c.hash }) {
			continue
		}
		picks = append(picks, &candidatePick{entry: c.candidateEntry})
	}
	return picks
}

func selectedAndTrustedOthers(
	instances []*models.Instance,
	torrents, others []qbittorrent.CrossInstanceTorrentView,
	isSelected func(hash string) bool,
) (all []qbittorrent.CrossInstanceTorrentView, sources []int) {
	all = make([]qbittorrent.CrossInstanceTorrentView, 0, len(others)+1)
	for i := range torrents {
		if isSelected(torrents[i].Hash) {
			all = append(all, torrents[i])
			sources = append(sources, i)
		}
	}
	for i := range others {
		if instanceHas(instances, others[i].InstanceID, models.CapabilityTrustedIdentity) {
			all = append(all, others[i])
		}
	}
	return all, sources
}

func (b *batchFiles) matchSource(
	ctx context.Context,
	source *qbt.Torrent,
	instanceID int,
	picks []*candidatePick,
	skip func(candidateEntry) bool,
	match func(LocalMatch),
) error {
	if len(picks) == 0 || source.ContentPath == "" {
		return nil
	}
	sourceData, ok := b.get(ctx, instanceID, source.Hash, source.SavePath)
	if !ok {
		return fmt.Errorf("read the file list of torrent %s", normalizeHash(source.Hash))
	}
	for _, p := range picks {
		c := p.entry
		if c.view.ContentPath == "" || skip(c) {
			continue
		}
		tier, err := b.strictTier(ctx, source, instanceID, sourceData, p)
		if err != nil {
			return err
		}
		if tier != "" {
			match(newLocalMatch(instanceByID(b.instances, c.instanceID), c.view, tier))
		}
	}
	return nil
}

func (b *batchFiles) strictTier(ctx context.Context, source *qbt.Torrent, sourceInstanceID int, sourceData batchFileData, p *candidatePick) (string, error) {
	c := p.entry
	candidateData, ok := b.get(ctx, c.instanceID, c.hash, c.savePath)
	if !ok {
		return "", fmt.Errorf("read the file list of torrent %s", c.hash)
	}
	if c.instanceID != sourceInstanceID {
		shared, err := b.sharedFile(ctx, sourceInstanceID, source, sourceData.files, c, candidateData.files)
		if err != nil {
			return "", fmt.Errorf("check the shared files of torrent %s: %w", c.hash, err)
		}
		if shared {
			return matchTypeContentPath, nil
		}
		if b.otherSharedOnly {
			return "", nil
		}
	}
	var linkErr error
	tier := localMatchTier(c.instanceID == sourceInstanceID, p.nameOrRelease, sourceData.data, candidateData.data, func() string {
		linked := b.linkedTier(ctx, source, sourceInstanceID, c)
		linkErr = linked.err
		return linked.tier
	})
	if linkErr != nil {
		return "", fmt.Errorf("check the links of torrent %s: %w", c.hash, linkErr)
	}
	return tier, nil
}

func (b *batchFiles) sharedFile(ctx context.Context, sourceInstanceID int, source *qbt.Torrent, sourceFiles qbt.TorrentFiles, c candidateEntry, candidateFiles qbt.TorrentFiles) (bool, error) {
	if !instanceHas(b.instances, sourceInstanceID, models.CapabilityTrustedIdentity) ||
		!instanceHas(b.instances, c.instanceID, models.CapabilityTrustedIdentity) {
		return false, nil
	}
	normalizer := normalizerForService(b.svc)
	sourceFolders, err := b.folderEntries(ctx, sourceInstanceID, source.SavePath, contentFiles(sourceFiles, normalizer))
	if err != nil {
		return false, err
	}
	candidateFolders, err := b.folderEntries(ctx, c.instanceID, c.savePath, contentFiles(candidateFiles, normalizer))
	if err != nil {
		return false, err
	}
	for name, folders := range candidateFolders {
		for _, folder := range folders {
			if slices.Contains(sourceFolders[name], folder) {
				return true, nil
			}
		}
	}
	return false, nil
}

func (b *batchFiles) folderEntries(ctx context.Context, instanceID int, savePath string, files qbt.TorrentFiles) (map[string][]fsops.FileKey, error) {
	backend, err := b.svc.getBackendForInstance(ctx, instanceID, models.CapabilityTrustedIdentity)
	if err != nil {
		return nil, fmt.Errorf("resolve filesystem backend for instance %d: %w", instanceID, err)
	}
	d := backend.Paths()
	base := d.Clean(d.FromSlash(savePath))
	if !d.IsAbs(base) {
		return nil, nil
	}
	instance := instanceByID(b.instances, instanceID)
	folders := make(map[string][]fsops.FileKey)
	for _, file := range files {
		fullPath, ok := resolveLocalTorrentFile(d, base, file.Name)
		if !ok {
			continue
		}
		dir := d.Dir(fullPath)
		folder, ok := b.folders[dir]
		if !ok {
			info, err := backend.Stat(ctx, dir)
			switch {
			case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR):
			case err != nil:
				return nil, err
			case info.FileIDErr != nil:
				return nil, fmt.Errorf("read the file identity of %s: %w", dir, info.FileIDErr)
			default:
				folder, _ = fsops.FileKeyOf(info.FileID, instance)
			}
			b.folders[dir] = folder
		}
		if folder != (fsops.FileKey{}) {
			name := d.Base(fullPath)
			folders[name] = append(folders[name], folder)
		}
	}
	return folders, nil
}

type deleteMatches struct {
	list []LocalMatch
	at   map[string]int
}

func (d *deleteMatches) shared(instanceID int, hash string) bool {
	at, ok := d.at[cacheKeyForTorrentFiles(instanceID, hash)]
	return ok && d.list[at].MatchType == matchTypeContentPath
}

func (d *deleteMatches) add(m LocalMatch) {
	key := cacheKeyForTorrentFiles(m.InstanceID, m.Hash)
	at, ok := d.at[key]
	switch {
	case !ok:
		d.at[key] = len(d.list)
		d.list = append(d.list, m)
	case m.MatchType == matchTypeContentPath:
		d.list[at].MatchType = m.MatchType
	}
}

func selectedHashes(torrents []qbittorrent.CrossInstanceTorrentView, hashes []string) map[string]struct{} {
	requested := make(map[string]struct{}, len(hashes))
	for _, hash := range hashes {
		requested[normalizeHash(hash)] = struct{}{}
	}
	selected := make(map[string]struct{}, len(hashes))
	for i := range torrents {
		t := torrents[i].Torrent
		if slices.ContainsFunc(normalizedHashes(t.Hash, t.InfohashV1, t.InfohashV2), func(h string) bool {
			_, ok := requested[h]
			return ok
		}) {
			selected[normalizeHash(t.Hash)] = struct{}{}
		}
	}
	return selected
}
