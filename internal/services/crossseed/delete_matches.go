// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/qbittorrent"
)

// FindDeleteMatches returns the torrents on the instance, other than the
// selected torrents, that a delete of the selected torrents with their files
// affects: the torrents that share a file path with a selected torrent and the
// linked copies of its files. It selects the candidates as BuildCrossMatchSets
// does, so one call covers a selection of any size, and it does not apply the
// title rescue. Only trusted identity counts for a link, because the delete
// dialog can add the matches to the delete. A file list or a link check that
// cannot be read fails the call, so the dialog does not report "no
// cross-seeds" in error.
func (s *Service) FindDeleteMatches(ctx context.Context, instanceID int, hashes []string) ([]LocalMatch, error) {
	torrents, err := s.syncManager.GetCachedInstanceTorrents(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("get torrents of instance %d: %w", instanceID, err)
	}
	instances, err := s.instanceStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}
	instance := instanceByID(instances, instanceID)
	if instance == nil {
		return nil, fmt.Errorf("instance %d: %w", instanceID, ErrInvalidRequest)
	}

	selected := selectedHashes(torrents, hashes)
	isSelected := func(hash string) bool {
		_, ok := selected[normalizeHash(hash)]
		return ok
	}

	reads := s.readLocalTorrents(ctx, instanceID, viewTorrents(torrents))
	idx := s.buildIndexFromViews(torrents, false, reads)
	if idx == nil {
		return nil, nil
	}
	picks := make([][]*candidatePick, len(torrents))
	for i := range torrents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if source := torrents[i].Torrent; isSelected(source.Hash) {
			picks[i] = s.selectCandidates(source, reads.read(s.matcher(), instanceID, source), idx, true)
		}
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
		trustedIdentityOnly: true,
	}
	files.prefetch(ctx, instanceID, torrents, pathGroups, picks)
	files.addSharedPathPicks(ctx, instanceID, torrents, pathGroups, picks, CrossMatchNeeds{})

	matches := deleteMatches{at: make(map[string]int)}
	for i := range torrents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		source := torrents[i].Torrent
		// A magnet without metadata has no files.
		if !isSelected(source.Hash) || len(picks[i]) == 0 || source.ContentPath == "" {
			continue
		}
		sourceData, ok := files.get(ctx, instanceID, source.Hash, source.SavePath)
		if !ok {
			return nil, fmt.Errorf("read the file list of torrent %s", normalizeHash(source.Hash))
		}
		for _, p := range picks[i] {
			c := p.entry
			if isSelected(c.hash) || c.view.ContentPath == "" || matches.shared(c.hash) {
				continue
			}
			tier, err := files.deleteTier(ctx, source, instanceID, sourceData, p)
			if err != nil {
				return nil, err
			}
			if tier == matchTypeContentPath || tier == matchTypeHardlink || tier == matchTypeReflink {
				matches.add(c.hash, newLocalMatch(instance, c.view, tier))
			}
		}
	}
	return matches.list, nil
}

// deleteTier returns the tier of a candidate of a selected torrent on the same
// instance. Unlike classifyCandidates, it checks the link also when the sizes
// pair: a linked copy survives the delete, and the dialog says so.
func (b *batchFiles) deleteTier(ctx context.Context, source *qbt.Torrent, instanceID int, sourceData batchFileData, p *candidatePick) (string, error) {
	c := p.entry
	candidateData, ok := b.get(ctx, instanceID, c.hash, c.savePath)
	if !ok {
		return "", fmt.Errorf("read the file list of torrent %s", c.hash)
	}
	var linkUnread bool
	tier := localMatchTier(true, p.nameOrRelease, sourceData.data, candidateData.data, func() string {
		linked := b.linkedTier(ctx, source, instanceID, c)
		linkUnread = linked.unread
		return linked.tier
	})
	if linkUnread {
		return "", fmt.Errorf("check the links of torrent %s", c.hash)
	}
	return tier, nil
}

// deleteMatches keeps one match for each torrent. A shared path outranks a
// link, because the delete removes the files of a torrent that shares a path.
type deleteMatches struct {
	list []LocalMatch
	at   map[string]int
}

// shared reports whether the torrent hash already has a shared-path match.
func (d *deleteMatches) shared(hash string) bool {
	at, ok := d.at[hash]
	return ok && d.list[at].MatchType == matchTypeContentPath
}

func (d *deleteMatches) add(hash string, m LocalMatch) {
	at, ok := d.at[hash]
	switch {
	case !ok:
		d.at[hash] = len(d.list)
		d.list = append(d.list, m)
	case m.MatchType == matchTypeContentPath:
		d.list[at].MatchType = m.MatchType
	}
}

// selectedHashes returns the Hash of each torrent that hashes names. A request
// can name a hybrid torrent by any of its hashes, as a bulk action can.
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
