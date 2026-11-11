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

// CopyGroupFiles returns the file lists that it can read of torrents on one
// instance, by normalized hash.
type CopyGroupFiles func(ctx context.Context, instanceID int, torrents []qbittorrent.CrossInstanceTorrentView) (map[string]qbt.TorrentFiles, error)

// FindCopyGroups asks read for the file lists of the torrents that can have a
// copy, and for no other list. A torrent without a list from read stays out of
// every group. An error of read ends the call.
func (s *Service) FindCopyGroups(
	ctx context.Context,
	torrents map[int][]qbittorrent.CrossInstanceTorrentView,
	read CopyGroupFiles,
	rules []models.CategoryMappingRule,
	key []byte,
) ([]CopyGroup, error) {
	lists := newCopyGroupLists(s, torrents, read)
	all := lists.all
	index := make(map[string]int, len(all))
	needsFiles := make([]bool, len(all))
	want := make([]bool, len(all))
	m := s.matcher()
	for i := range all {
		index[cacheKeyForTorrentFiles(all[i].InstanceID, all[i].Hash)] = i
		needsFiles[i] = nameNeedsFiles(m.readName(all[i].Name).release)
		want[i] = needsFiles[i]
	}
	pathGroups := make([][][]int, len(lists.instances))
	for n, instance := range lists.instances {
		pathGroups[n] = sharedPathGroups(all[instance.lo:instance.hi])
		for _, group := range pathGroups[n] {
			for _, i := range group {
				want[instance.lo+i] = true
			}
		}
	}
	if err := lists.fetch(ctx, want); err != nil {
		return nil, err
	}

	reads := make(localReads)
	for i := range all {
		t := all[i].Torrent
		k := cacheKeyForTorrentFiles(all[i].InstanceID, t.Hash)
		if list := lists.held.fetched[k]; needsFiles[i] && len(list) > 0 {
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
	for n, instance := range lists.instances {
		shared := make([][]*candidatePick, instance.hi-instance.lo)
		lists.held.addSharedPathPicks(ctx, instance.id, all[instance.lo:instance.hi], pathGroups[n], shared, CrossMatchNeeds{})
		for k := range shared {
			picks[instance.lo+k] = append(picks[instance.lo+k], shared[k]...)
		}
	}

	clear(want)
	for i := range picks {
		picks[i] = slices.DeleteFunc(picks[i], func(p *candidatePick) bool {
			return index[cacheKeyForTorrentFiles(p.entry.instanceID, p.entry.hash)] == i
		})
		for _, p := range picks[i] {
			want[i], want[index[cacheKeyForTorrentFiles(p.entry.instanceID, p.entry.hash)]] = true, true
		}
	}
	if err := lists.fetch(ctx, want); err != nil {
		return nil, err
	}

	roots, err := linkCopies(ctx, all, lists.held, picks, index)
	if err != nil {
		return nil, err
	}
	return copyGroupsOf(all, roots, key), nil
}

// copyGroupLists holds the file lists that one FindCopyGroups call asked for.
type copyGroupLists struct {
	all       []qbittorrent.CrossInstanceTorrentView
	instances []copyGroupInstance
	held      *batchFiles
	read      CopyGroupFiles
	asked     []bool
}

// copyGroupInstance is the range of one instance in copyGroupLists.all.
type copyGroupInstance struct {
	id, lo, hi int
}

func newCopyGroupLists(s *Service, torrents map[int][]qbittorrent.CrossInstanceTorrentView, read CopyGroupFiles) *copyGroupLists {
	l := &copyGroupLists{read: read}
	for _, id := range slices.Sorted(maps.Keys(torrents)) {
		l.instances = append(l.instances, copyGroupInstance{id: id, lo: len(l.all), hi: len(l.all) + len(torrents[id])})
		l.all = append(l.all, torrents[id]...)
	}
	l.asked = make([]bool, len(l.all))
	l.held = &batchFiles{svc: s, byKey: make(map[string]*batchFileData), fetched: make(map[string]qbt.TorrentFiles, len(l.all))}
	// An entry without a list keeps batchFiles.get away from the client.
	for i := range l.all {
		l.held.fetched[cacheKeyForTorrentFiles(l.all[i].InstanceID, l.all[i].Hash)] = nil
	}
	return l
}

// fetch asks read one time for each torrent of want.
func (l *copyGroupLists) fetch(ctx context.Context, want []bool) error {
	for _, instance := range l.instances {
		var torrents []qbittorrent.CrossInstanceTorrentView
		for i := instance.lo; i < instance.hi; i++ {
			if want[i] && !l.asked[i] {
				l.asked[i] = true
				torrents = append(torrents, l.all[i])
			}
		}
		if len(torrents) == 0 {
			continue
		}
		files, err := l.read(ctx, instance.id, torrents)
		if err != nil {
			return err
		}
		for _, t := range torrents {
			l.held.fetched[cacheKeyForTorrentFiles(instance.id, t.Hash)] = files[normalizeHash(t.Hash)]
		}
	}
	return nil
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
		if len(picks[i]) == 0 {
			continue
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
