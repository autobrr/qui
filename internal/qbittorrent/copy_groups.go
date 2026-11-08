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

// CopyGroups maps instance and hash to a Copy group id, with the record of instances that ended a first read.
type CopyGroups struct {
	ids  map[int]map[string]uint64
	read map[int]struct{}
}

// NewCopyGroups keeps ids, so the caller must not change it after the call.
func NewCopyGroups(ids map[int]map[string]uint64, read []int) *CopyGroups {
	record := make(map[int]struct{}, len(read))
	for _, id := range read {
		record[id] = struct{}{}
	}
	return &CopyGroups{ids: ids, read: record}
}

func (g *CopyGroups) hasRead(instanceID int) bool {
	if g == nil {
		return false
	}
	_, ok := g.read[instanceID]
	return ok
}

// An instance outside the record has no groups, so its rows stay flat.
func (g *CopyGroups) groupID(instanceID int, hash string) (uint64, bool) {
	if !g.hasRead(instanceID) {
		return 0, false
	}
	id, ok := g.ids[instanceID][hash]
	return id, ok
}

type CopyGroupRow struct {
	ID string `json:"id"`
	// FirstSorted is set on the Group representative only.
	FirstSorted *CopyGroupMember `json:"first_sorted,omitempty"`
}

type CopyGroupMember struct {
	InstanceID int    `json:"instance_id"`
	Hash       string `json:"hash"`
}

// CompareRepresentative sorts the Group representative of a Copy group first.
func CompareRepresentative(aInstanceID int, a *qbt.Torrent, bInstanceID int, b *qbt.Torrent) int {
	return cmp.Or(
		cmp.Compare(a.AddedOn, b.AddedOn),
		strings.Compare(a.Hash, b.Hash),
		cmp.Compare(aInstanceID, bInstanceID),
	)
}

// SetCopyGroups wakes the streams of each instance whose entries or ready state changed.
func (sm *SyncManager) SetCopyGroups(next *CopyGroups) {
	prev := sm.copyGroups.Swap(next)
	for _, instanceID := range changedCopyGroupInstances(prev, next) {
		sm.notifyTrackerHealthUpdated(instanceID)
	}
}

// SetCopyGroupsDemandHandler registers the function that a list request or a grouped stream for Copy groups calls.
func (sm *SyncManager) SetCopyGroupsDemandHandler(handler func()) {
	sm.copyGroupsDemand.Store(&handler)
}

// DemandCopyGroups wakes the Copy groups job, or keeps it running.
func (sm *SyncManager) DemandCopyGroups() {
	if handler := sm.copyGroupsDemand.Load(); handler != nil {
		(*handler)()
	}
}

// LastSuccessfulSync reads the pool offline, so it never waits on a login. It
// is zero for an instance with no client or no successful sync.
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
	}
	var changed []int
	for _, id := range slices.Sorted(maps.Keys(instances)) {
		if prev.hasRead(id) != next.hasRead(id) || !maps.Equal(prev.entries(id), next.entries(id)) {
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

// A limit <= 0 means no limit.
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

func collapseInstanceTorrents(rows []qbt.Torrent, instanceID int, groups *CopyGroups, offset, limit int) ([]copyGroupPageRow, bool) {
	layout := collapseCopyGroups(len(rows),
		func(i int) (uint64, bool) { return groups.groupID(instanceID, rows[i].Hash) },
		func(a, b int) int { return CompareRepresentative(instanceID, &rows[a], instanceID, &rows[b]) })
	return layout.page(offset, limit, func(i int) CopyGroupMember {
		return CopyGroupMember{InstanceID: instanceID, Hash: rows[i].Hash}
	})
}

// The rows are built for this request, so their views take the group data in place.
func collapseCrossInstanceTorrents(rows []CrossInstanceTorrentView, groups *CopyGroups, offset, limit int) ([]CrossInstanceTorrentView, bool) {
	layout := collapseCopyGroups(len(rows),
		func(i int) (uint64, bool) { return groups.groupID(rows[i].InstanceID, rows[i].Hash) },
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
