// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"maps"
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// CopyGroup is one Copy group (GLOSSARY.md). ID is keyed, so it does not show
// the instance or hash of the Group representative (ADR 0041).
type CopyGroup struct {
	ID      uint64
	Members []qbittorrent.CopyGroupMember
}

// FindCopyGroups reads no store and no client: a torrent without a list in
// files stays out of every group.
func (s *Service) FindCopyGroups(
	ctx context.Context,
	torrents map[int][]qbittorrent.CrossInstanceTorrentView,
	files map[int]map[string]qbt.TorrentFiles,
	rules []models.CategoryMappingRule,
	key []byte,
) ([]CopyGroup, error) {
	instanceIDs := slices.Sorted(maps.Keys(torrents))
	var all []qbittorrent.CrossInstanceTorrentView
	for _, id := range instanceIDs {
		all = append(all, torrents[id]...)
	}

	held := &batchFiles{svc: s, byKey: make(map[string]*batchFileData), fetched: make(map[string]qbt.TorrentFiles, len(all))}
	index := make(map[string]int, len(all))
	reads := make(localReads)
	m := s.matcher()
	for i := range all {
		t := all[i].Torrent
		k := cacheKeyForTorrentFiles(all[i].InstanceID, t.Hash)
		index[k] = i
		list := files[all[i].InstanceID][normalizeHash(t.Hash)]
		held.fetched[k] = list
		if len(list) > 0 && nameNeedsFiles(m.readName(t.Name).release) {
			reads[k] = localReadEntry{name: t.Name, category: t.Category, read: s.readLocalFiles(rules, t.Name, t.Category, list), fromFiles: true}
		}
	}

	idx := s.buildIndexFromViews(all, false, reads)
	if idx == nil {
		return nil, nil
	}
	picks := s.selectAllCandidates(ctx, all, idx, false, reads)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The helper skips a pick whose hash it holds, so a pick of the same hash
	// on another instance must not block a shared-path pick.
	lo := 0
	for _, id := range instanceIDs {
		hi := lo + len(torrents[id])
		shared := make([][]*candidatePick, hi-lo)
		held.addSharedPathPicks(ctx, id, all[lo:hi], sharedPathGroups(all[lo:hi]), shared, CrossMatchNeeds{})
		for k := range shared {
			picks[lo+k] = append(picks[lo+k], shared[k]...)
		}
		lo = hi
	}

	roots, err := linkCopies(ctx, all, held, picks, index)
	if err != nil {
		return nil, err
	}
	return copyGroupsOf(all, roots, key), nil
}

func linkCopies(ctx context.Context, all []qbittorrent.CrossInstanceTorrentView, held *batchFiles, picks [][]*candidatePick, index map[string]int) ([]int, error) {
	parent := make([]int, len(all))
	for i := range parent {
		parent[i] = i
	}
	root := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	for i := range all {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		source := all[i]
		sourceData, ok := held.get(ctx, source.InstanceID, source.Hash, source.SavePath)
		if !ok {
			continue
		}
		for _, p := range picks[i] {
			candidateData, ok := held.get(ctx, p.entry.instanceID, p.entry.hash, p.entry.savePath)
			if !ok || !slices.Equal(sourceData.data.sizes, candidateData.data.sizes) ||
				localMatchTier(p.entry.instanceID == source.InstanceID, p.nameOrRelease, sourceData.data, candidateData.data, nil) == "" {
				continue
			}
			parent[root(index[cacheKeyForTorrentFiles(p.entry.instanceID, p.entry.hash)])] = root(i)
		}
	}
	for i := range parent {
		parent[i] = root(i)
	}
	return parent, nil
}

func copyGroupsOf(all []qbittorrent.CrossInstanceTorrentView, roots []int, key []byte) []CopyGroup {
	members := make(map[int][]int)
	for i, r := range roots {
		members[r] = append(members[r], i)
	}
	var groups []CopyGroup
	for i := range all {
		group := members[i]
		if len(group) < 2 {
			continue
		}
		rep := all[slices.MinFunc(group, func(a, b int) int {
			return qbittorrent.CompareRepresentative(all[a].InstanceID, all[a].Torrent, all[b].InstanceID, all[b].Torrent)
		})]
		copyGroup := CopyGroup{ID: copyGroupID(key, rep.InstanceID, rep.Hash)}
		for _, member := range group {
			copyGroup.Members = append(copyGroup.Members, qbittorrent.CopyGroupMember{InstanceID: all[member].InstanceID, Hash: normalizeHash(all[member].Hash)})
		}
		groups = append(groups, copyGroup)
	}
	return groups
}

func copyGroupID(key []byte, instanceID int, hash string) uint64 {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(cacheKeyForTorrentFiles(instanceID, hash)))
	return binary.BigEndian.Uint64(mac.Sum(nil))
}
