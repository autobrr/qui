// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

type CopyGroups struct {
	ids     map[int]map[string]uint64
	read    map[int]struct{}
	parts   map[int]map[string][]SharedPart
	partIDs uint64
	sizes   map[int]map[string]uniqueSizeEntry
}

type uniqueSizeEntry struct {
	group             uint64
	grouped, hasParts bool
	parts             []SharedPart
}

type SharedPart struct {
	ID   uint64
	Size int64
}

func NewCopyGroups(ids map[int]map[string]uint64, parts map[int]map[string][]SharedPart, read []int) *CopyGroups {
	record := make(map[int]struct{}, len(read))
	for _, id := range read {
		record[id] = struct{}{}
	}
	g := &CopyGroups{ids: ids, parts: parts, read: record, sizes: make(map[int]map[string]uniqueSizeEntry, len(ids))}
	for instanceID, byHash := range ids {
		g.sizes[instanceID] = make(map[string]uniqueSizeEntry, len(byHash)+len(parts[instanceID]))
		for hash, group := range byHash {
			g.sizes[instanceID][hash] = uniqueSizeEntry{group: group, grouped: true}
		}
	}
	for instanceID, byHash := range parts {
		if g.sizes[instanceID] == nil {
			g.sizes[instanceID] = make(map[string]uniqueSizeEntry, len(byHash))
		}
		for hash, list := range byHash {
			g.sizes[instanceID][hash] = uniqueSizeEntry{hasParts: true, parts: list}
			for _, part := range list {
				g.partIDs = max(g.partIDs, part.ID+1)
			}
		}
	}
	return g
}

func (g *CopyGroups) hasRead(instanceID int) bool {
	if g == nil {
		return false
	}
	_, ok := g.read[instanceID]
	return ok
}

func (g *CopyGroups) GroupID(instanceID int, hash string) (uint64, bool) {
	if !g.hasRead(instanceID) {
		return 0, false
	}
	id, ok := g.ids[instanceID][hash]
	return id, ok
}

type uniqueSizes struct {
	entries                 map[string]uniqueSizeEntry
	counted, countedSeeding []bool
	onPath                  []partsOnPath
	parent                  []int32
	byGroup                 map[uint64]int32
	byPath                  map[string]int32
	members                 []uniqueSizeMember
	total, seeding          int64
}

type partsOnPath struct {
	path  string
	parts []SharedPart
}

type uniqueSizeMember struct {
	set     int32
	size    int64
	seeding bool
}

func (g *CopyGroups) uniqueSizes(instanceID int) *uniqueSizes {
	if !g.hasRead(instanceID) {
		return nil
	}
	u := &uniqueSizes{entries: g.sizes[instanceID], byGroup: map[uint64]int32{}, byPath: map[string]int32{}}
	if len(g.parts[instanceID]) > 0 {
		u.counted, u.countedSeeding = make([]bool, g.partIDs), make([]bool, g.partIDs)
	}
	return u
}

func (u *uniqueSizes) add(t *qbt.Torrent, sharedPath, seeding bool) {
	if u == nil {
		return
	}
	sharedPath = sharedPath && t.ContentPath != ""
	entry := u.entries[t.Hash]
	if entry.hasParts {
		u.count(entry.parts, seeding)
		if sharedPath {
			u.onPath = append(u.onPath, partsOnPath{path: t.ContentPath, parts: entry.parts})
		}
		return
	}
	if !entry.grouped && !sharedPath {
		u.total += t.Size
		if seeding {
			u.seeding += t.Size
		}
		return
	}
	set := int32(-1)
	if entry.grouped {
		set = u.set(u.byGroup, entry.group)
	}
	if sharedPath {
		path := u.set(u.byPath, t.ContentPath)
		if set >= 0 {
			u.parent[u.root(set)] = u.root(path)
		}
		set = path
	}
	u.members = append(u.members, uniqueSizeMember{set: set, size: t.Size, seeding: seeding})
}

func (u *uniqueSizes) count(parts []SharedPart, seeding bool) {
	for _, part := range parts {
		u.total += countOnce(u.counted, part)
		if seeding {
			u.seeding += countOnce(u.countedSeeding, part)
		}
	}
}

func countOnce(counted []bool, part SharedPart) int64 {
	if counted[part.ID] {
		return 0
	}
	counted[part.ID] = true
	return part.Size
}

func (u *uniqueSizes) set[K comparable](sets map[K]int32, key K) int32 {
	set, ok := sets[key]
	if !ok {
		set = int32(len(u.parent))
		u.parent = append(u.parent, set)
		sets[key] = set
	}
	return set
}

func (u *uniqueSizes) root(set int32) int32 {
	for u.parent[set] != set {
		u.parent[set] = u.parent[u.parent[set]]
		set = u.parent[set]
	}
	return set
}

func (u *uniqueSizes) totals() (total, seeding int64) {
	all := make([]int64, len(u.parent))
	seeds := make([]int64, len(u.parent))
	for _, m := range u.members {
		set := u.root(m.set)
		all[set] = max(all[set], m.size)
		if m.seeding {
			seeds[set] = max(seeds[set], m.size)
		}
	}
	paths := u.partsOnPaths()
	for set, parts := range paths {
		if seeds[set] >= partsSize(parts, nil) {
			u.count(parts, true)
		}
	}
	for set, parts := range paths {
		all[set] -= partsSize(parts, nil)
		seeds[set] -= partsSize(parts, u.countedSeeding)
	}
	total, seeding = u.total, u.seeding
	for set := range all {
		total += max(0, all[set])
		seeding += max(0, seeds[set])
	}
	return total, seeding
}

func (u *uniqueSizes) partsOnPaths() map[int32][]SharedPart {
	var paths map[int32][]SharedPart
	for _, holder := range u.onPath {
		set, ok := u.byPath[holder.path]
		if !ok {
			continue
		}
		if paths == nil {
			paths = map[int32][]SharedPart{}
		}
		set = u.root(set)
		for _, part := range holder.parts {
			if !slices.Contains(paths[set], part) {
				paths[set] = append(paths[set], part)
			}
		}
	}
	return paths
}

func partsSize(parts []SharedPart, marks []bool) int64 {
	var size int64
	for _, part := range parts {
		if marks == nil || marks[part.ID] {
			size += part.Size
		}
	}
	return size
}

type CopyGroupRow struct {
	ID          string           `json:"id"`
	FirstSorted *CopyGroupMember `json:"first_sorted,omitempty"`
}

type CopyGroupMember struct {
	InstanceID int    `json:"instance_id"`
	Hash       string `json:"hash"`
}

func CompareRepresentative(aInstanceID int, a *qbt.Torrent, bInstanceID int, b *qbt.Torrent) int {
	return cmp.Or(
		cmp.Compare(a.AddedOn, b.AddedOn),
		strings.Compare(a.Hash, b.Hash),
		cmp.Compare(aInstanceID, bInstanceID),
	)
}

func (sm *SyncManager) SetCopyGroups(next *CopyGroups) {
	prev := sm.copyGroups.Swap(next)
	for _, instanceID := range changedCopyGroupInstances(prev, next) {
		sm.notifyTrackerHealthUpdated(instanceID)
	}
}

func (sm *SyncManager) CopyGroups() *CopyGroups {
	return sm.copyGroups.Load()
}

func (sm *SyncManager) LastSuccessfulSync(ctx context.Context, instanceID int) time.Time {
	client, err := sm.clientPool.GetClientOffline(ctx, instanceID)
	if err != nil {
		return time.Time{}
	}
	return client.GetLastSyncUpdate()
}

func changedCopyGroupInstances(prev, next *CopyGroups) []int {
	instances := map[int]struct{}{}
	for _, g := range []*CopyGroups{prev, next} {
		if g == nil {
			continue
		}
		for id := range g.ids {
			instances[id] = struct{}{}
		}
		for id := range g.read {
			instances[id] = struct{}{}
		}
		for id := range g.parts {
			instances[id] = struct{}{}
		}
	}
	var changed []int
	for _, id := range slices.Sorted(maps.Keys(instances)) {
		if prev.hasRead(id) != next.hasRead(id) || !maps.Equal(prev.entries(id), next.entries(id)) ||
			!maps.EqualFunc(prev.sharedParts(id), next.sharedParts(id), slices.Equal) {
			changed = append(changed, id)
		}
	}
	return changed
}

func (g *CopyGroups) entries(instanceID int) map[string]uint64 {
	if g == nil {
		return nil
	}
	return g.ids[instanceID]
}

func (g *CopyGroups) sharedParts(instanceID int) map[string][]SharedPart {
	if g == nil {
		return nil
	}
	return g.parts[instanceID]
}

func (sm *SyncManager) copyGroupsReady(ctx context.Context, groups *CopyGroups, instanceIDs []int) bool {
	if groups == nil {
		return false
	}
	synced := false
	for _, id := range instanceIDs {
		if groups.hasRead(id) {
			return true
		}
		if !sm.LastSuccessfulSync(ctx, id).IsZero() {
			synced = true
		}
	}
	return !synced
}

type copyGroupLayout struct {
	order  []int
	starts []int
	ids    []uint64
	first  []int
}

func collapseCopyGroups(n int, groupID func(i int) (uint64, bool), compare func(a, b int) int) *copyGroupLayout {
	l := &copyGroupLayout{}
	slotOf := map[uint64]int{}
	rowSlot := make([]int, n)
	var counts []int
	for i := range n {
		id, ok := groupID(i)
		if ok {
			if slot, seen := slotOf[id]; seen {
				rowSlot[i] = slot
				counts[slot]++
				continue
			}
			slotOf[id] = len(l.first)
		}
		rowSlot[i] = len(l.first)
		l.first = append(l.first, i)
		l.ids = append(l.ids, id)
		counts = append(counts, 1)
	}

	l.starts = make([]int, len(counts))
	next := make([]int, len(counts))
	pos := 0
	for slot, count := range counts {
		l.starts[slot], next[slot] = pos, pos
		pos += count
	}
	l.order = make([]int, n)
	for i, slot := range rowSlot {
		l.order[next[slot]] = i
		next[slot]++
	}

	for slot, count := range counts {
		if count < 2 {
			continue
		}
		members := l.order[l.starts[slot] : l.starts[slot]+count]
		rep := 0
		for k := 1; k < len(members); k++ {
			if compare(members[k], members[rep]) < 0 {
				rep = k
			}
		}
		r := members[rep]
		copy(members[1:rep+1], members[:rep])
		members[0] = r
	}
	return l
}

func (l *copyGroupLayout) end(slot int) int {
	if slot+1 < len(l.starts) {
		return l.starts[slot+1]
	}
	return len(l.order)
}

type copyGroupPageRow struct {
	row   int
	group *CopyGroupRow
}

func (l *copyGroupLayout) page(offset, limit int, member func(i int) CopyGroupMember) ([]copyGroupPageRow, bool) {
	start := min(max(offset, 0), len(l.starts))
	stop := len(l.starts)
	if limit > 0 {
		stop = min(start+limit, stop)
	}
	if start == stop {
		return nil, false
	}
	rows := make([]copyGroupPageRow, 0, l.end(stop-1)-l.starts[start])
	for slot := start; slot < stop; slot++ {
		members := l.order[l.starts[slot]:l.end(slot)]
		if len(members) == 1 {
			rows = append(rows, copyGroupPageRow{row: members[0]})
			continue
		}
		id := fmt.Sprintf("%016x", l.ids[slot])
		first := member(l.first[slot])
		rows = append(rows, copyGroupPageRow{row: members[0], group: &CopyGroupRow{ID: id, FirstSorted: &first}})
		shared := &CopyGroupRow{ID: id}
		for _, row := range members[1:] {
			rows = append(rows, copyGroupPageRow{row: row, group: shared})
		}
	}
	return rows, stop < len(l.starts)
}

func collapseInstanceTorrents(rows []*qbt.Torrent, instanceID int, groups *CopyGroups, offset, limit int) ([]copyGroupPageRow, bool) {
	layout := collapseCopyGroups(len(rows),
		func(i int) (uint64, bool) { return groups.GroupID(instanceID, rows[i].Hash) },
		func(a, b int) int { return CompareRepresentative(instanceID, rows[a], instanceID, rows[b]) })
	return layout.page(offset, limit, func(i int) CopyGroupMember {
		return CopyGroupMember{InstanceID: instanceID, Hash: rows[i].Hash}
	})
}

func collapseCrossInstanceTorrents(rows []CrossInstanceTorrentView, groups *CopyGroups, offset, limit int) ([]CrossInstanceTorrentView, bool) {
	layout := collapseCopyGroups(len(rows),
		func(i int) (uint64, bool) { return groups.GroupID(rows[i].InstanceID, rows[i].Hash) },
		func(a, b int) int {
			return CompareRepresentative(rows[a].InstanceID, rows[a].Torrent, rows[b].InstanceID, rows[b].Torrent)
		})
	grouped, hasMore := layout.page(offset, limit, func(i int) CopyGroupMember {
		return CopyGroupMember{InstanceID: rows[i].InstanceID, Hash: rows[i].Hash}
	})
	out := make([]CrossInstanceTorrentView, len(grouped))
	for i, g := range grouped {
		out[i] = rows[g.row]
		out[i].CopyGroup = g.group
	}
	return out, hasMore
}
